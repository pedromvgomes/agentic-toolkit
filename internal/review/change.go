package review

import (
	"fmt"
	"sort"
	"strings"
)

// ChangedFile is one file in a change, classified.
type ChangedFile struct {
	DiffFile
	// Language is what the file is written in, as far as its name says.
	Language Language
	// Excluded is why the file does not count, or NotExcluded.
	Excluded Exclusion
}

// Reviewable reports whether this file counts toward the change's size and is
// read for signals.
func (f ChangedFile) Reviewable() bool { return f.Excluded == NotExcluded }

// Count is a number that may not exist.
//
// Unavailable is never low. A count that could not be computed is not a small
// count, and an escalation reading it as one is a protection a repo believes
// it has and does not — which is why every read goes through Value, and why a
// rule over an unavailable count is refused rather than evaluated.
type Count struct {
	N         int
	Available bool
	// Reason says why the count is unavailable, in terms a manifest author
	// can act on.
	Reason string
}

// AvailableCount builds a count that exists.
func AvailableCount(n int) Count { return Count{N: n, Available: true} }

// UnavailableCount builds a count that does not, and says why.
func UnavailableCount(format string, args ...interface{}) Count {
	return Count{Reason: fmt.Sprintf(format, args...)}
}

// Value returns the count and whether it exists. A caller that drops ok reads
// unavailable as zero, which is the one reading this type exists to prevent.
func (c Count) Value() (int, bool) { return c.N, c.Available }

// String renders the count for --explain.
func (c Count) String() string {
	if !c.Available {
		return "unavailable (" + c.Reason + ")"
	}
	return fmt.Sprintf("%d", c.N)
}

// Profile is everything a change is, as far as panel selection is concerned:
// what it touched, how much, and what it appears to be about.
//
// It is computed once, before any model runs, and every escalation rule is
// evaluated against this one object. Nothing here needs a provider, which is
// what makes `agtk code-review explain` free to run.
type Profile struct {
	// Files are every changed file, reviewable or not. The excluded ones are
	// kept so --explain can say what was left out and why.
	Files []ChangedFile

	// ChangedLines and ChangedFiles count only the reviewable files.
	ChangedLines int
	ChangedFiles int

	// Signals are the concerns the change appears to touch.
	Signals *SignalSet

	// ReferencingFiles counts the files reaching the exported symbols this
	// change modifies.
	ReferencingFiles Count

	// Symbols are the exported symbols the count was taken over, kept so
	// --explain can name them.
	Symbols []string

	// reviewable caches the filtered slice. Selection asks for it once per
	// `touches` rule and Explain asks again, and refiltering every changed
	// file each time is work that grows with both the change and the manifest.
	//
	// reviewableFor is the length of Files the cache was built from. Files is
	// exported, so a caller can append to it after the cache exists; without
	// that check the appended file would be invisible to every later reader
	// while appearing in Files, which is worse than not caching at all.
	reviewable    []ChangedFile
	reviewableFor int
}

// ReviewableFiles returns the files that count.
func (p *Profile) ReviewableFiles() []ChangedFile {
	if p.reviewable != nil && p.reviewableFor == len(p.Files) {
		return p.reviewable
	}
	out := make([]ChangedFile, 0, len(p.Files))
	for _, f := range p.Files {
		if f.Reviewable() {
			out = append(out, f)
		}
	}
	p.reviewable = out
	p.reviewableFor = len(p.Files)
	return out
}

// ExcludedFiles returns the files that do not count, with their reasons.
func (p *Profile) ExcludedFiles() []ChangedFile {
	out := make([]ChangedFile, 0, len(p.Files))
	for _, f := range p.Files {
		if !f.Reviewable() {
			out = append(out, f)
		}
	}
	return out
}

// Languages lists the languages of the reviewable files, in a stable order.
func (p *Profile) Languages() []Language {
	seen := map[Language]bool{}
	for _, f := range p.ReviewableFiles() {
		seen[f.Language] = true
	}
	out := make([]string, 0, len(seen))
	for lang := range seen {
		out = append(out, string(lang))
	}
	sort.Strings(out)
	langs := make([]Language, 0, len(out))
	for _, name := range out {
		langs = append(langs, Language(name))
	}
	return langs
}

