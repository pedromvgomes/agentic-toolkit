package memory

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"
)

// Hit is one read of a note through `agtk memory show`.
//
// Accounting rides on the read path rather than on a separate "record a
// hit" call, because a separate call is exactly what an agent skips under
// context pressure — and a denominator that drifts makes the hit rate lie
// in the reassuring direction.
type Hit struct {
	Note string    `json:"note"`
	At   time.Time `json:"at"`
}

// RecordHit appends one line to the gitignored hits log. Failures are the
// caller's to ignore: telemetry must never break a read.
func (s *Store) RecordHit(name string, at time.Time) error {
	// `show` can be the first command ever run against a hand-created
	// store, and writing the log without its .gitignore would commit local
	// telemetry on the next `git add -A`.
	if err := s.ensureGitignore(); err != nil {
		return err
	}
	f, err := os.OpenFile(s.HitsPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) // #nosec G302,G304 -- local, gitignored telemetry in the store the invoker named
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	line, err := json.Marshal(Hit{Note: name, At: at.UTC()})
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Close()
}

// Hits reads the log. A malformed line is skipped rather than fatal — the
// file is append-only local telemetry and a torn write must not take out
// `stats`.
func (s *Store) Hits() ([]Hit, error) {
	f, err := os.Open(s.HitsPath()) // #nosec G304 -- reads the store's own hits log
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", s.HitsPath(), err)
	}
	defer func() { _ = f.Close() }()

	var hits []Hit
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var h Hit
		if err := json.Unmarshal(sc.Bytes(), &h); err != nil {
			continue
		}
		hits = append(hits, h)
	}
	if err := sc.Err(); err != nil {
		return hits, fmt.Errorf("scan %s: %w", s.HitsPath(), err)
	}
	return hits, nil
}
