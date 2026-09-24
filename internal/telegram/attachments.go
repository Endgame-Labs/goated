package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"goated/internal/gateway"
)

const (
	defaultAttachmentsRootRel   = "workspace/tmp/telegram/attachments"
	defaultAttachmentMaxBytes   = int64(25 * 1024 * 1024)
	defaultAttachmentTotalMax   = int64(251 * 1024 * 1024)
	defaultAttachmentRetention  = 30 * 24 * time.Hour
	defaultAttachmentSweepEvery = 4 * time.Hour
)

var allowedAttachmentMIMEs = map[string]struct{}{
	"application/pdf":           {},
	"text/csv":                  {},
	"text/tab-separated-values": {},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":       {},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {},
	"application/csv":                                  {},
	"application/vnd.ms-excel":                         {},
	"application/vnd.ms-excel.sheet.macroenabled.12":   {},
	"application/vnd.ms-word.document.macroenabled.12": {},
}

var allowedAttachmentExts = map[string]struct{}{
	".png":  {},
	".jpg":  {},
	".jpeg": {},
	".gif":  {},
	".webp": {},
	".bmp":  {},
	".heic": {},
	".heif": {},
	".csv":  {},
	".tsv":  {},
	".xlsx": {},
	".docx": {},
	".pdf":  {},
	".txt":  {},
	".md":   {},
	".json": {},
	".yaml": {},
	".yml":  {},
	".log":  {},
}

type AttachmentConfig struct {
	RootPath      string
	MaxBytes      int64
	MaxTotalBytes int64
}

type attachmentSidecar struct {
	IngestedAt         string `json:"ingested_at"`
	FileID             string `json:"file_id"`
	Filename           string `json:"filename"`
	MIMEType           string `json:"mime_type"`
	StoredName         string `json:"stored_name"`
	StoredRelativePath string `json:"stored_relative_path"`
	Bytes              int64  `json:"bytes"`
	SHA256             string `json:"sha256"`
}

type fileSpec struct {
	Index    int
	FileID   string
	Filename string
	MIMEType string
	Size     int64
}

