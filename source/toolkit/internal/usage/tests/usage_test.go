package tests

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
)

var opts = usage.Options{AgtkVersion: "v9.9.9"}

func parse(t *testing.T, name string) usage.Result {
	t.Helper()
	res, err := usage.ParseFile(filepath.Join("testdata", name), opts)
	if err != nil {
		t.Fatalf("ParseFile(%s): %v", name, err)
	}
	return res
}

func wantOutputs(t *testing.T, rows []usage.Row, want map[string]int64) {
	t.Helper()
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for _, r := range rows {
		w, ok := want[r.MessageID]
		if !ok {
			t.Errorf("unexpected row %q", r.MessageID)
		} else if r.OutputTokens != w {
			t.Errorf("%s output = %d, want %d", r.MessageID, r.OutputTokens, w)
		}
	}
}

func TestLastRecordOfASplitMessageWins(t *testing.T) {
	res := parse(t, "split.jsonl")
	wantOutputs(t, res.Rows, map[string]int64{"msg_1": 830, "msg_2": 12})
	if res.Rows[0].MessageID != "msg_1" || res.Rows[1].MessageID != "msg_2" {
		t.Errorf("rows are not in first-appearance order: %+v", res.Rows)
	}
	if len(res.Held) != 0 || res.Skipped != 0 {
		t.Errorf("held=%v skipped=%d, want none", res.Held, res.Skipped)
	}
}

func TestMessageFollowedByAnotherIsCompleteWithoutStopReason(t *testing.T) {
	res := parse(t, "followed.jsonl")
	wantOutputs(t, res.Rows, map[string]int64{"msg_1": 9, "msg_2": 11})
	if len(res.Held) != 0 {
		t.Errorf("held = %v, want none", res.Held)
	}
}

func TestTrailingMessageWithoutStopReasonIsHeld(t *testing.T) {
	res := parse(t, "trailing_held.jsonl")
	wantOutputs(t, res.Rows, map[string]int64{"msg_1": 7})
	want := []usage.MessageRef{{SessionID: "sess-1", MessageID: "msg_2"}}
	if !reflect.DeepEqual(res.Held, want) {
		t.Errorf("held = %v, want %v", res.Held, want)
	}
}

func TestTrailingMessageWithoutStopReasonKeyIsHeld(t *testing.T) {
	res := parse(t, "trailing_absent_stop.jsonl")
	if len(res.Rows) != 0 {
		t.Errorf("rows = %+v, want none", res.Rows)
	}
	want := []usage.MessageRef{{SessionID: "sess-1", MessageID: "msg_1"}}
	if !reflect.DeepEqual(res.Held, want) {
		t.Errorf("held = %v, want %v", res.Held, want)
	}
}

func TestTrailingMessageWithStopReasonIsEmitted(t *testing.T) {
	res := parse(t, "trailing_stopped.jsonl")
	wantOutputs(t, res.Rows, map[string]int64{"msg_1": 7, "msg_2": 13})
	if len(res.Held) != 0 {
		t.Errorf("held = %v, want none", res.Held)
	}
}

func TestSessionReadsSubagentsAfterMainInFilenameOrder(t *testing.T) {
	res, err := usage.ParseSession(filepath.Join("testdata", "session", "main.jsonl"), opts)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range res.Rows {
		ids = append(ids, r.MessageID)
		if r.SessionID != "sess-1" {
			t.Errorf("%s session id = %q, want the parent's", r.MessageID, r.SessionID)
		}
		if want := strings.HasPrefix(r.MessageID, "msg_s"); r.IsSidechain != want {
			t.Errorf("%s IsSidechain = %v, want %v", r.MessageID, r.IsSidechain, want)
		}
	}
	if want := []string{"msg_1", "msg_s1", "msg_s2"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("row order = %v, want %v", ids, want)
	}
}

func TestSessionWithoutSubagentsDirectoryIsTheMainTranscript(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "trailing_stopped.jsonl"), filepath.Join(dir, "solo.jsonl"))
	res, err := usage.ParseSession(filepath.Join(dir, "solo.jsonl"), opts)
	if err != nil {
		t.Fatal(err)
	}
	wantOutputs(t, res.Rows, map[string]int64{"msg_1": 7, "msg_2": 13})
}

func TestSessionSumsHeldAndSkippedAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "skipped.jsonl"), filepath.Join(dir, "s.jsonl"))
	copyFile(t, filepath.Join("testdata", "trailing_held.jsonl"), filepath.Join(dir, "s", "subagents", "agent-1.jsonl"))
	copyFile(t, filepath.Join("testdata", "skipped.jsonl"), filepath.Join(dir, "s", "subagents", "agent-2.jsonl"))
	copyFile(t, filepath.Join("testdata", "skipped.jsonl"), filepath.Join(dir, "s", "subagents", "notes.jsonl"))
	res, err := usage.ParseSession(filepath.Join(dir, "s.jsonl"), opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 6 || len(res.Held) != 1 || len(res.Rows) != 3 {
		t.Errorf("skipped=%d held=%v rows=%d, want 6, 1 held, 3 rows", res.Skipped, res.Held, len(res.Rows))
	}
}

func TestSessionWithMissingMainStillReturnsSubagentRows(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "trailing_stopped.jsonl"), filepath.Join(dir, "gone", "subagents", "agent-a.jsonl"))
	main := filepath.Join(dir, "gone.jsonl")
	res, err := usage.ParseSession(main, opts)
	if err == nil || !strings.Contains(err.Error(), main) || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want one naming %s wrapping ErrNotExist", err, main)
	}
	if len(res.Rows) != 2 {
		t.Errorf("rows = %d, want the subagent's 2", len(res.Rows))
	}
}

func TestSessionWhoseSubagentsPathIsNotADirectoryReturnsMainRowsAndError(t *testing.T) {
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "trailing_stopped.jsonl"), filepath.Join(dir, "s.jsonl"))
	blocker := filepath.Join(dir, "s", "subagents")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := usage.ParseSession(filepath.Join(dir, "s.jsonl"), opts)
	if err == nil || !strings.Contains(err.Error(), blocker) {
		t.Fatalf("err = %v, want one naming %s", err, blocker)
	}
	if len(res.Rows) != 2 {
		t.Errorf("rows = %d, want the main transcript's 2", len(res.Rows))
	}
}

func TestSessionWithUnreadableSubagentReturnsOtherRowsAndError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("file permissions do not block reads as root")
	}
	dir := t.TempDir()
	copyFile(t, filepath.Join("testdata", "trailing_stopped.jsonl"), filepath.Join(dir, "s.jsonl"))
	bad := filepath.Join(dir, "s", "subagents", "agent-a.jsonl")
	copyFile(t, filepath.Join("testdata", "trailing_stopped.jsonl"), bad)
	copyFile(t, filepath.Join("testdata", "trailing_stopped.jsonl"), filepath.Join(dir, "s", "subagents", "agent-b.jsonl"))
	if err := os.Chmod(bad, 0); err != nil {
		t.Fatal(err)
	}
	res, err := usage.ParseSession(filepath.Join(dir, "s.jsonl"), opts)
	if err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("err = %v, want one naming %s", err, bad)
	}
	if len(res.Rows) != 4 {
		t.Errorf("rows = %d, want 4 from the readable files", len(res.Rows))
	}
}

func TestUnusableAssistantRecordsAreSkippedAndCounted(t *testing.T) {
	res := parse(t, "skipped.jsonl")
	wantOutputs(t, res.Rows, map[string]int64{"msg_ok": 2})
	if res.Skipped != 3 {
		t.Errorf("Skipped = %d, want 3", res.Skipped)
	}
}

func TestUnknownRecordTypesAndFieldsAreIgnored(t *testing.T) {
	res := parse(t, "ignored.jsonl")
	wantOutputs(t, res.Rows, map[string]int64{"msg_1": 7})
	if res.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0", res.Skipped)
	}
}

