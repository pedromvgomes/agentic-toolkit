package review

import (
	"regexp"
	"strings"
)

// universalContent holds the patterns that evidence a signal whatever the
// file is written in.
//
// A word like `authorize` or `LaunchDarkly` means the same thing in every
// language, and a per-language copy of it would be twelve places to forget.
// What belongs in the language table is syntax — `sync.Mutex`, `@Transactional`
// — where the spelling is the language's, not the concern's.
var universalContent = map[Signal][]*regexp.Regexp{
	SignalAuth: {
		re(`(?i)\bauthenticat`), re(`(?i)\bauthoriz`), re(`(?i)\bjwt\b`),
		re(`(?i)\boauth`), re(`(?i)\bbearer\b`), re(`(?i)\bcsrf\b`),
		re(`(?i)\brbac\b`), re(`(?i)\bpermission`), re(`(?i)\bsession\b`),
		re(`(?i)\bapi[_-]?key\b`), re(`(?i)\baccess[_-]?token\b`),
	},
	SignalCrypto: {
		re(`(?i)\bencrypt`), re(`(?i)\bdecrypt`), re(`(?i)\bprivate[_-]?key\b`),
		re(`(?i)\bhmac\b`), re(`(?i)\bsha256\b`), re(`(?i)\bbcrypt\b`),
		re(`(?i)\bargon2\b`), re(`(?i)\bpbkdf2\b`), re(`\bTLS\b`), re(`(?i)\bx509\b`),
	},
	SignalSensitiveData: {
		re(`(?i)\bssn\b`), re(`(?i)\bcredit[_-]?card`), re(`(?i)\bcard[_-]?number`),
		re(`(?i)\bcvv\b`), re(`(?i)\biban\b`), re(`(?i)\bpii\b`),
		re(`(?i)\bdate[_-]?of[_-]?birth`), re(`(?i)\bpassword\b`),
		re(`(?i)\bbilling\b`), re(`(?i)\bstripe\b`),
	},
	SignalFeatureFlags: {
		re(`(?i)\bfeature[_-]?flag`), re(`(?i)\bfeature[_-]?toggle`),
		re(`(?i)\blaunchdarkly\b`), re(`(?i)\bunleash\b`), re(`(?i)\bsplit\.io\b`),
		re(`(?i)\bkill[_-]?switch\b`), re(`(?i)\bis[_-]?enabled\b`),
	},
	SignalMessageConsumers: {
		re(`(?i)\bconsumer\b`), re(`(?i)\bsubscriber\b`), re(`(?i)\bkafka\b`),
		re(`(?i)\brabbitmq\b`), re(`(?i)\bsqs\b`), re(`(?i)\bpubsub\b`),
		re(`(?i)\bcron\b`), re(`(?i)\bscheduler\b`),
	},
	SignalMigrations: {
		re(`(?i)\bALTER TABLE\b`), re(`(?i)\bCREATE TABLE\b`),
		re(`(?i)\bADD COLUMN\b`), re(`(?i)\bDROP COLUMN\b`),
	},
	SignalPublicAPI: {
		re(`(?i)\bopenapi\b`), re(`(?i)\bgraphql\b`), re(`(?i)\bgrpc\b`),
	},
}