func (c *Connector) processAttachments(ctx context.Context, msg *tgbotapi.Message) ([]string, []gateway.AttachmentResult, []gateway.AttachmentResult, []gateway.AttachmentResult) {
	specs := telegramFileSpecs(msg)
	attachments := make([]string, 0, len(specs))
	results := make([]gateway.AttachmentResult, 0, len(specs))
	failed := make([]gateway.AttachmentResult, 0, len(specs))
	succeeded := make([]gateway.AttachmentResult, 0, len(specs))

	seenFileIDs := map[string]struct{}{}
	var acceptedTotal int64

	for _, spec := range specs {
		res := gateway.AttachmentResult{
			Index:    spec.Index,
			FileID:   spec.FileID,
			Filename: spec.Filename,
			MIMEType: strings.ToLower(strings.TrimSpace(spec.MIMEType)),
		}

		if spec.FileID != "" {
			if _, ok := seenFileIDs[spec.FileID]; ok {
				res.Outcome = "failed"
				res.ReasonCode = "deduped"
				res.Reason = "duplicate file id in message"
				results = append(results, res)
				failed = append(failed, res)
				continue
			}
			seenFileIDs[spec.FileID] = struct{}{}
		}

		if !isAllowedByMetadata(spec.Filename, spec.MIMEType) {
			res.Outcome = "failed"
			res.ReasonCode = "unsupported_type"
			res.Reason = "unsupported file type"
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		if spec.Size > 0 && spec.Size > c.attachmentMaxBytes {
			res.Outcome = "failed"
			res.ReasonCode = "too_large"
			res.Reason = "file exceeds per-attachment size limit"
			res.Bytes = spec.Size
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		if spec.Size > 0 && acceptedTotal+spec.Size > c.attachmentTotalMax {
			res.Outcome = "failed"
			res.ReasonCode = "too_large"
			res.Reason = "aggregate attachment size exceeds per-message limit"
			res.Bytes = spec.Size
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		relPath, written, detectedMIME, reasonCode, reason := c.downloadAndPersistAttachment(ctx, spec, msg)
		if reasonCode != "" {
			res.Outcome = "failed"
			res.ReasonCode = reasonCode
			res.Reason = reason
			res.Bytes = written
			if detectedMIME != "" {
				res.MIMEType = detectedMIME
			}
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		if acceptedTotal+written > c.attachmentTotalMax {
			_ = os.Remove(filepath.Join(c.attachmentsRootAbs, filepath.Base(relPath)))
			_ = os.Remove(filepath.Join(c.attachmentsRootAbs, filepath.Base(relPath)+".meta.json"))
			res.Outcome = "failed"
			res.ReasonCode = "too_large"
			res.Reason = "aggregate attachment size exceeds per-message limit"
			res.Bytes = written
			results = append(results, res)
			failed = append(failed, res)
			continue
		}

		acceptedTotal += written
		res.Outcome = "succeeded"
		res.Path = relPath
		res.Bytes = written
		if detectedMIME != "" {
			res.MIMEType = detectedMIME
		}
		attachments = append(attachments, relPath)
		results = append(results, res)
		succeeded = append(succeeded, res)
	}

	return attachments, results, failed, succeeded
}

func telegramFileSpecs(msg *tgbotapi.Message) []fileSpec {
	if msg == nil {
		return nil
	}
	specs := make([]fileSpec, 0, 2)

	if len(msg.Photo) > 0 {
		best := msg.Photo[len(msg.Photo)-1]
		specs = append(specs, fileSpec{
			Index:    len(specs),
			FileID:   best.FileID,
			Filename: "telegram_photo.jpg",
			MIMEType: "image/jpeg",
			Size:     int64(best.FileSize),
		})
	}

	if msg.Document != nil {
		filename := strings.TrimSpace(msg.Document.FileName)
		if filename == "" {
			filename = "telegram_document"
		}
		specs = append(specs, fileSpec{
			Index:    len(specs),
			FileID:   msg.Document.FileID,
			Filename: filename,
			MIMEType: msg.Document.MimeType,
			Size:     int64(msg.Document.FileSize),
		})
	}

	return specs
}

func (c *Connector) downloadAndPersistAttachment(ctx context.Context, spec fileSpec, msg *tgbotapi.Message) (relativePath string, written int64, detectedMIME string, reasonCode string, reason string) {
	if spec.FileID == "" {
		return "", 0, "", "download_failed", "missing telegram file id"
	}

	url, err := c.bot.GetFileDirectURL(spec.FileID)
	if err != nil {
		return "", 0, "", "download_failed", "failed resolving telegram file URL"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, "", "download_failed", "failed creating download request"
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, "", "download_failed", "download request failed"
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, "", "download_failed", fmt.Sprintf("download failed with status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(c.attachmentsRootAbs, "download-*.part")
	if err != nil {
		return "", 0, "", "download_failed", "failed creating temp file"
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	h := sha256.New()
	buf := make([]byte, 32*1024)
	sniff := make([]byte, 0, 512)
	for {
		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			if written+int64(nr) > c.attachmentMaxBytes {
				_ = tmpFile.Close()
				return "", written, "", "too_large", "file exceeds per-attachment size limit"
			}
			if _, ew := tmpFile.Write(buf[:nr]); ew != nil {
				_ = tmpFile.Close()
				return "", written, "", "download_failed", "failed writing attachment"
			}
			if _, ew := h.Write(buf[:nr]); ew != nil {
				_ = tmpFile.Close()
				return "", written, "", "download_failed", "failed hashing attachment"
			}
			if len(sniff) < 512 {
				need := 512 - len(sniff)
				if need > nr {
					need = nr
				}
				sniff = append(sniff, buf[:need]...)
			}
			written += int64(nr)
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			_ = tmpFile.Close()
			return "", written, "", "download_failed", "attachment download interrupted"
		}
	}

	if written == 0 {
		_ = tmpFile.Close()
		return "", 0, "", "download_failed", "attachment was empty"
	}

	detectedMIME = http.DetectContentType(sniff)
	if !isAllowedByContent(spec.Filename, spec.MIMEType, detectedMIME) {
		_ = tmpFile.Close()
		return "", written, detectedMIME, "corrupt", "file content type does not match allowed formats"
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return "", written, detectedMIME, "download_failed", "failed flushing attachment to disk"
	}
	if err := tmpFile.Close(); err != nil {
		return "", written, detectedMIME, "download_failed", "failed closing attachment temp file"
	}

	ext := normalizedAttachmentExt(spec.Filename)
	if ext == "" {
		ext = extFromMIME(spec.MIMEType)
	}
	seed := fmt.Sprintf("%s|%s|%d|%d", spec.FileID, spec.Filename, msg.MessageID, written)
	seedHash := sha256.Sum256([]byte(seed))
	storedName := hex.EncodeToString(seedHash[:16]) + ext
	finalAbs := filepath.Join(c.attachmentsRootAbs, storedName)
	if !isSubPath(c.attachmentsRootAbs, finalAbs) {
		return "", written, detectedMIME, "download_failed", "refusing to write outside attachment root"
	}
	if err := os.Rename(tmpName, finalAbs); err != nil {
		return "", written, detectedMIME, "download_failed", "failed moving attachment into place"
	}

	relPath := filepath.ToSlash(filepath.Join(c.attachmentsRootRel, storedName))
	sidecar := attachmentSidecar{
		IngestedAt:         time.Now().UTC().Format(time.RFC3339),
		FileID:             spec.FileID,
		Filename:           spec.Filename,
		MIMEType:           strings.ToLower(strings.TrimSpace(spec.MIMEType)),
		StoredName:         storedName,
		StoredRelativePath: relPath,
		Bytes:              written,
		SHA256:             hex.EncodeToString(h.Sum(nil)),
	}
	if err := writeSidecar(finalAbs+".meta.json", sidecar); err != nil {
		_ = os.Remove(finalAbs)
		return "", written, detectedMIME, "download_failed", "failed writing sidecar metadata"
	}

	return relPath, written, detectedMIME, "", ""
}

func writeSidecar(metaPath string, meta attachmentSidecar) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp := metaPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, metaPath)
}

func (c *Connector) runAttachmentSweeper(ctx context.Context) {
	c.cleanupExpiredAttachments()
	ticker := time.NewTicker(c.sweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.cleanupExpiredAttachments()
		}
	}
}

func (c *Connector) cleanupExpiredAttachments() {
	entries, err := os.ReadDir(c.attachmentsRootAbs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "telegram attachments sweep error: %v\n", err)
		return
	}

	now := time.Now().UTC()
	scanned := 0
	deleted := 0
	skippedRecent := 0
	skippedLocked := 0
	errors := 0

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}
		scanned++
		metaPath := filepath.Join(c.attachmentsRootAbs, entry.Name())
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			errors++
			continue
		}
		var meta attachmentSidecar
		if err := json.Unmarshal(metaData, &meta); err != nil {
			errors++
			continue
		}
		ingestedAt, err := time.Parse(time.RFC3339, meta.IngestedAt)
		if err != nil {
			errors++
			continue
		}
		if now.Sub(ingestedAt) <= c.attachmentRetention {
			skippedRecent++
			continue
		}

		storedName := meta.StoredName
		if storedName == "" {
			storedName = strings.TrimSuffix(entry.Name(), ".meta.json")
		}
		attachmentPath := filepath.Join(c.attachmentsRootAbs, storedName)
		if _, err := os.Stat(attachmentPath + ".lock"); err == nil {
			skippedLocked++
			continue
		}

		if err := os.Remove(attachmentPath); err != nil && !os.IsNotExist(err) {
			errors++
			continue
		}
		if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
			errors++
			continue
		}
		deleted++
	}

	fmt.Fprintf(os.Stderr,
		"telegram attachments sweep: scanned=%d deleted=%d skipped_recent=%d skipped_locked=%d errors=%d\n",
		scanned, deleted, skippedRecent, skippedLocked, errors,
	)
}

