// Package parser loads Claude Code session JSONL files and extracts the
// post-compaction conversation slice.
package parser

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Turn is a single user or assistant turn in a session.
type Turn struct {
	Role      string   `json:"role"`
	Text      string   `json:"text"`
	ToolCalls []string `json:"tool_calls"`
	Timestamp string   `json:"timestamp"`
	UUID      string   `json:"uuid,omitempty"`
}

// CompactMeta holds the raw compactMetadata fields from a system event.
type CompactMeta struct {
	Trigger    string `json:"trigger,omitempty"`
	PreTokens  int    `json:"preTokens,omitempty"`
	PostTokens int    `json:"postTokens,omitempty"`
	DurationMs int    `json:"durationMs,omitempty"`
	// Extra holds fields not modeled above so render can pass them through.
	Extra map[string]any `json:"-"`
}

// Session is the parsed view of a session JSONL.
type Session struct {
	ProjectPath      string
	SessionID        string
	GitBranch        string
	Summary          string
	HasSummary       bool
	CompactMeta      *CompactMeta
	CompactTimestamp string
	Turns            []Turn

	// Events holds the full parsed event stream for callers (state/cursor) that
	// need to slice or look up by uuid.
	Events []Event
	// SummaryIdx is the index of the last isCompactSummary event in Events,
	// or -1 if the session has never been compacted.
	SummaryIdx int
}

// Event is a single JSONL line as a parsed object plus the raw 0-based line
// index it came from (useful as a fallback cursor).
type Event struct {
	Raw       map[string]any
	LineIndex int
}

// LoadJSONL reads a JSONL file, skipping blank lines and silently dropping
// malformed JSON lines.
func LoadJSONL(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return loadFromReader(f)
}

func loadFromReader(r io.Reader) ([]Event, error) {
	var events []Event
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<25)
	idx := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			idx++
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			idx++
			continue
		}
		events = append(events, Event{Raw: obj, LineIndex: idx})
		idx++
	}
	if err := sc.Err(); err != nil {
		return events, err
	}
	return events, nil
}

// ExtractTextFromMessage returns plain text from a message.content (string or
// list-of-parts), skipping IDE markers and non-text parts.
func ExtractTextFromMessage(message map[string]any) string {
	content, ok := message["content"]
	if !ok {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	parts, ok := content.([]any)
	if !ok {
		return ""
	}
	var out []string
	for _, p := range parts {
		part, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if part["type"] != "text" {
			continue
		}
		text, _ := part["text"].(string)
		if strings.HasPrefix(text, "<ide_opened_file>") || strings.HasPrefix(text, "<ide_selection>") {
			continue
		}
		out = append(out, text)
	}
	return strings.Join(out, "\n")
}

// FindLastCompaction scans events backwards for the last isCompactSummary user
// event and looks back ≤5 events for the matching system+compactMetadata.
// Returns (idx, summaryText, meta) or (-1, "", nil) if never compacted.
func FindLastCompaction(events []Event) (int, string, *CompactMeta) {
	summaryIdx := -1
	var summaryText string
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i].Raw
		if ev["type"] == "user" && truthy(ev["isCompactSummary"]) {
			summaryIdx = i
			msg, _ := ev["message"].(map[string]any)
			if msg != nil {
				summaryText = ExtractTextFromMessage(msg)
			}
			break
		}
	}
	if summaryIdx < 0 {
		return -1, "", nil
	}

	lower := summaryIdx - 5
	if lower < -1 {
		lower = -1
	}
	for i := summaryIdx; i > lower; i-- {
		ev := events[i].Raw
		if ev["type"] != "system" {
			continue
		}
		meta, ok := ev["compactMetadata"].(map[string]any)
		if !ok {
			continue
		}
		return summaryIdx, summaryText, mapToCompactMeta(meta)
	}
	return summaryIdx, summaryText, nil
}