// signalPaths maps a signal to the path globs that evidence it.
//
// Paths are the weaker half of the reading and the half a repo can already
// express itself through `touches`. They are here so a signal fires on a file
// whose name says what it is even when the diff's own lines are unremarkable —
// a one-line edit inside `db/migrations/` is a migration change however
// ordinary the line looks.
var signalPaths = map[Signal][]string{
	// Directories that are what they are called. A file under `auth/` is auth
	// code whatever the diff says, so an edit that removes a check without
	// naming one — deleting `if !user.IsAdmin() { return ErrForbidden }` —
	// still raises the signal.
	//
	// The name-fragment globs that used to sit here (`**/*permission*`,
	// `**/*middleware*`, `**/*guard*`, `**/*auth*`) are deliberately absent.
	// A fragment in a filename is a claim about the file, not about the change
	// made to it, and it bought the most expensive panel for a settings file
	// that merely says "permissions". Those files are still read by the
	// content pass, which decides on what the change actually says.
	SignalAuth: {
		"**/auth/**", "**/authn/**", "**/authz/**",
		"**/session/**", "**/sessions/**",
	},
	SignalMigrations: {
		"**/migrations/**", "**/migrate/**", "**/db/migrate/**",
		"**/changelog/**", "**/*.sql", "**/liquibase/**", "**/flyway/**",
	},
	SignalSharedKernel: {
		"common/**", "shared/**", "lib/**", "libs/**", "pkg/**", "core/**",
		"**/common/**", "**/shared/**", "**/core/**",
	},
	SignalPublicAPI: {
		"**/*.proto", "**/*.graphql", "**/*.graphqls", "**/*.avsc",
		"**/openapi*", "**/swagger*", "api/**", "**/api/**", "**/*.thrift",
	},
	SignalMessageConsumers: {
		"**/*consumer*", "**/*subscriber*", "**/*listener*", "**/*worker*",
		"**/jobs/**", "**/*scheduler*", "**/*cron*",
	},
	SignalCICD: {
		".github/workflows/**", ".github/actions/**", ".gitlab-ci.yml",
		"**/Jenkinsfile*", ".circleci/**", "**/Dockerfile*", "**/*.dockerfile",
		"**/release*.sh", "**/publish*.sh", ".buildkite/**", "azure-pipelines.yml",
	},
	SignalIaC: {
		"**/*.tf", "**/*.tfvars", "**/helm/**", "**/charts/**",
		"**/k8s/**", "**/kubernetes/**", "**/manifests/**",
		"**/cloudformation/**", "**/*.bicep", "**/terraform/**", "**/ansible/**",
	},
	SignalCrypto: {
		"**/crypto/**", "**/*crypt*", "**/*cipher*", "**/keys/**", "**/*keystore*",
	},
	SignalFeatureFlags: {
		"**/*flag*", "**/*toggle*", "**/features/**",
	},
	SignalSensitiveData: {
		"**/billing/**", "**/payment*/**", "**/*payment*", "**/pii/**",
	},
	SignalConcurrency: nil, // Nothing about a path says a change touches locking.
}

// detectSignals reads a change for every signal except fix-revert, which
// needs git history rather than the diff and is gathered separately.
//
// A signal fires on either evidence — a path that says what the file is, or a
// changed line that says what the change did. They are alternatives rather
// than a score, because a signal is a reason to look harder, and one reason is
// enough.
func detectSignals(files []ChangedFile, patch string) *SignalSet {
	set := NewSignalSet()

	for _, f := range files {
		if f.Excluded != NotExcluded {
			continue
		}
		for sig, globs := range signalPaths {
			if !MatchAnyGlob(globs, f.Path) {
				continue
			}
			set.Add(sig)
		}
	}

	byPath := make(map[string]Language, len(files))
	for _, f := range files {
		if f.Excluded == NotExcluded {
			byPath[f.Path] = f.Language
		}
	}

	walkPatch(patch, func(file, line string) {
		lang, reviewable := byPath[file]
		if !reviewable || IsComment(lang, line) {
			return
		}
		for _, sig := range Signals {
			if sig == SignalFixRevert || sig == SignalBugfixLines {
				continue
			}
			if has, _ := set.Has(sig); has {
				continue
			}
			for _, pattern := range contentPatterns(lang, sig) {
				if pattern.MatchString(line) {
					set.Add(sig)
					break
				}
			}
		}
	})

	return set
}

// walkPatch calls fn for every added or removed line in a unified diff,
// together with the file it belongs to.
//
// Only changed lines are read. Context lines are what the change left alone,
// and a signal detected in one would fire on every neighbouring edit to a file
// that happens to contain a mutex somewhere.
//
// A deleted file's lines are delivered under its own name, taken from the
// `---` side, because its `+++` side is `/dev/null`. Deleting the locking a
// repair added is a change to locking, and attributing those lines to
// whichever file happened to come before them in the patch reads them with the
// wrong language and credits them to the wrong path.
func walkPatch(patch string, fn func(file, line string)) {
	var section diffSection
	for _, line := range strings.Split(patch, "\n") {
		if section.track(line) {
			continue
		}
		switch {
		case strings.HasPrefix(line, "@@"):
			// Hunk headers carry no content.
		case section.path() == "":
			// Preamble before the first file header.
		case strings.HasPrefix(line, "+"), strings.HasPrefix(line, "-"):
			fn(section.path(), line[1:])
		}
	}
}
