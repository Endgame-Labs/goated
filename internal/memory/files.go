package memory

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// FileSearcher is Goated's dependency-free default. It searches text memory
// files under the instance workspace, never credentials or runtime state.
type FileSearcher struct {
	Workspace  string
	MaxFiles   int
	MaxResults int
}

func (s FileSearcher) Search(ctx context.Context, query string) ([]Chunk, error) {
	roots := fileSearchRoots(s.Workspace)
	terms := queryTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	maxFiles := s.MaxFiles
	if maxFiles <= 0 {
		maxFiles = 25000
	}
	maxResults := s.MaxResults
	if maxResults <= 0 {
		maxResults = 32
	}
	type hit struct {
		chunk Chunk
		score int
	}
	var hits []hit
	seen := 0
	skip := map[string]bool{".git": true, "creds": true, "logs": true, "tmp": true, "node_modules": true, "vendor": true, "state": true, "archives": true, "experiments": true, "tools": true, ".venv": true, "venv": true}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// WalkDir does not follow directory symlinks, but ReadFile follows
			// file symlinks. Exclude both, including selected search roots.
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if d.IsDir() {
				if path != root && skip[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == "AGENTS.md" || d.Name() == "GOATED.md" || d.Name() == "CLAUDE.md" {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".md" && ext != ".txt" {
				return nil
			}
			seen++
			if seen > maxFiles {
				return filepath.SkipAll
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 256<<10 {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			content := string(data)
			lower := strings.ToLower(content)
			name := strings.ToLower(d.Name())
			score := 0
			at := -1
			for _, term := range terms {
				if containsWord(name, term) {
					score += 5
				}
				i := wordIndex(lower, term)
				if i >= 0 {
					score += 2
					if at < 0 || i < at {
						at = i
					}
				}
			}
			if score == 0 {
				return nil
			}
			if strings.Contains(lower, strings.ToLower(strings.TrimSpace(query))) {
				score += 8
			}
			start := at - 300
			if start < 0 {
				start = 0
			}
			end := start + 1500
			if end > len(content) {
				end = len(content)
			}
			if start > 0 {
				for start < len(content) && content[start-1] != '\n' {
					start++
				}
			}
			if end < len(content) {
				for end > start && content[end-1] != '\n' {
					end--
				}
			}
			if start >= end {
				start = 0
				end = len(content)
				if end > 1500 {
					end = 1500
				}
			}
			snippet := strings.TrimSpace(content[start:end])
			if snippet == "" {
				return nil
			}
			rel, _ := filepath.Rel(s.Workspace, path)
			hits = append(hits, hit{Chunk{Source: filepath.ToSlash(rel), Text: snippet}, score})
			return nil
		})
		if err != nil {
			return nil, err
		}
		if seen >= maxFiles {
			break
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].chunk.Source < hits[j].chunk.Source
		}
		return hits[i].score > hits[j].score
	})
	if len(hits) > maxResults {
		hits = hits[:maxResults]
	}
	out := make([]Chunk, len(hits))
	for i, h := range hits {
		out[i] = h.chunk
	}
	return out, nil
}
func queryTerms(s string) []string {
	stop := map[string]bool{"the": true, "and": true, "for": true, "with": true, "that": true, "this": true, "what": true, "when": true, "where": true, "how": true, "you": true, "your": true, "have": true, "from": true, "into": true, "then": true, "there": true, "about": true, "just": true, "please": true, "could": true, "would": true, "should": true, "make": true, "does": true, "are": true, "can": true, "will": true}
	var out []string
	seen := map[string]bool{}
	var b strings.Builder
	flush := func() {
		t := b.String()
		b.Reset()
		if len([]rune(t)) >= 3 && !stop[t] && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

func containsWord(s, term string) bool { return wordIndex(s, term) >= 0 }
func wordIndex(s, term string) int {
	for from := 0; from < len(s); {
		i := strings.Index(s[from:], term)
		if i < 0 {
			return -1
		}
		i += from
		before := i == 0 || !wordRune(rune(s[i-1]))
		after := i+len(term) == len(s) || !wordRune(rune(s[i+len(term)]))
		if before && after {
			return i
		}
		from = i + 1
	}
	return -1
}
func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func fileSearchRoots(workspace string) []string {
	self := filepath.Join(workspace, "self")
	if info, err := os.Lstat(self); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil
	} else if err != nil || !info.IsDir() {
		return []string{workspace}
	}
	var roots []string
	var rootInfos []os.FileInfo
	for _, name := range []string{"MEMORY.md", "USER.md", "SOUL.md", "memory", "vault", "VAULT", "MISSIONS", "missions", "notes", "knowledge"} {
		p := filepath.Join(self, name)
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink == 0 {
			duplicate := false
			for _, prior := range rootInfos {
				if os.SameFile(prior, info) {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			roots = append(roots, p)
			rootInfos = append(rootInfos, info)
		}
	}
	if len(roots) == 0 {
		return []string{self}
	}
	return roots
}