func TestUndecodableLinesAreSkippedAndCounted(t *testing.T) {
	good, err := os.ReadFile(filepath.Join("testdata", "trailing_stopped.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.SplitAfter(string(good), "\n")
	content := lines[0] + "{not json\n" + lines[1] + lines[2][:len(lines[2])/2]
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := usage.ParseFile(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 2 {
		t.Errorf("Skipped = %d, want 2", res.Skipped)
	}
	if len(res.Rows) != 1 || res.Rows[0].MessageID != "msg_1" {
		t.Errorf("rows = %+v, want msg_1", res.Rows)
	}
	want := []usage.MessageRef{{SessionID: "sess-1", MessageID: "msg_2"}}
	if !reflect.DeepEqual(res.Held, want) {
		t.Errorf("held = %v, want %v", res.Held, want)
	}
}

func TestThinkingTokensAreABreakdownOfOutputTokens(t *testing.T) {
	rows := parse(t, "tokens.jsonl").Rows
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].OutputTokens != 100 || rows[0].ThinkingTokens != 60 {
		t.Errorf("output=%d thinking=%d, want 100 and 60", rows[0].OutputTokens, rows[0].ThinkingTokens)
	}
	if rows[1].OutputTokens != 2 || rows[1].ThinkingTokens != 0 {
		t.Errorf("output=%d thinking=%d, want 2 and 0", rows[1].OutputTokens, rows[1].ThinkingTokens)
	}
}

func TestCacheReadAndWriteTiersAreSeparate(t *testing.T) {
	rows := parse(t, "tokens.jsonl").Rows
	r := rows[0]
	if r.InputTokens != 10 || r.CacheReadTokens != 40 || r.CacheWrite5mTokens != 20 || r.CacheWrite1hTokens != 30 {
		t.Errorf("row = %+v", r)
	}
	if z := rows[1]; z.CacheReadTokens != 0 || z.CacheWrite5mTokens != 0 || z.CacheWrite1hTokens != 0 {
		t.Errorf("absent cache fields = %+v, want zeros", z)
	}
}

func TestRowCarriesTranscriptAndReaderFields(t *testing.T) {
	r := parse(t, "trailing_stopped.jsonl").Rows[0]
	if want := time.Date(2026, 10, 10, 6, 42, 53, 777_000_000, time.UTC); !r.Timestamp.Equal(want) || r.Timestamp.Location() != time.UTC {
		t.Errorf("Timestamp = %v, want %v in UTC", r.Timestamp, want)
	}
	if r.Harness != "claude-code" || r.AgtkVersion != "v9.9.9" || r.HarnessVersion != "2.0.0" ||
		r.SessionID != "sess-1" || r.Model != "claude-opus-5-5" || r.Entrypoint != "cli" ||
		r.Cwd != "/work/example" || r.GitBranch != "main" || r.IsSidechain {
		t.Errorf("row = %+v", r)
	}
}

func TestRowJSONUsesSnakeCaseKeys(t *testing.T) {
	b, err := json.Marshal(parse(t, "tokens.jsonl").Rows[0])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"harness", "harness_version", "session_id", "message_id", "timestamp", "model",
		"input_tokens", "output_tokens", "thinking_tokens", "cache_read_tokens", "cache_write_5m_tokens",
		"cache_write_1h_tokens", "is_sidechain", "entrypoint", "cwd", "git_branch", "agtk_version"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q in %s", k, b)
		}
	}
}

func TestEmptyFileYieldsNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.jsonl")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := usage.ParseFile(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rows) != 0 || len(res.Held) != 0 || res.Skipped != 0 {
		t.Errorf("res = %+v, want empty", res)
	}
}

func TestMissingFileIsAnError(t *testing.T) {
	if _, err := usage.ParseFile(filepath.Join(t.TempDir(), "nope.jsonl"), opts); err == nil {
		t.Fatal("want an error")
	}
}

func TestParsingTheSameFileTwiceYieldsEqualResults(t *testing.T) {
	for _, name := range []string{"split.jsonl", "trailing_held.jsonl", "skipped.jsonl"} {
		if a, b := parse(t, name), parse(t, name); !reflect.DeepEqual(a, b) {
			t.Errorf("%s: results differ:\n%+v\n%+v", name, a, b)
		}
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		t.Fatal(err)
	}
}