// ProfileOptions bound the work building a profile is allowed to do.
type ProfileOptions struct {
	// Dir is the repository the change lives in.
	Dir string
	// Base and Head bound the change.
	Base, Head string
	// BlameBudget caps how many hunks fix-revert may trace. Past it the
	// signal is undetermined rather than absent: a budget that ran out is not
	// evidence that nothing was undone.
	BlameBudget int
	// SymbolBudget caps how many exported symbols referencing_files counts
	// over. The count is a blast-radius estimate, and the widest few symbols
	// carry it.
	SymbolBudget int
	// Exclude are the manifest's exclusion globs. Passed in rather than read
	// here, because which manifest governs is a question about the context a
	// review runs in, and a profile that answered it a second way would size
	// the change against rules the review was not judged by.
	Exclude []string
}

// Default budgets. Both bound work that grows with the size of a change, on a
// command a person waits for.
const (
	DefaultBlameBudget  = 200
	DefaultSymbolBudget = 10
)

func (o ProfileOptions) blameBudget() int {
	if o.BlameBudget <= 0 {
		return DefaultBlameBudget
	}
	return o.BlameBudget
}

func (o ProfileOptions) symbolBudget() int {
	if o.SymbolBudget <= 0 {
		return DefaultSymbolBudget
	}
	return o.SymbolBudget
}

// BuildProfile reads the change and classifies it.
func BuildProfile(opts ProfileOptions) (*Profile, error) {
	diff, err := DiffRange(opts.Dir, opts.Base, opts.Head)
	if err != nil {
		return nil, err
	}

	// A review of the working tree sees the files the change added. They are
	// untracked, so git diff does not know about them, and a profile built
	// without them reports a smaller change rather than an incomplete one.
	// With a head named, the range is commits, and there is nothing untracked
	// in it by construction.
	untrackedPatch := ""
	if opts.Head == "" {
		untracked, patch, err := collectUntracked(opts.Dir)
		if err != nil {
			return nil, err
		}
		diff = append(diff, untracked...)
		untrackedPatch = patch
	}

	paths := make([]string, 0, len(diff))
	for _, f := range diff {
		paths = append(paths, f.Path)
	}
	// A repo with no .gitattributes is the common case, so an error here
	// costs the repo's own opinion about its generated trees and nothing
	// else. The built-in patterns still apply.
	attrGenerated, _ := AttrGenerated(opts.Dir, paths)

	// The content marker is read for the files the cheaper tests did not
	// already settle, so a change that is mostly a vendored tree does not pay
	// to read it.
	var needHead []string
	for _, d := range diff {
		if Classify(d, attrGenerated, opts.Exclude) == NotExcluded {
			needHead = append(needHead, d.Path)
		}
	}
	heads := FileHeads(opts.Dir, opts.Head, needHead, headBytes)

	p := &Profile{}
	for _, d := range diff {
		f := ChangedFile{DiffFile: d, Language: LanguageOf(d.Path)}
		f.Excluded = Classify(d, attrGenerated, opts.Exclude)
		if f.Excluded == NotExcluded && HasGeneratedMarker(heads[d.Path]) {
			f.Excluded = ExcludedGenerated
		}
		p.Files = append(p.Files, f)
		if f.Reviewable() {
			p.ChangedFiles++
			p.ChangedLines += f.Changed()
		}
	}

	reviewable := p.ReviewableFiles()
	patch, err := Patch(opts.Dir, opts.Base, opts.Head, reviewable)
	if err != nil {
		return nil, err
	}
	patch += untrackedPatch

	p.Signals = detectSignals(reviewable, patch)
	detectFixRevert(opts, reviewable, patch, p.Signals)

	p.Symbols, p.ReferencingFiles = countReferencingFiles(opts, reviewable, patch)
	return p, nil
}

// Summary is the one line --explain leads with.
func (p *Profile) Summary() string {
	parts := []string{
		fmt.Sprintf("%d files, %d lines", p.ChangedFiles, p.ChangedLines),
		"signals: " + p.Signals.String(),
		"referencing files: " + p.ReferencingFiles.String(),
	}
	if excluded := p.ExcludedFiles(); len(excluded) > 0 {
		parts = append(parts, fmt.Sprintf("%d excluded", len(excluded)))
	}
	return strings.Join(parts, " · ")
}