// textLikeMIMEs is the explicit set of declared types we accept as text
// uploads. It mirrors the text formats named in the user-facing supported
// uploads message (TXT, MD, JSON, YAML, LOG, CSV/TSV) plus the aliases
// Telegram clients and browsers realistically send for them. A blanket
// "text/" prefix would also pull in text/html, text/x-python and friends,
// which we do not promise to handle.
var textLikeMIMEs = map[string]struct{}{
	"text/plain":                {},
	"text/markdown":             {},
	"text/x-markdown":           {},
	"application/json":          {},
	"text/json":                 {},
	"application/yaml":          {},
	"application/x-yaml":        {},
	"text/yaml":                 {},
	"text/x-yaml":               {},
	"text/csv":                  {},
	"application/csv":           {},
	"text/tab-separated-values": {},
}

func isTextLikeMIME(mime string) bool {
	_, ok := textLikeMIMEs[mime]
	return ok
}

func isAllowedByMetadata(filename, mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	ext := normalizedAttachmentExt(filename)
	if strings.HasPrefix(mime, "image/") {
		return true
	}
	if isTextLikeMIME(mime) {
		return true
	}
	if _, ok := allowedAttachmentMIMEs[mime]; ok {
		return true
	}
	_, ok := allowedAttachmentExts[ext]
	return ok
}

