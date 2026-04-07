package logging

import (
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// isRetentionTarget returns true if the file should be considered for
// retention (age/count cleanup and compression).
func isRetentionTarget(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasSuffix(lower, ".stream") {
		return false
	}
	for _, ext := range []string{".log", ".jsonl", ".log.gz", ".jsonl.gz"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// isCompressible returns true if the file is an uncompressed log/jsonl file.
func isCompressible(name string) bool {
	lower := strings.ToLower(name)
	return (strings.HasSuffix(lower, ".log") || strings.HasSuffix(lower, ".jsonl")) &&
		!strings.HasSuffix(lower, ".gz")
}

// EnforceRetention deletes log files older than maxAgeDays and caps total file
// count at maxFiles. If compress is true, gzip .log and .jsonl files older
// than 1 day (skipping the newest file in case it is still being written).
func EnforceRetention(logDir string, maxAgeDays int, maxFiles int, compress bool) error {
	if logDir == "" {
		return fmt.Errorf("logDir is empty")
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)

	// Phase 1: delete files older than maxAgeDays
	if err := deleteByAge(logDir, cutoff); err != nil {
		return fmt.Errorf("delete by age: %w", err)
	}

	// Phase 2: compress eligible files older than 1 day
	if compress {
		if err := compressOldFiles(logDir); err != nil {
			return fmt.Errorf("compress: %w", err)
		}
	}

	// Phase 3: cap by count (keep most recent maxFiles)
	if err := deleteByCount(logDir, maxFiles); err != nil {
		return fmt.Errorf("delete by count: %w", err)
	}

	return nil
}

// deleteByAge walks the directory and removes retention-target files whose
// modification time is before the cutoff.
func deleteByAge(logDir string, cutoff time.Time) error {
	return filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		if !isRetentionTarget(info.Name()) {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			log.Printf("log retention: deleting expired file %s (age=%s)",
				path, time.Since(info.ModTime()).Round(time.Hour))
			if removeErr := os.Remove(path); removeErr != nil {
				log.Printf("log retention: failed to delete %s: %v", path, removeErr)
			}
		}
		return nil
	})
}

type fileEntry struct {
	path    string
	modTime time.Time
}

// deleteByCount re-reads the directory, sorts by modification time, and
// removes the oldest files until the count is at or below maxFiles.
func deleteByCount(logDir string, maxFiles int) error {
	var files []fileEntry
	err := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !isRetentionTarget(info.Name()) {
			return nil
		}
		files = append(files, fileEntry{path: path, modTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return err
	}

	if len(files) <= maxFiles {
		return nil
	}

	// Sort oldest first
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.Before(files[j].modTime)
	})

	toRemove := len(files) - maxFiles
	for i := 0; i < toRemove; i++ {
		log.Printf("log retention: deleting excess file %s (count cap=%d, total=%d)",
			files[i].path, maxFiles, len(files))
		if removeErr := os.Remove(files[i].path); removeErr != nil {
			log.Printf("log retention: failed to delete %s: %v", files[i].path, removeErr)
		}
	}
	return nil
}

// compressOldFiles gzips .log and .jsonl files older than 1 day, skipping the
// newest compressible file in each directory in case it is still being written.
func compressOldFiles(logDir string) error {
	oneDayAgo := time.Now().Add(-24 * time.Hour)

	// Collect compressible files grouped by directory
	byDir := make(map[string][]fileEntry)
	err := filepath.Walk(logDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !isCompressible(info.Name()) {
			return nil
		}
		dir := filepath.Dir(path)
		byDir[dir] = append(byDir[dir], fileEntry{path: path, modTime: info.ModTime()})
		return nil
	})
	if err != nil {
		return err
	}

	for _, entries := range byDir {
		if len(entries) == 0 {
			continue
		}
		// Sort newest first so we can skip index 0
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].modTime.After(entries[j].modTime)
		})
		for i := 1; i < len(entries); i++ {
			e := entries[i]
			if e.modTime.After(oneDayAgo) {
				continue // not old enough
			}
			if err := gzipFile(e.path); err != nil {
				log.Printf("log retention: failed to compress %s: %v", e.path, err)
			} else {
				log.Printf("log retention: compressed %s", e.path)
			}
		}
	}
	return nil
}

// gzipFile compresses src into src.gz and removes src on success.
func gzipFile(src string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dst := src + ".gz"
	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	gw := gzip.NewWriter(out)
	if _, err := io.Copy(gw, in); err != nil {
		gw.Close()
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := gw.Close(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}

	// Source successfully compressed — remove original
	return os.Remove(src)
}
