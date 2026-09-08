package review

// ManifestDir is where a consumer's review manifest and its own prompt bodies
// live, relative to the repo root. Fixed rather than configurable: the
// manifest is configuration, not committed content a repo has an opinion
// about placing, and `agtk` reads the whole directory at a ref rather than a
// file it was pointed at.
const ManifestDir = ".agents/code-review"

// ManifestFile is the manifest's name inside ManifestDir.
const ManifestFile = "manifest.yaml"

// SchemaVersion is the only `version:` this build understands. A manifest
// naming another one is refused rather than read on the assumption that the
// fields it recognises mean what they used to.
const SchemaVersion = 1

// Manifest is a repo's whole review configuration: who reviews, in what
// groupings, and what raises one grouping to another.
//
// A repo with a manifest is using it whole. Prompt bodies stay shareable
// through `builtin:` references rather than through a merge algorithm, so
// there is no layering here and no question about which half of a panel came
// from where.
type Manifest struct {
	Version   int               `yaml:"version"   agtkdoc:"required;Manifest schema version. Currently always 1."`
	Reviewers map[string]Runner `yaml:"reviewers" agtkdoc:"required;The reviewers this repo can staff a panel with, keyed by name. The name is what a panel lists and what a finding is attributed to."`
	Judge     *Runner           `yaml:"judge,omitempty"     agtkdoc:"required;The single run that merges findings, sets final severity and decides which survive. It decides; it does not transmit."`
	Validator *Runner           `yaml:"validator,omitempty" agtkdoc:"required;The run handed one candidate finding and asked whether it holds. A context that posts always validates, so this is required whatever the panels say."`
	Panels    map[string]Panel  `yaml:"panels"    agtkdoc:"required;Named sets of reviewers, keyed by name. Exactly one panel runs per review."`
	Defaults  Defaults          `yaml:"defaults"  agtkdoc:"required;The panel each context starts from, before escalation."`
	Escalate  []Escalation      `yaml:"escalate,omitempty" agtkdoc:"Rules that raise the panel above a context's default. Every rule is evaluated and the highest target wins, so their order carries no meaning."`
	Approval  Approval          `yaml:"approval,omitempty" agtkdoc:"What approving a reviewed head requires of a finding's severity. Absent means the default floor, AMBER."`

	// Conventions replaces the default rule documents rather than adding to
	// them. A repo that names its own has said where its rules live, and
	// appending the defaults would hold it against documents it did not name.
	Conventions []string `yaml:"conventions,omitempty" agtkdoc:"Documents holding this repo's own written rules, as paths from the repo root, read at the base ref and injected raw into every reviewer's prompt. Replaces the default list rather than adding to it. Absent means the defaults: CLAUDE.md, AGENTS.md, .claude/CLAUDE.md, CONTEXT.md, CONTRIBUTING.md, docs/ARCHITECTURE.md, docs/CODE_STANDARDS.md."`

	// Builtin records that this is the manifest that ships with agtk rather
	// than one a repo wrote. Not a field a manifest may set: it is a fact
	// about where the document came from.
	//
	// It decides what an unreadable condition means. A rule a repo wrote is a
	// protection it asked for, so a change the rule cannot be evaluated
	// against is a refusal. A rule in the built-in default was never asked
	// for, and refusing there turns a language the toolkit does not recognise
	// into a review that cannot run at all — in exactly the repos with no
	// manifest to edit, and with no way to take the advice the refusal gives.
	Builtin bool `yaml:"-"`
}

// Approval is what this repo requires before a reviewed head may be approved.
//
// The floor is the only thing here that a repo gets a say in. Everything else
// approval requires — a review of the current head that reached a verdict,
// every finding at or above the floor answered in writing, every thread
// resolved — is the control itself, and a manifest that could weaken it would
// be a --force written in YAML.
type Approval struct {
	// Floor is the severity at which a finding obliges an answer: a fix, or a
	// written statement on its thread that it is not a defect.
	//
	// AMBER by default, because RED and AMBER are both defects and differ in
	// the strength of the claim rather than in what they oblige. A repo that
	// wants only RED to oblige a fix says so here.
	Floor Severity `yaml:"floor,omitempty" agtkdoc:"Severity at or above which a finding must be fixed or marked a false positive before an approval is granted: RED, AMBER or GREEN. Defaults to AMBER, since RED and AMBER are both defects and differ in the strength of the claim rather than in what they oblige."`
}

