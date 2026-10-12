package usage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ParseSession reads the main transcript at mainPath and every subagent
// transcript beside it, and returns their rows together.
//
// Subagent transcripts are the regular files named agent-*.jsonl in the
// subagents directory of the session, which is mainPath with its .jsonl
// suffix removed. The main transcript's rows come first, then each subagent
// file's in filename order. Held and Skipped are summed across the files. A
// session with no subagents directory is the main transcript alone.
//
// A file that cannot be read does not stop the others: the rows of every file
// that parsed come back beside an error joining one failure per file, each
// naming its path.
//
// Every returned row, main transcript and subagent alike, carries the one
// Identity ResolveIdentity gives for the session's cwd under os.LookupEnv.
// The session's cwd is the first non-empty Row.Cwd in row order, so the main
// transcript's wins and a subagent's stands in only when the main transcript
// yields none.
func ParseSession(mainPath string, opts Options) (Result, error) {
	var res Result
	var errs []error

	paths := []string{mainPath}
	subs, err := subagentPaths(strings.TrimSuffix(mainPath, ".jsonl"))
	if err != nil {
		errs = append(errs, err)
	}
	paths = append(paths, subs...)

	for _, p := range paths {
		r, err := ParseFile(p, opts)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			continue
		}
		res.Rows = append(res.Rows, r.Rows...)
		res.Held = append(res.Held, r.Held...)
		res.Skipped += r.Skipped
	}

	id := ResolveIdentity(sessionCwd(res.Rows), os.LookupEnv)
	for i := range res.Rows {
		res.Rows[i].Identity = id
	}
	return res, errors.Join(errs...)
}

// sessionCwd is the first non-empty Cwd among rows, or "" when none has one.
func sessionCwd(rows []Row) string {
	for _, r := range rows {
		if r.Cwd != "" {
			return r.Cwd
		}
	}
	return ""
}

// subagentPaths lists the subagent transcripts of the session in sessionDir,
// sorted by filename. A missing subagents directory yields none.
func subagentPaths(sessionDir string) ([]string, error) {
	dir := filepath.Join(sessionDir, "subagents")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if ok, _ := filepath.Match("agent-*.jsonl", e.Name()); ok {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	paths := make([]string, len(names))
	for i, n := range names {
		paths[i] = filepath.Join(dir, n)
	}
	return paths, nil
}