func mapToCompactMeta(m map[string]any) *CompactMeta {
	cm := &CompactMeta{Extra: map[string]any{}}
	for k, v := range m {
		switch k {
		case "trigger":
			if s, ok := v.(string); ok {
				cm.Trigger = s
			}
		case "preTokens":
			cm.PreTokens = toInt(v)
		case "postTokens":
			cm.PostTokens = toInt(v)
		case "durationMs":
			cm.DurationMs = toInt(v)
		default:
			cm.Extra[k] = v
		}
	}
	return cm
}

func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	}
	return 0
}

// SummarizeToolUse renders a single tool_use part into the `↳ Tool: ...` line
// shown in the markdown output. Format must match the Python implementation
// exactly — tests pin it.
func SummarizeToolUse(part map[string]any) string {
	name, _ := part["name"].(string)
	if name == "" {
		name = "?"
	}
	inp, _ := part["input"].(map[string]any)
	if inp == nil {
		inp = map[string]any{}
	}
	getStr := func(k string) string {
		s, _ := inp[k].(string)
		return s
	}

	switch name {
	case "Bash":
		desc := getStr("description")
		cmd := getStr("command")
		if desc != "" {
			return "↳ Bash: " + desc
		}
		if len(cmd) > 80 {
			cmd = cmd[:80]
		}
		return "↳ Bash: " + cmd
	case "Read", "Write":
		fp := getStr("file_path")
		if fp == "" {
			fp = "?"
		}
		return "↳ " + name + ": " + fp
	case "Edit":
		fp := getStr("file_path")
		if fp == "" {
			fp = "?"
		}
		return "↳ Edit: " + fp
	case "NotebookEdit":
		nb := getStr("notebook_path")
		if nb == "" {
			nb = "?"
		}
		return "↳ NotebookEdit: " + nb
	case "Grep":
		p := getStr("pattern")
		if p == "" {
			p = "?"
		}
		return "↳ Grep: " + p
	case "Glob":
		p := getStr("pattern")
		if p == "" {
			p = "?"
		}
		return "↳ Glob: " + p
	case "WebFetch":
		u := getStr("url")
		if u == "" {
			u = "?"
		}
		return "↳ WebFetch: " + u
	case "WebSearch":
		q := getStr("query")
		if q == "" {
			q = "?"
		}
		return "↳ WebSearch: " + q
	case "TodoWrite":
		todos, _ := inp["todos"].([]any)
		return "↳ TodoWrite: " + strconv.Itoa(len(todos)) + " items"
	case "Task", "Agent":
		desc := getStr("description")
		sub := getStr("subagent_type")
		label := ""
		if sub != "" {
			label = sub + ": "
		}
		return "↳ Agent: " + label + desc
	}
	return "↳ " + name
}