// DefaultApprovalFloor is the floor a manifest that names none is read as
// naming.
const DefaultApprovalFloor = SeverityAmber

// EffectiveFloor is the floor this manifest sets, or the default.
func (a Approval) EffectiveFloor() Severity {
	if a.Floor == "" {
		return DefaultApprovalFloor
	}
	return a.Floor
}

// Runner is one configured model run — the shape a reviewer, the judge
// and the validator all take. It says which CLI, which model and which prompt,
// and nothing about what the run may do: every run here is read-only, and how
// that is enforced is a fact about the provider rather than something a
// manifest gets to weaken.
type Runner struct {
	Provider string    `yaml:"provider" agtkdoc:"required;Coding-agent CLI this run is driven through, e.g. \"claudecode\" or \"codex\"."`
	Model    string    `yaml:"model,omitempty" agtkdoc:"Model to run, by family alias (\"opus\", \"sonnet\") or exact name. Empty leaves the CLI's own default."`
	Prompt   PromptRef `yaml:"prompt" agtkdoc:"required;The prompt body: \"builtin:<name>\" for one that ships with agtk, or \"./<path>\" for one in this repo's review manifest directory."`
}

// Panel is a named set of reviewers and how hard they are run.
type Panel struct {
	// Description says what this panel is for, in the manifest author's own
	// words.
	//
	// A panel's cost is derivable — reviewers times quorum — and says what it
	// spends, never what it is for. A person choosing between `standard` and
	// `deep` is choosing on the second, and a name plus a run count does not
	// carry it. Optional: a manifest that omits it is listed by name and cost,
	// which is what a panel that has always been obvious needs.
	Description string   `yaml:"description,omitempty" agtkdoc:"What this panel is for, in one line. Shown when a panel is listed or chosen, because a name and a run count say what a panel spends and not what it is for."`
	Reviewers   []string `yaml:"reviewers" agtkdoc:"required;Names from the manifest's reviewers map. A panel that names one this manifest does not declare cannot staff itself, and is refused."`
	Quorum      int      `yaml:"quorum,omitempty" agtkdoc:"How many independent instances of each reviewer to run. Agreement between them is the confidence signal. Defaults to 1."`
	Validate    *bool    `yaml:"validate,omitempty" agtkdoc:"Whether findings are put to the validator. Unset leaves it to the context, and a context that posts validates regardless: a false finding on a PR is published and blocks approval."`

	// Judge and Validator override the manifest's own, for reviews this panel
	// produces.
	//
	// A panel is how one context's reviewers are chosen, so it is also where
	// the run that reconciles them belongs. Without this, a repo reviewing
	// locally with one provider and its pull requests with another can say so
	// for its reviewers and not for the judge, and the judge runs in every
	// review — so the choice would be made once for both contexts by whichever
	// one was written down.
	//
	// An override rather than a requirement: the manifest's own judge is what
	// a panel that says nothing uses, so declaring these on every panel is
	// never the price of declaring them on one.
	Judge     *Runner `yaml:"judge,omitempty"     agtkdoc:"Judge for reviews this panel produces, instead of the manifest's. Unset uses the manifest's."`
	Validator *Runner `yaml:"validator,omitempty" agtkdoc:"Validator for reviews this panel produces, instead of the manifest's. Unset uses the manifest's."`
}

// EffectiveJudge is the judge that reconciles a review the named panel
// produced: the panel's own, or the manifest's.
//
// Resolution has one home because the fallback is a rule rather than a
// convenience. Two callers reading `panel.Judge` and deciding for themselves
// is two chances to read the manifest's judge where a panel had overridden it,
// and a review judged by the wrong provider says nothing about it in its
// output.
func (m *Manifest) EffectiveJudge(panel string) *Runner {
	if p, ok := m.Panels[panel]; ok && p.Judge != nil {
		return p.Judge
	}
	return m.Judge
}

// EffectiveValidator is the validator a review the named panel produced puts
// its candidate findings to: the panel's own, or the manifest's. Nil when
// neither declares one.
func (m *Manifest) EffectiveValidator(panel string) *Runner {
	if p, ok := m.Panels[panel]; ok && p.Validator != nil {
		return p.Validator
	}
	return m.Validator
}