func isAllowedByContent(filename, mime, detected string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	detected = strings.ToLower(strings.TrimSpace(detected))
	ext := normalizedAttachmentExt(filename)

	if strings.HasPrefix(mime, "image/") {
		return strings.HasPrefix(detected, "image/")
	}
	if ext == ".pdf" || mime == "application/pdf" {
		return detected == "application/pdf"
	}
	if ext == ".docx" || ext == ".xlsx" ||
		mime == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" ||
		mime == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		return detected == "application/zip" || detected == "application/octet-stream"
	}
	if ext == ".csv" || ext == ".tsv" || mime == "text/csv" || mime == "text/tab-separated-values" || mime == "application/csv" {
		return strings.HasPrefix(detected, "text/plain") || detected == "application/octet-stream"
	}
	if isTextLikeMIME(mime) || isTextLikeExt(ext) {
		// Genuine text always sniffs as some text/ type. Anything else here
		// is a binary payload wearing a text extension: executables and other
		// unrecognised bytes come back as application/octet-stream, archives
		// as application/zip, and so on.
		return strings.HasPrefix(detected, "text/")
	}
	if _, ok := allowedAttachmentMIMEs[mime]; ok {
		return detected == mime || detected == "application/octet-stream"
	}
	return false
}

// textLikeExts backs the fallback for uploads Telegram sends without a usable
// MIME type. Every entry must also appear in allowedAttachmentExts, otherwise
// isAllowedByMetadata rejects the file before content sniffing ever runs.
var textLikeExts = map[string]struct{}{
	".txt":  {},
	".md":   {},
	".json": {},
	".yaml": {},
	".yml":  {},
	".log":  {},
	".csv":  {},
	".tsv":  {},
}

func isTextLikeExt(ext string) bool {
	_, ok := textLikeExts[ext]
	return ok
}

func normalizedAttachmentExt(name string) string {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(name)))
	if ext == ".tiff" {
		return ".tif"
	}
	return ext
}

func extFromMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/csv", "application/csv":
		return ".csv"
	case "text/tab-separated-values":
		return ".tsv"
	case "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return ".xlsx"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "text/plain":
		return ".txt"
	case "text/markdown":
		return ".md"
	case "application/json":
		return ".json"
	case "application/yaml", "application/x-yaml", "text/yaml":
		return ".yaml"
	default:
		return ""
	}
}

func isSubPath(root, target string) bool {
	rootClean := filepath.Clean(root)
	targetClean := filepath.Clean(target)
	if rootClean == targetClean {
		return true
	}
	prefix := rootClean + string(os.PathSeparator)
	return strings.HasPrefix(targetClean, prefix)
}

func sumAttachmentBytes(items []gateway.AttachmentResult) int64 {
	var total int64
	for _, i := range items {
		total += i.Bytes
	}
	return total
}
