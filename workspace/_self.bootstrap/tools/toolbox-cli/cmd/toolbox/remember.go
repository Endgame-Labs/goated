package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"toolbox-example/internal/workspace"

	"github.com/spf13/cobra"
)

type rememberResult struct {
	Source string `json:"source"`
	Kind   string `json:"kind"`
	Text   string `json:"text,omitempty"`
}

func rememberCmd() *cobra.Command {
	var indexOnly bool
	var limit int
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "remember [query]",
		Short: "Search local filesystem memory for context on a person, place, thing, or idea",
		Long: `Searches filesystem-based memory only. This example does not use vectors,
Turbopuffer, or external semantic search.

It looks through common self-repo memory locations like:
  - top-level markdown files
  - vault/
  - memory/
  - notes/
  - docs/
  - posts/
  - archives/`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			results, err := searchFilesystemMemory(args[0], limit, indexOnly)
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}

			if len(results) == 0 {
				fmt.Printf("No results found for %q\n", args[0])
				return nil
			}

			fmt.Printf("=== Remembering %q — %d results ===\n\n", args[0], len(results))
			for _, r := range results {
				if indexOnly {
					fmt.Printf("[%s] %s\n", r.Kind, r.Source)
					continue
				}
				fmt.Printf("--- [%s] %s ---\n", r.Kind, r.Source)
				if r.Text != "" {
					fmt.Println(r.Text)
				}
				fmt.Println()
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&indexOnly, "index", false, "Only show paths, not content snippets")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	cmd.Flags().IntVarP(&limit, "limit", "n", 12, "Maximum number of results")
	return cmd
}

func searchFilesystemMemory(query string, limit int, indexOnly bool) ([]rememberResult, error) {
	selfDir, err := workspace.SelfDirFromExecutable()
	if err != nil {
		return nil, err
	}

	candidates := collectRememberPaths(selfDir)
	queryNorm := normalizeRemember(query)
	results := make([]rememberResult, 0, limit)

	for _, path := range candidates {
		rel, relErr := filepath.Rel(selfDir, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		baseNorm := normalizeRemember(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
		content, readErr := os.ReadFile(path)
		text := ""
		contentNorm := ""
		if readErr == nil {
			text = string(content)
			contentNorm = normalizeRemember(text)
		}

		matched := false
		kind := "content"
		if strings.Contains(baseNorm, queryNorm) || strings.Contains(queryNorm, baseNorm) {
			matched = true
			kind = "file"
		} else if contentNorm != "" && strings.Contains(contentNorm, queryNorm) {
			matched = true
		}
		if !matched {
			continue
		}

		result := rememberResult{
			Source: rel,
			Kind:   kind,
		}
		if !indexOnly {
			result.Text = rememberSnippet(text, query)
		}
		results = append(results, result)
		if len(results) >= limit {
			break
		}
	}

	return dedupRemember(results), nil
}

func collectRememberPaths(selfDir string) []string {
	var paths []string
	searchRoots := []string{
		selfDir,
		filepath.Join(selfDir, "vault"),
		filepath.Join(selfDir, "memory"),
		filepath.Join(selfDir, "notes"),
		filepath.Join(selfDir, "docs"),
		filepath.Join(selfDir, "posts"),
		filepath.Join(selfDir, "archives"),
	}
	seen := map[string]bool{}

	for _, root := range searchRoots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				switch name {
				case ".git", "tools", "logs", "tmp", "node_modules", "vendor":
					if path != selfDir {
						return filepath.SkipDir
					}
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(name))
			switch ext {
			case ".md", ".txt", ".json":
			default:
				return nil
			}
			if !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
			return nil
		})
	}

	sort.Strings(paths)
	return paths
}

func rememberSnippet(text, query string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	queryNorm := normalizeRemember(query)
	for i, line := range lines {
		if strings.Contains(normalizeRemember(line), queryNorm) {
			start := i - 2
			if start < 0 {
				start = 0
			}
			end := i + 3
			if end > len(lines) {
				end = len(lines)
			}
			return truncateRemember(strings.TrimSpace(strings.Join(lines[start:end], "\n")), 1200)
		}
	}
	return truncateRemember(strings.TrimSpace(text), 1200)
}

func dedupRemember(in []rememberResult) []rememberResult {
	seen := map[string]bool{}
	out := make([]rememberResult, 0, len(in))
	for _, r := range in {
		key := r.Kind + "|" + r.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, r)
	}
	return out
}

func normalizeRemember(s string) string {
	s = strings.ToLower(s)
	replacer := strings.NewReplacer("-", " ", "_", " ", "/", " ", ".", " ")
	s = replacer.Replace(s)
	return strings.Join(strings.Fields(s), " ")
}

func truncateRemember(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "..."
}