// EffectiveQuorum is Quorum, or 1 when the panel does not set one.
func (p Panel) EffectiveQuorum() int {
	if p.Quorum < 1 {
		return 1
	}
	return p.Quorum
}

// Defaults names the panel each context starts from.
type Defaults struct {
	Worktree string `yaml:"worktree" agtkdoc:"required;Panel a local working-tree review starts from."`
	PR       string `yaml:"pr"       agtkdoc:"required;Panel a pull-request review starts from."`
}

// Context is what a review runs against. It picks a default panel, and it
// decides what a run is obliged to do rather than what it may.
type Context string

const (
	// ContextWorktree is a review of the local working tree, reported to the
	// terminal.
	ContextWorktree Context = "worktree"
	// ContextPR is a review of an open pull request, posted to it. A context
	// that posts always validates.
	ContextPR Context = "pr"
)

// Contexts are the contexts a review can run in, in the order help lists them.
var Contexts = []Context{ContextWorktree, ContextPR}

// Posts reports whether a review in this context is published where a false
// finding blocks approval, rather than printed where it merely clutters a
// terminal. It is what forces validation on.
func (c Context) Posts() bool { return c == ContextPR }

// Default returns the panel this context starts from.
func (d Defaults) Default(c Context) string {
	switch c {
	case ContextWorktree:
		return d.Worktree
	case ContextPR:
		return d.PR
	}
	return ""
}

// Escalation raises the panel above a context's default when a change meets
// its condition. Rules only ever raise, so a mistaken rule costs money and
// never yields a shallower review than the default.
//
// Exactly one of All or Any is set, named on the rule. Nesting them freely
// would put the combinator in the shape of the document rather than in a word
// the author wrote, which is the defect the whole grammar exists to avoid.
type Escalation struct {
	To  string      `yaml:"to" agtkdoc:"required;Panel to raise to when this rule fires."`
	All []Condition `yaml:"all,omitempty" agtkdoc:"Conditions that must all hold. Exactly one of all: or any: is set on a rule."`
	Any []Condition `yaml:"any,omitempty" agtkdoc:"Conditions of which at least one must hold. Exactly one of all: or any: is set on a rule."`
}

// Conditions returns the rule's clauses and whether they are conjoined,
// without the caller having to know which field carried them.
func (e Escalation) Conditions() (conds []Condition, all bool) {
	if len(e.All) > 0 {
		return e.All, true
	}
	return e.Any, false
}

// PromptKind distinguishes the two places a prompt body comes from.
type PromptKind int

const (
	// PromptBuiltin names a prompt that ships in the binary: `builtin:<name>`.
	PromptBuiltin PromptKind = iota
	// PromptPath names a file in the repo's review manifest directory,
	// cleaned and guaranteed not to climb out of it.
	PromptPath
)

func (k PromptKind) String() string {
	switch k {
	case PromptBuiltin:
		return "builtin"
	case PromptPath:
		return "path"
	}
	return "unknown"
}

// builtinPrefix marks a prompt that ships with agtk.
const builtinPrefix = "builtin:"

// PromptRef is one parsed `prompt:` value.
//
// Repo-local paths resolve against the review manifest's own directory rather
// than the repo root, so a manifest and the prompts it names travel as one
// tree — which is what lets agtk read both from the base ref, and a branch
// rewrite neither.
type PromptRef struct {
	// Raw is the verbatim string from the YAML, kept for diagnostics.
	Raw string
	// Kind disambiguates the two resolution paths.
	Kind PromptKind
	// Name is set for PromptBuiltin.
	Name string
	// Path is set for PromptPath, relative to the manifest's directory.
	Path string
}

// IsBuiltin reports whether this prompt ships with agtk.
func (p PromptRef) IsBuiltin() bool { return p.Kind == PromptBuiltin }

// ConventionDocs returns the documents this repo is held against.
//
// A manifest that names its own replaces the defaults rather than extending
// them: a repo that has said where its rules live has also said where they do
// not, and appending would hold it against documents it did not name and may
// not have meant as rules.
func (m *Manifest) ConventionDocs(defaults []string) []string {
	if len(m.Conventions) > 0 {
		return m.Conventions
	}
	return defaults
}
