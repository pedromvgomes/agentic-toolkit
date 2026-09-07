package review

import (
	"fmt"
	"regexp"
	"strings"
)

// countReferencingFiles measures how widely used the changed code is.
//
// It counts distinct files mentioning the exported symbols the change
// modifies, with grep — which is what `sizing.md` used, and what makes the
// measure available in any repo without a language server running. A precision
// upgrade behind the same key changes nothing a manifest can see.
//
// A language with no extractor makes the count unavailable, naming the
// language. Unavailable is never low: a change in a language the toolkit
// cannot read symbols out of has an unknown blast radius, not a small one, and
// a rule reading it is refused rather than quietly passed over.
func countReferencingFiles(opts ProfileOptions, files []ChangedFile, patch string) ([]string, Count) {
	if len(files) == 0 {
		return nil, AvailableCount(0)
	}

	// Every language present has to be readable, not just one of them. A
	// count taken over the half of a polyglot change the toolkit understands
	// would be a number that looks like a blast radius and is not.
	var unreadable []string
	seen := map[Language]bool{}
	for _, f := range files {
		if seen[f.Language] {
			continue
		}
		seen[f.Language] = true
		if !SymbolsCountable(f.Language) {
			unreadable = append(unreadable, languageLabel(f.Language))
		}
	}
	if len(unreadable) > 0 {
		return nil, UnavailableCount("no symbol extractor for %s", strings.Join(unreadable, ", "))
	}

	symbols := changedSymbols(files, patch, opts.symbolBudget())
	if len(symbols) == 0 {
		// Every language was readable and nothing exported changed. That is a
		// real answer: nothing outside can be reaching what this change moved.
		return nil, AvailableCount(0)
	}

	total := 0
	for _, sym := range symbols {
		n, err := countSymbolReferences(opts.Dir, sym, files)
		if err != nil {
			return symbols, UnavailableCount("counting references to %s failed: %v", sym, err)
		}
		if n > total {
			total = n
		}
	}
	return symbols, AvailableCount(total)
}

// languageLabel names a language for a diagnostic, including the one that has
// no name.
func languageLabel(lang Language) string {
	if lang == LangUnknown {
		return "unrecognised files"
	}
	return string(lang)
}

// changedSymbols reads the exported names the change declares, newest-widest
// first is not knowable here so declaration order is kept, capped at budget.
func changedSymbols(files []ChangedFile, patch string, budget int) []string {
	byPath := make(map[string]Language, len(files))
	for _, f := range files {
		byPath[f.Path] = f.Language
	}

	seen := map[string]bool{}
	var out []string
	walkPatch(patch, func(file, line string) {
		lang, ok := byPath[file]
		if !ok || len(out) >= budget {
			return
		}
		for _, sym := range ExportedSymbols(lang, line) {
			if seen[sym] || len(out) >= budget {
				continue
			}
			seen[sym] = true
			out = append(out, sym)
		}
	})
	return out
}

// countSymbolReferences counts distinct files mentioning sym, not counting the
// files the change itself touched — a symbol's own definition and its
// neighbours are not downstream users.
//
// The symbol reaches git as a pattern, so it is checked against symbolNameRE
// first: under a PR review the names come out of a diff somebody else wrote,
// and a name is only ever counted when it is a plain identifier.
func countSymbolReferences(dir, sym string, changed []ChangedFile) (int, error) {
	if !isSymbolName(sym) {
		return 0, nil
	}
	// -w matches whole words, -l lists files rather than lines, and the
	// pattern is passed with -e so a name beginning with a dash could never
	// be read as an option.
	out, err := git(dir, "grep", "--no-color", "-l", "-w", "-e", regexp.QuoteMeta(sym), "--")
	if err != nil {
		// git grep exits 1 with no output when nothing matched, which is an
		// answer of zero rather than a failure.
		if strings.TrimSpace(string(out)) == "" {
			return 0, nil
		}
		return 0, err
	}

	touched := make(map[string]bool, len(changed))
	for _, f := range changed {
		touched[f.Path] = true
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" || touched[line] {
			continue
		}
		count++
	}
	return count, nil
}

// fixCommitRE is what marks a commit as a deliberate repair. Matched against
// the subject line, where the convention that carries this information lives.
var fixCommitRE = regexp.MustCompile(`(?i)\b(fix|bug|hotfix|revert|security|cve)\b`)

// historyDepthPerHunk is how far back one region's history is read. A repair
// that a change is undoing is the recent history of those exact lines; a
// commit twenty rewrites ago is not what the signal is about.
const historyDepthPerHunk = 5

// detectFixRevert traces the regions a change deleted or rewrote back to the
// commits that wrote them, and fires when one of those was a repair.
//
// It addresses lines, not files. "Has this file ever been fixed" is true of
// nearly every file in a mature repo, so a signal built on it fires always and
// means nothing; "were these lines written by a fix" is the question worth
// asking. Hunks that only add lines are skipped — an added line has no history
// to undo, so blaming it would spend the budget on the half of a change that
// cannot possibly be reverting anything.
//
// Past the budget the signal is marked undetermined rather than left absent. A
// budget that ran out is not evidence that nothing was undone, and a rule that
// read it as such would be a protection that quietly stops working on exactly
// the large changes it exists for.
func detectFixRevert(opts ProfileOptions, files []ChangedFile, patch string, set *SignalSet) {
	basePath := make(map[string]string, len(files))
	for _, f := range files {
		basePath[f.Path] = f.pathAtBase()
	}

	budget := opts.blameBudget()
	spent := 0
	for _, hunk := range Hunks(patch) {
		if hunk.Length == 0 {
			continue
		}
		path, tracked := basePath[hunk.File]
		if !tracked {
			continue
		}
		if spent >= budget {
			set.MarkUndetermined(SignalFixRevert,
				fmt.Sprintf("the %d-hunk history budget ran out", budget))
			return
		}
		spent++

		subjects, err := LineHistory(opts.Dir, opts.Base, path, hunk.Start, hunk.Length, historyDepthPerHunk)
		if err != nil {
			// One unreadable region is not a verdict about the change, but it
			// is not nothing either: the signal stays undetermined unless
			// some other hunk produces real evidence.
			set.MarkUndetermined(SignalFixRevert, "reading the history of "+path+" failed")
			continue
		}
		for _, subject := range subjects {
			if fixCommitRE.MatchString(subject) {
				set.Add(SignalFixRevert)
				return
			}
		}
	}
}

// pathAtBase is the file's name before the change, which is where its history
// is recorded.
func (f ChangedFile) pathAtBase() string {
	if f.OldPath != "" {
		return f.OldPath
	}
	return f.Path
}
