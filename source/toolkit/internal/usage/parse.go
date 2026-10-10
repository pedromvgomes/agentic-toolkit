package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"
)

// syntheticModel is the model Claude Code records on assistant messages it
// writes itself, which no model produced and no token was spent on.
const syntheticModel = "<synthetic>"

// Options carries the values a row takes from the reader rather than from
// the transcript.
type Options struct {
	// AgtkVersion is stamped on every row as Row.AgtkVersion.
	AgtkVersion string
}

// Result is what one transcript yields.
type Result struct {
	// Rows holds one row per complete assistant message, in the order each
	// message id first appears in the file.
	Rows []Row
	// Held lists the message that is not yet complete: the file's last
	// assistant message when none of its records carries a stop_reason.
	// It is the only message a transcript still being written can leave
	// open, and it becomes a row once the transcript moves past it.
	Held []MessageRef
	// Skipped counts lines that fail to decode and assistant records that
	// carry no message id, no usage, or the synthetic model. Blank lines and
	// records of any other type are not counted.
	Skipped int
}

// record is the subset of a transcript line a row is built from. Every other
// field is ignored.
type record struct {
	Type        string   `json:"type"`
	Version     string   `json:"version"`
	SessionID   string   `json:"sessionId"`
	Timestamp   string   `json:"timestamp"`
	IsSidechain bool     `json:"isSidechain"`
	Entrypoint  string   `json:"entrypoint"`
	Cwd         string   `json:"cwd"`
	GitBranch   string   `json:"gitBranch"`
	Message     *message `json:"message"`
}

type message struct {
	ID    string `json:"id"`
	Model string `json:"model"`
	// StopReason is raw so that an absent value and JSON null both read as
	// "not stopped", and any other value reads as stopped.
	StopReason json.RawMessage `json:"stop_reason"`
	Usage      *tokenUsage     `json:"usage"`
}

type tokenUsage struct {
	InputTokens          int64 `json:"input_tokens"`
	OutputTokens         int64 `json:"output_tokens"`
	CacheReadInputTokens int64 `json:"cache_read_input_tokens"`
	CacheCreation        *struct {
		Ephemeral5m int64 `json:"ephemeral_5m_input_tokens"`
		Ephemeral1h int64 `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
	OutputTokensDetails *struct {
		ThinkingTokens int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func (m *message) stopped() bool {
	raw := bytes.TrimSpace(m.StopReason)
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null"))
}

// pending is one message id's state while the file is read.
type pending struct {
	winner  *record
	stopped bool
}

// ParseFile reads the Claude Code transcript at path and returns its Usage
// rows.
//
// A streamed message is written as several records sharing one message id,
// not necessarily adjacent. The last usable record in file order wins, and
// every field of the row comes from it. A message is complete when any of
// its records carries a stop_reason, or when a record of a different
// assistant message follows its last record; only the file's final
// assistant message can be neither, and it is held rather than emitted.
//
// A line that fails to decode is skipped, so a transcript still being
// written is read up to its last complete line. Only failing to open or
// read the file is an error.
func ParseFile(path string, opts Options) (Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()

	var res Result
	var order []string
	byID := map[string]*pending{}
	// lastID is the message id of the last assistant record carrying one,
	// usable or not: every other message has a different id after it.
	var lastID string

	r := bufio.NewReader(f)
	for {
		line, readErr := r.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return Result{}, readErr
		}
		if len(bytes.TrimSpace(line)) > 0 {
			var rec record
			if err := json.Unmarshal(line, &rec); err != nil {
				res.Skipped++
			} else if rec.Type == "assistant" {
				if rec.Message == nil || rec.Message.ID == "" {
					res.Skipped++
				} else {
					id := rec.Message.ID
					lastID = id
					p, seen := byID[id]
					if !seen {
						p = &pending{}
						byID[id] = p
					}
					if rec.Message.stopped() {
						p.stopped = true
					}
					if rec.Message.Usage == nil || rec.Message.Model == syntheticModel {
						res.Skipped++
					} else {
						if p.winner == nil {
							order = append(order, id)
						}
						p.winner = &rec
					}
				}
			}
		}
		if readErr != nil {
			break
		}
	}

	for _, id := range order {
		p := byID[id]
		if id == lastID && !p.stopped {
			res.Held = append(res.Held, MessageRef{SessionID: p.winner.SessionID, MessageID: id})
			continue
		}
		res.Rows = append(res.Rows, toRow(p.winner, opts))
	}
	return res, nil
}

func toRow(rec *record, opts Options) Row {
	m := rec.Message
	u := m.Usage
	row := Row{
		Harness:         Harness,
		HarnessVersion:  rec.Version,
		SessionID:       rec.SessionID,
		MessageID:       m.ID,
		Model:           m.Model,
		InputTokens:     u.InputTokens,
		OutputTokens:    u.OutputTokens,
		CacheReadTokens: u.CacheReadInputTokens,
		IsSidechain:     rec.IsSidechain,
		Entrypoint:      rec.Entrypoint,
		Cwd:             rec.Cwd,
		GitBranch:       rec.GitBranch,
		AgtkVersion:     opts.AgtkVersion,
	}
	if ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil {
		row.Timestamp = ts.UTC()
	}
	if u.OutputTokensDetails != nil {
		row.ThinkingTokens = u.OutputTokensDetails.ThinkingTokens
	}
	if u.CacheCreation != nil {
		row.CacheWrite5mTokens = u.CacheCreation.Ephemeral5m
		row.CacheWrite1hTokens = u.CacheCreation.Ephemeral1h
	}
	return row
}
