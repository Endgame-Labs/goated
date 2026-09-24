package telegram

import "testing"

// The accepted set must stay in step with the user-facing sentence in
// gateway.failedAttachmentReply: "images, PDF, CSV/TSV, DOCX, XLSX, and text
// files (TXT, MD, JSON, YAML, LOG)". The cases below pin both halves of that
// promise: what Telegram may declare, and what the downloaded bytes may be.

func TestIsAllowedByMetadata_AcceptsPromisedTextFormats(t *testing.T) {
	cases := []struct {
		filename string
		mime     string
	}{
		{"notes.txt", "text/plain"},
		{"README.md", "text/markdown"},
		{"README.md", "text/x-markdown"},
		{"data.json", "application/json"},
		{"data.json", "text/json"},
		{"config.yaml", "application/yaml"},
		{"config.yaml", "application/x-yaml"},
		{"config.yml", "text/yaml"},
		{"app.log", "text/plain"},
		// Telegram frequently omits the MIME type; the extension carries it.
		{"notes.txt", ""},
		{"README.md", ""},
		{"data.json", ""},
		{"config.yaml", ""},
		{"config.yml", ""},
		{"app.log", ""},
	}
	for _, tc := range cases {
		if !isAllowedByMetadata(tc.filename, tc.mime) {
			t.Errorf("isAllowedByMetadata(%q, %q) = false, want true", tc.filename, tc.mime)
		}
	}
}

func TestIsAllowedByMetadata_RejectsTextFormatsOutsidePromise(t *testing.T) {
	cases := []struct {
		filename string
		mime     string
	}{
		{"page.html", "text/html"},
		{"script.py", "text/x-python"},
		{"deploy.sh", "text/x-shellscript"},
		{"app.js", "application/javascript"},
		{"app.ts", "application/typescript"},
		{"data.xml", "application/xml"},
		{"config.toml", "application/toml"},
		{"events.ndjson", "application/x-ndjson"},
	}
	for _, tc := range cases {
		if isAllowedByMetadata(tc.filename, tc.mime) {
			t.Errorf("isAllowedByMetadata(%q, %q) = true, want false", tc.filename, tc.mime)
		}
	}
}

func TestIsAllowedByMetadata_RejectsBinaryFormats(t *testing.T) {
	cases := []struct {
		filename string
		mime     string
	}{
		{"report.exe", "application/x-msdownload"},
		{"report.exe", ""},
		{"archive.zip", "application/zip"},
		{"archive.zip", ""},
		{"tool", "application/octet-stream"},
	}
	for _, tc := range cases {
		if isAllowedByMetadata(tc.filename, tc.mime) {
			t.Errorf("isAllowedByMetadata(%q, %q) = true, want false", tc.filename, tc.mime)
		}
	}
}

func TestIsAllowedByContent_AcceptsTextSniffedAsText(t *testing.T) {
	// http.DetectContentType reports UTF-8 text as text/plain with a charset.
	cases := []struct {
		filename string
		mime     string
		detected string
	}{
		{"notes.txt", "text/plain", "text/plain; charset=utf-8"},
		{"notes.txt", "", "text/plain; charset=utf-8"},
		{"README.md", "text/markdown", "text/plain; charset=utf-8"},
		{"data.json", "application/json", "text/plain; charset=utf-8"},
		{"config.yaml", "application/yaml", "text/plain; charset=utf-8"},
		{"config.yml", "", "text/plain; charset=utf-8"},
		{"app.log", "", "text/plain; charset=utf-8"},
		// A note that happens to open with markup still sniffs as text.
		{"notes.txt", "text/plain", "text/html; charset=utf-8"},
	}
	for _, tc := range cases {
		if !isAllowedByContent(tc.filename, tc.mime, tc.detected) {
			t.Errorf("isAllowedByContent(%q, %q, %q) = false, want true", tc.filename, tc.mime, tc.detected)
		}
	}
}

func TestIsAllowedByContent_RejectsTextExtensionWithPDFBytes(t *testing.T) {
	cases := []struct {
		filename string
		mime     string
	}{
		{"notes.txt", "text/plain"},
		{"notes.txt", ""},
		{"data.json", "application/json"},
		{"config.yaml", ""},
		{"app.log", "text/plain"},
	}
	for _, tc := range cases {
		if isAllowedByContent(tc.filename, tc.mime, "application/pdf") {
			t.Errorf("isAllowedByContent(%q, %q, application/pdf) = true, want false", tc.filename, tc.mime)
		}
	}
}

func TestIsAllowedByContent_RejectsTextExtensionWithBinaryBytes(t *testing.T) {
	// Go sniffs an unrecognised binary payload (a PE or ELF executable) as
	// application/octet-stream, and a ZIP container as application/zip.
	// Real text never lands on either, so both must fail the text branch.
	cases := []struct {
		filename string
		mime     string
		detected string
	}{
		{"notes.txt", "text/plain", "application/octet-stream"},
		{"notes.txt", "", "application/octet-stream"},
		{"data.json", "application/json", "application/octet-stream"},
		{"notes.txt", "text/plain", "application/zip"},
		{"config.yaml", "", "application/zip"},
	}
	for _, tc := range cases {
		if isAllowedByContent(tc.filename, tc.mime, tc.detected) {
			t.Errorf("isAllowedByContent(%q, %q, %q) = true, want false", tc.filename, tc.mime, tc.detected)
		}
	}
}

func TestIsAllowedByContent_RejectsTextFormatsOutsidePromise(t *testing.T) {
	cases := []struct {
		filename string
		mime     string
		detected string
	}{
		{"page.html", "text/html", "text/html; charset=utf-8"},
		{"script.py", "text/x-python", "text/plain; charset=utf-8"},
		{"data.xml", "application/xml", "text/xml; charset=utf-8"},
		{"config.toml", "application/toml", "text/plain; charset=utf-8"},
	}
	for _, tc := range cases {
		if isAllowedByContent(tc.filename, tc.mime, tc.detected) {
			t.Errorf("isAllowedByContent(%q, %q, %q) = true, want false", tc.filename, tc.mime, tc.detected)
		}
	}
}