// EventToTurn converts a single event into a Turn, applying the noise filter.
// Returns nil if the event should be dropped from the turn stream.
func EventToTurn(event Event, includeToolCalls bool) *Turn {
	ev := event.Raw
	et, _ := ev["type"].(string)
	if et != "user" && et != "assistant" {
		return nil
	}
	if truthy(ev["isSidechain"]) {
		return nil
	}
	if truthy(ev["isCompactSummary"]) {
		return nil
	}

	msg, _ := ev["message"].(map[string]any)
	if msg == nil {
		msg = map[string]any{}
	}
	content := msg["content"]
	ts, _ := ev["timestamp"].(string)
	uuid, _ := ev["uuid"].(string)

	if et == "user" {
		if parts, ok := content.([]any); ok {
			hasReal := false
			for _, p := range parts {
				part, ok := p.(map[string]any)
				if !ok {
					continue
				}
				if part["type"] != "text" {
					continue
				}
				text, _ := part["text"].(string)
				if text == "" {
					continue
				}
				if strings.HasPrefix(text, "<ide_opened_file>") || strings.HasPrefix(text, "<ide_selection>") {
					continue
				}
				hasReal = true
				break
			}
			if !hasReal {
				return nil
			}
		}
		text := ExtractTextFromMessage(msg)
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return &Turn{Role: "user", Text: text, Timestamp: ts, UUID: uuid}
	}

	// assistant
	var textParts []string
	var toolCalls []string
	switch c := content.(type) {
	case []any:
		for _, p := range c {
			part, ok := p.(map[string]any)
			if !ok {
				continue
			}
			t, _ := part["type"].(string)
			switch t {
			case "text":
				txt, _ := part["text"].(string)
				if strings.TrimSpace(txt) != "" {
					textParts = append(textParts, txt)
				}
			case "tool_use":
				if includeToolCalls {
					toolCalls = append(toolCalls, SummarizeToolUse(part))
				}
			}
		}
	case string:
		if strings.TrimSpace(c) != "" {
			textParts = append(textParts, c)
		}
	}

	text := strings.TrimSpace(strings.Join(textParts, "\n"))
	if text == "" && len(toolCalls) == 0 {
		return nil
	}
	return &Turn{Role: "assistant", Text: text, ToolCalls: toolCalls, Timestamp: ts, UUID: uuid}
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

// ParseSession loads and parses a session JSONL into a Session.
//
// includeToolCalls controls whether assistant tool_use parts are summarized;
// maxTurns (when > 0) keeps only the last N turns.
func ParseSession(jsonlPath string, includeToolCalls bool, maxTurns int) (*Session, error) {
	events, err := LoadJSONL(jsonlPath)
	if err != nil {
		return nil, err
	}
	return parseEvents(events, jsonlPath, includeToolCalls, maxTurns), nil
}

func parseEvents(events []Event, jsonlPath string, includeToolCalls bool, maxTurns int) *Session {
	cwd, sessionID, branch := firstMeta(events)
	if sessionID == "" {
		sessionID = stem(jsonlPath)
	}

	summaryIdx, summaryText, compactMeta := FindLastCompaction(events)
	var compactTS string
	postEvents := events
	if summaryIdx >= 0 {
		if ts, ok := events[summaryIdx].Raw["timestamp"].(string); ok {
			compactTS = ts
		}
		postEvents = events[summaryIdx+1:]
	}

	var turns []Turn
	for _, ev := range postEvents {
		if t := EventToTurn(ev, includeToolCalls); t != nil {
			turns = append(turns, *t)
		}
	}

	if maxTurns > 0 && len(turns) > maxTurns {
		turns = turns[len(turns)-maxTurns:]
	}

	return &Session{
		ProjectPath:      cwd,
		SessionID:        sessionID,
		GitBranch:        branch,
		Summary:          summaryText,
		HasSummary:       summaryIdx >= 0,
		CompactMeta:      compactMeta,
		CompactTimestamp: compactTS,
		Turns:            turns,
		Events:           events,
		SummaryIdx:       summaryIdx,
	}
}

// ParseSessionFromEvents parses an already-loaded event slice. Used by callers
// that have read events for cursor logic and want to avoid re-reading the file.
func ParseSessionFromEvents(events []Event, jsonlPath string, includeToolCalls bool, maxTurns int) *Session {
	return parseEvents(events, jsonlPath, includeToolCalls, maxTurns)
}

// TurnsFromEvents converts a slice of events into turns with the same noise
// filter, useful for diff slices that aren't a full session parse.
func TurnsFromEvents(events []Event, includeToolCalls bool) []Turn {
	var turns []Turn
	for _, ev := range events {
		if t := EventToTurn(ev, includeToolCalls); t != nil {
			turns = append(turns, *t)
		}
	}
	return turns
}

func firstMeta(events []Event) (cwd, sessionID, branch string) {
	for _, ev := range events {
		if _, ok := ev.Raw["cwd"]; !ok {
			continue
		}
		cwd, _ = ev.Raw["cwd"].(string)
		sessionID, _ = ev.Raw["sessionId"].(string)
		branch, _ = ev.Raw["gitBranch"].(string)
		return
	}
	return "", "", ""
}

func stem(path string) string {
	name := filepath.Base(path)
	if dot := strings.LastIndex(name, "."); dot > 0 {
		return name[:dot]
	}
	return name
}
