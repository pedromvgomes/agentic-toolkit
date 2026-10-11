// Package usage reads Claude Code session transcripts into Usage rows: one
// row per assistant message, holding its token counts and the model that
// produced them. It makes no model calls and reads nothing but the
// transcript, so the same file always yields the same rows.
package usage

import "time"

// Harness is the value of Row.Harness for every row read from a Claude Code
// transcript.
const Harness = "claude-code"

// Row is one assistant message's token use, identified by
// (SessionID, MessageID). It holds counts and never a dollar amount.
type Row struct {
	Harness        string `json:"harness"`
	HarnessVersion string `json:"harness_version"`
	SessionID      string `json:"session_id"`
	MessageID      string `json:"message_id"`
	// Timestamp is the winning record's timestamp in UTC, or the zero time
	// when the record carries none that parses.
	Timestamp time.Time `json:"timestamp"`
	// Model is the full versioned model id the message reports.
	Model       string `json:"model"`
	InputTokens int64  `json:"input_tokens"`
	// OutputTokens includes the thinking tokens.
	OutputTokens int64 `json:"output_tokens"`
	// ThinkingTokens is the share of OutputTokens spent thinking, 0 when the
	// transcript does not report it. It is a breakdown of OutputTokens, so
	// adding it to OutputTokens counts those tokens twice.
	ThinkingTokens     int64 `json:"thinking_tokens"`
	CacheReadTokens    int64 `json:"cache_read_tokens"`
	CacheWrite5mTokens int64 `json:"cache_write_5m_tokens"`
	CacheWrite1hTokens int64 `json:"cache_write_1h_tokens"`
	IsSidechain        bool  `json:"is_sidechain"`
	// Entrypoint is the transcript's own entrypoint value, unmapped.
	Entrypoint  string `json:"entrypoint"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"git_branch"`
	AgtkVersion string `json:"agtk_version"`
}

// MessageRef identifies one assistant message within a session.
type MessageRef struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
}
