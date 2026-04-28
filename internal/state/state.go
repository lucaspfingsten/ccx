// Package state manages .ccx-state.json — the persistent cursor used by
// ccx --save to emit only new turns since the previous run.
package state

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/lucaspfingsten/ccx/internal/parser"
)

// File is the on-disk schema for .ccx-state.json. Snake_case JSON keys.
type File struct {
	SessionID     string `json:"session_id"`
	LastUUID      string `json:"last_uuid"`
	LastTimestamp string `json:"last_timestamp"`
	LastSave      string `json:"last_save"`
	// LastLineIndex is the fallback cursor used when LastUUID is empty.
	LastLineIndex int `json:"last_line_index,omitempty"`
}

// Read loads a state file. Returns (nil, nil) if the file doesn't exist.
func Read(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// Write persists state to path with 0644 permissions.
func Write(path string, f *File) error {
	out, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	return os.WriteFile(path, out, 0o644)
}

// FromLastEvent returns a state pointing at the last event in events. If events
// is empty, returns a fresh state with empty cursor (still records sessionID).
func FromLastEvent(sessionID string, events []parser.Event) *File {
	now := time.Now().UTC().Format(time.RFC3339)
	f := &File{SessionID: sessionID, LastSave: now}
	if len(events) == 0 {
		return f
	}
	last := events[len(events)-1]
	if u, ok := last.Raw["uuid"].(string); ok {
		f.LastUUID = u
	}
	if ts, ok := last.Raw["timestamp"].(string); ok {
		f.LastTimestamp = ts
	}
	f.LastLineIndex = last.LineIndex
	return f
}

// SliceResult describes the outcome of CursorSlice — see that function.
type SliceResult struct {
	// Mode is one of "full", "diff", "empty".
	//   "full"  — no usable cursor, callers should render the whole session.
	//   "diff"  — there's a cursor and there are new events after it.
	//   "empty" — there's a cursor and no new events; callers should emit a
	//             "no new turns since X" notice.
	Mode string

	// Events holds the slice of events strictly after the cursor (for "diff"
	// and "empty" modes). For "full" it is the entire input slice.
	Events []parser.Event

	// MatchedAt is the index in the input events where the cursor was found
	// (or -1 if no match). Only meaningful when Mode != "full".
	MatchedAt int
}

// CursorSlice computes the post-cursor slice given the loaded state and the
// current session's session id and events. The decision tree:
//
//   - If state is nil → full.
//   - If state.SessionID != currentSessionID → full (different session, reset).
//   - If LastUUID is set and matches an event in the slice → slice from after
//     that uuid. (primary cursor)
//   - Else if LastTimestamp+LastLineIndex match an event → slice from after it.
//     (fallback cursor)
//   - Else → full (cursor lost).
func CursorSlice(state *File, currentSessionID string, events []parser.Event) SliceResult {
	if state == nil || state.SessionID == "" || state.SessionID != currentSessionID {
		return SliceResult{Mode: "full", Events: events, MatchedAt: -1}
	}

	idx := -1
	if state.LastUUID != "" {
		for i, ev := range events {
			if u, ok := ev.Raw["uuid"].(string); ok && u == state.LastUUID {
				idx = i
				break
			}
		}
	}
	if idx == -1 && state.LastTimestamp != "" {
		for i, ev := range events {
			ts, _ := ev.Raw["timestamp"].(string)
			if ts == state.LastTimestamp && ev.LineIndex == state.LastLineIndex {
				idx = i
				break
			}
		}
	}
	if idx == -1 {
		return SliceResult{Mode: "full", Events: events, MatchedAt: -1}
	}

	post := events[idx+1:]
	mode := "diff"
	if len(post) == 0 {
		mode = "empty"
	}
	return SliceResult{Mode: mode, Events: post, MatchedAt: idx}
}
