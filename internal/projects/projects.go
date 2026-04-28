// Package projects resolves Claude Code project directories and enumerates
// session JSONL files.
package projects

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// nonAlnum matches the same character class as Python's
// re.sub(r"[^a-zA-Z0-9]", "-", abs_path).
var nonAlnum = regexp.MustCompile("[^a-zA-Z0-9]")

// ClaudeProjectsDir returns ~/.claude/projects (or "" if HOME is unset).
func ClaudeProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// ProjectKeyFromPath converts an absolute project path into Claude Code's
// directory key by replacing every non-alphanumeric character with `-`.
func ProjectKeyFromPath(path string) (string, error) {
	abs, err := absResolve(path)
	if err != nil {
		return "", err
	}
	return nonAlnum.ReplaceAllString(abs, "-"), nil
}

func absResolve(path string) (string, error) {
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	// Resolve symlinks where possible; if the path doesn't exist, fall back to
	// the cleaned absolute path (Python's Path.resolve() also degrades to
	// best-effort on missing paths).
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path, err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// ProjectDirFor returns the ~/.claude/projects/<key> directory for path, or
// "" if it doesn't exist.
func ProjectDirFor(path string) string {
	key, err := ProjectKeyFromPath(path)
	if err != nil {
		return ""
	}
	root := ClaudeProjectsDir()
	if root == "" {
		return ""
	}
	d := filepath.Join(root, key)
	if info, err := os.Stat(d); err == nil && info.IsDir() {
		return d
	}
	return ""
}

// FindProjectForCWD walks up from cwd looking for a matching project dir.
// Returns "" if none found. Pass "" for cwd to use the current working dir.
func FindProjectForCWD(cwd string) string {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}
	abs, err := absResolve(cwd)
	if err != nil {
		return ""
	}
	for p := abs; ; p = filepath.Dir(p) {
		if d := ProjectDirFor(p); d != "" {
			return d
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
	}
	return ""
}

// LatestSessionJSONL returns the *.jsonl file in projectDir with the newest
// mtime, or "" if there are none.
func LatestSessionJSONL(projectDir string) string {
	matches, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	var newest string
	var newestMtime int64
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		mt := info.ModTime().UnixNano()
		if newest == "" || mt > newestMtime {
			newest = p
			newestMtime = mt
		}
	}
	return newest
}

// SessionJSONL returns projectDir/<sessionID>.jsonl if it exists, else "".
func SessionJSONL(projectDir, sessionID string) string {
	p := filepath.Join(projectDir, sessionID+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// SessionInfo describes a session for the picker.
type SessionInfo struct {
	ProjectName  string
	ProjectPath  string
	SessionID    string
	JSONLPath    string
	FirstPrompt  string
	MessageCount int
	GitBranch    string
	MTime        int64 // milliseconds
}

// ListRecentSessions enumerates recent sessions across all projects in
// ~/.claude/projects, newest first, capped at limit.
func ListRecentSessions(limit int) []SessionInfo {
	root := ClaudeProjectsDir()
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	var out []SessionInfo
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		projectDir := filepath.Join(root, ent.Name())
		indexed := map[string]bool{}

		idxPath := filepath.Join(projectDir, "sessions-index.json")
		if data, err := os.ReadFile(idxPath); err == nil {
			var idx struct {
				Entries []struct {
					FullPath     string `json:"fullPath"`
					ProjectPath  string `json:"projectPath"`
					SessionID    string `json:"sessionId"`
					FirstPrompt  string `json:"firstPrompt"`
					MessageCount int    `json:"messageCount"`
					Modified     string `json:"modified"`
					GitBranch    string `json:"gitBranch"`
					FileMtime    int64  `json:"fileMtime"`
				} `json:"entries"`
			}
			if err := json.Unmarshal(data, &idx); err == nil {
				for _, e := range idx.Entries {
					if e.FullPath == "" {
						continue
					}
					info, err := os.Stat(e.FullPath)
					if err != nil {
						continue
					}
					indexed[e.FullPath] = true
					name := filepath.Base(e.ProjectPath)
					if name == "" || name == "." || name == "/" {
						name = ent.Name()
					}
					sid := e.SessionID
					if sid == "" {
						sid = stem(e.FullPath)
					}
					mt := e.FileMtime
					if mt == 0 {
						mt = info.ModTime().UnixMilli()
					}
					out = append(out, SessionInfo{
						ProjectName:  name,
						ProjectPath:  e.ProjectPath,
						SessionID:    sid,
						JSONLPath:    e.FullPath,
						FirstPrompt:  strings.TrimSpace(e.FirstPrompt),
						MessageCount: e.MessageCount,
						GitBranch:    e.GitBranch,
						MTime:        mt,
					})
				}
			}
		}

		matches, _ := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
		sort.Strings(matches)
		for _, p := range matches {
			if indexed[p] {
				continue
			}
			info, err := os.Stat(p)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			meta := peekJSONLMetadata(p)
			name := filepath.Base(meta.cwd)
			if name == "" || name == "." || name == "/" {
				name = ent.Name()
			}
			out = append(out, SessionInfo{
				ProjectName: name,
				ProjectPath: meta.cwd,
				SessionID:   meta.sessionID,
				JSONLPath:   p,
				FirstPrompt: meta.firstPrompt,
				GitBranch:   meta.gitBranch,
				MTime:       info.ModTime().UnixMilli(),
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].MTime > out[j].MTime })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

type peekedMeta struct {
	cwd, gitBranch, sessionID, firstPrompt string
}

// peekJSONLMetadata scans the first ~30 lines of a JSONL for cwd / sessionId /
// gitBranch / first user prompt.
func peekJSONLMetadata(path string) peekedMeta {
	out := peekedMeta{sessionID: stem(path)}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<25)
	for i := 0; sc.Scan(); i++ {
		if i > 30 && out.cwd != "" && out.firstPrompt != "" {
			break
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			continue
		}
		if out.cwd == "" {
			if c, ok := obj["cwd"].(string); ok && c != "" {
				out.cwd = c
				if b, ok := obj["gitBranch"].(string); ok && b != "" {
					out.gitBranch = b
				}
				if s, ok := obj["sessionId"].(string); ok && s != "" {
					out.sessionID = s
				}
			}
		}
		if out.firstPrompt == "" && obj["type"] == "user" && !asBool(obj["isSidechain"]) {
			msg, _ := obj["message"].(map[string]any)
			text := ""
			if msg != nil {
				switch c := msg["content"].(type) {
				case string:
					text = c
				case []any:
					for _, p := range c {
						part, ok := p.(map[string]any)
						if !ok {
							continue
						}
						if part["type"] != "text" {
							continue
						}
						t, _ := part["text"].(string)
						if t == "" {
							continue
						}
						if strings.HasPrefix(t, "<ide_opened_file>") || strings.HasPrefix(t, "<ide_selection>") {
							continue
						}
						text = t
						break
					}
				}
			}
			if strings.TrimSpace(text) != "" {
				out.firstPrompt = strings.TrimSpace(text)
			}
		}
	}
	return out
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func stem(path string) string {
	base := filepath.Base(path)
	if dot := strings.LastIndex(base, "."); dot > 0 {
		return base[:dot]
	}
	return base
}
