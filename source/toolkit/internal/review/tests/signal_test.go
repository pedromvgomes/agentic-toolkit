package tests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/review"
)

// Every signal in the vocabulary is proven reachable. A signal that can never
// fire is a protection a repo believes it has and does not, and the glob and
// regex tables behind them are data no other test exercises.
func TestEverySignalCanFire(t *testing.T) {
	cases := []struct {
		signal review.Signal
		path   string
		body   string
	}{
		// Content, not just the path: `auth`'s globs are the broadest in the
		// table, so a name alone no longer establishes it.
		{review.SignalAuth, "internal/auth/token.go", "package auth\n\nfunc Authorize() {}\n"},
		{review.SignalMigrations, "db/migrations/0001_init.sql", "ALTER TABLE users ADD COLUMN email text;\n"},
		{review.SignalSharedKernel, "pkg/util/util.go", "package util\n"},
		{review.SignalPublicAPI, "api/service.proto", "message Request {}\n"},
		{review.SignalMessageConsumers, "jobs/consumer.go", "package jobs\n"},
		{review.SignalCICD, ".github/workflows/ci.yml", "on: push\n"},
		{review.SignalIaC, "infra/main.tf", "resource \"aws_s3_bucket\" \"b\" {}\n"},
		{review.SignalConcurrency, "worker.go", "package main\n\nimport \"sync\"\n\nvar mu sync.Mutex\n"},
		{review.SignalSensitiveData, "billing/charge.go", "package billing\n"},
		{review.SignalCrypto, "crypto/keys.go", "package crypto\n"},
		{review.SignalFeatureFlags, "flags/flags.go", "package flags\n"},
	}

	for _, tc := range cases {
		t.Run(string(tc.signal), func(t *testing.T) {
			r := newRepo(t)
			r.write("seed.txt", "x\n")
			base := r.commit("base")

			r.write(tc.path, tc.body)
			p := buildProfile(t, r, base)

			has, known := p.Signals.Has(tc.signal)
			if !known {
				t.Fatalf("signal %s could not be determined: %s", tc.signal, p.Signals.Undetermined(tc.signal))
			}
			if !has {
				t.Errorf("signal %s did not fire for %s; signals were %s", tc.signal, tc.path, p.Signals)
			}
		})
	}
}

// fix-revert is the one signal that reads history rather than the change, so
// it gets its own case: it must fire on lines a repair wrote and stay silent
// on lines merely near them.
func TestFixRevertFiresOnRepairedLinesOnly(t *testing.T) {
	r := newRepo(t)
	r.write("pkg/lock.go", "package pkg\n\nimport \"sync\"\n\nvar mu sync.RWMutex\n")
	base := r.commit("revert: restore the locking")

	t.Run("rewriting the repaired line fires", func(t *testing.T) {
		r.write("pkg/lock.go", "package pkg\n\nimport \"sync\"\n\nvar mu sync.Mutex\n")
		p := buildProfile(t, r, base)
		if has, _ := p.Signals.Has(review.SignalFixRevert); !has {
			t.Errorf("fix-revert did not fire; signals were %s", p.Signals)
		}
	})

	// A hunk header's pre-image length counts only removed lines, so a comment
	// added beside repaired code blames nothing. With context lines included
	// it would blame the repair three lines either side of every edit.
	t.Run("a comment added beside it does not", func(t *testing.T) {
		r.write("pkg/lock.go", "package pkg\n\n// an explanatory note\nimport \"sync\"\n\nvar mu sync.RWMutex\n")
		p := buildProfile(t, r, base)
		if has, _ := p.Signals.Has(review.SignalFixRevert); has {
			t.Errorf("fix-revert fired on lines no repair wrote; signals were %s", p.Signals)
		}
	})

	// A deleted file's lines belong to the deleted file. Attributing them to
	// whichever file preceded it in the patch loses the deletion's own history.
	t.Run("deleting the repaired file fires", func(t *testing.T) {
		r.git("checkout", "--", ".")
		r.write("other.go", "package pkg\n")
		r.commit("add a neighbour")
		r.git("rm", "-q", "pkg/lock.go")

		p := buildProfile(t, r, base)
		if has, _ := p.Signals.Has(review.SignalFixRevert); !has {
			t.Errorf("fix-revert did not fire on a deletion; signals were %s", p.Signals)
		}
		if has, _ := p.Signals.Has(review.SignalConcurrency); !has {
			t.Errorf("deleting locking code is a change to locking; signals were %s", p.Signals)
		}
	})
}

// The two history signals name different things. A revert or a security repair
// is worth the deepest reading, because undoing one reinstates a defect
// somebody removed on purpose. A routine `fix(scope):` is not: under
// Conventional Commits it is a type prefix on a large share of every subject
// line, so treating it the same would escalate nearly every branch in a mature
// repo and distinguish nothing.
func TestARoutineFixIsBugfixLinesAndNotFixRevert(t *testing.T) {
	r := newRepo(t)
	r.write("pkg/svc.go", "package pkg\n\nvar limit = 10\n")
	base := r.commit("fix(pkg): raise the limit")

	r.write("pkg/svc.go", "package pkg\n\nvar limit = 20\n")
	p := buildProfile(t, r, base)

	if has, known := p.Signals.Has(review.SignalBugfixLines); !has || !known {
		t.Errorf("bugfix-lines = (%v, known=%v), want present; signals were %s", has, known, p.Signals)
	}
	if has, _ := p.Signals.Has(review.SignalFixRevert); has {
		t.Errorf("a routine fix raised fix-revert, which escalates every branch in a repo using Conventional Commits; signals were %s", p.Signals)
	}
}

// A fragment in a filename is a claim about the file, not about the change
// made to it. The most expensive panel must not be bought by naming a settings
// file for a thing it configures.
func TestAnAuthNameFragmentWithoutAuthContentDoesNotFire(t *testing.T) {
	r := newRepo(t)
	r.write("seed.txt", "x\n")
	base := r.commit("base")

	r.write("config/guard-settings.yaml", "name: guard-settings\nvalue:\n  timeout: 30\n")
	p := buildProfile(t, r, base)

	if has, _ := p.Signals.Has(review.SignalAuth); has {
		t.Errorf("auth fired on a name fragment alone; signals were %s", p.Signals)
	}
}

// A directory that is what it is called is evidence on its own. The dangerous
// edit to auth code is the one that removes a check, and removing a check
// removes the words that name it — so a rule keyed on content alone would go
// quiet on exactly the change it exists for.
func TestAuthFiresOnAnAuthDirectoryWithNoAuthKeyword(t *testing.T) {
	r := newRepo(t)
	r.write("internal/auth/middleware.go",
		"package auth\n\nfunc Check(admin bool) error {\n\tif !admin {\n\t\treturn errForbidden\n\t}\n\treturn nil\n}\n")
	base := r.commit("base")

	// The guard is deleted. Nothing left in the diff says "authorize".
	r.write("internal/auth/middleware.go",
		"package auth\n\nfunc Check(admin bool) error {\n\treturn nil\n}\n")
	p := buildProfile(t, r, base)

	if has, known := p.Signals.Has(review.SignalAuth); !has || !known {
		t.Errorf("auth = (%v, known=%v) for a check deleted under internal/auth/; signals were %s", has, known, p.Signals)
	}
}

// The dangerous edit to a gate is the one that deletes the check, and deleting
// a check deletes the words that name it — so a signal resting on content
// alone goes quiet on exactly the change it exists for. Middleware and guards
// are frequently not under an `auth/` directory, which is why the filename has
// to carry it.
func TestAuthFiresOnAGatingFileOutsideAnAuthDirectory(t *testing.T) {
	for _, tc := range []struct{ name, path, before, after string }{
		{
			name:   "go middleware",
			path:   "server/middleware.go",
			before: "package server\n\nfunc Check(admin bool) error {\n\tif !admin {\n\t\treturn errForbidden\n\t}\n\treturn nil\n}\n",
			after:  "package server\n\nfunc Check(admin bool) error {\n\treturn nil\n}\n",
		},
		{
			name:   "typescript guard",
			path:   "guards/admin_guard.ts",
			before: "export function check(isAdmin: boolean) {\n  if (!isAdmin) {\n    throw new Error('forbidden');\n  }\n}\n",
			after:  "export function check(isAdmin: boolean) {\n}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepo(t)
			r.write(tc.path, tc.before)
			base := r.commit("base")

			r.write(tc.path, tc.after)
			p := buildProfile(t, r, base)

			if has, known := p.Signals.Has(review.SignalAuth); !has || !known {
				t.Errorf("auth = (%v, known=%v) for a check deleted from %s; signals were %s",
					has, known, tc.path, p.Signals)
			}
		})
	}
}

// The same fragment on a file that is read rather than called stays silent. A
// file named for a thing it configures is describing a gate, not being one.
func TestAGatingNameOnAConfigFileDoesNotFire(t *testing.T) {
	r := newRepo(t)
	r.write("seed.txt", "x\n")
	base := r.commit("base")

	r.write("config/guard-settings.yaml", "name: guard-settings\nvalue:\n  timeout: 30\n")
	p := buildProfile(t, r, base)

	if has, _ := p.Signals.Has(review.SignalAuth); has {
		t.Errorf("auth fired on a config file's name; signals were %s", p.Signals)
	}
}

// A gate written in a language the table does not carry is still a gate. The
// fragment rule must fail toward firing, or the protection is missing exactly
// where the toolkit's knowledge is and nothing reports it.
func TestAGatingFileFiresInALanguageTheTableDoesNotKnow(t *testing.T) {
	for _, tc := range []struct{ name, path, before, after string }{
		{
			name:   "elixir plug",
			path:   "lib/auth_plug.ex",
			before: "defmodule AuthPlug do\n  def call(conn) do\n    if conn.admin, do: conn, else: halt(conn)\n  end\nend\n",
			after:  "defmodule AuthPlug do\n  def call(conn) do\n    conn\n  end\nend\n",
		},
		{
			// Shell carries NoSymbols only because nothing reads symbols out
			// of it. A shell script runs.
			name:   "shell guard",
			path:   "scripts/deploy-guard.sh",
			before: "#!/bin/sh\nif [ \"$ROLE\" != admin ]; then\n  exit 1\nfi\n",
			after:  "#!/bin/sh\nexit 0\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepo(t)
			r.write(tc.path, tc.before)
			base := r.commit("base")

			r.write(tc.path, tc.after)
			p := buildProfile(t, r, base)

			if has, known := p.Signals.Has(review.SignalAuth); !has || !known {
				t.Errorf("auth = (%v, known=%v) for a check deleted from %s; signals were %s",
					has, known, tc.path, p.Signals)
			}
		})
	}
}

// The tail cutoff: once a routine repair has answered, the search for a revert
// runs a bounded distance further and then reports undetermined.
//
// Undetermined rather than absent is the whole point. A scan that stopped
// looking and a scan that looked everywhere both produce "no revert found",
// and only one of them licenses skipping the deeper panel.
func TestTheRevertSearchStopsAndSaysSoRatherThanReportingAbsent(t *testing.T) {
	const files = 60

	r := newRepo(t)
	for i := 0; i < files; i++ {
		r.write(fmt.Sprintf("pkg/f%02d.go", i), fmt.Sprintf("package pkg\n\nvar v%02d = 1\n", i))
	}
	// Every line in the change traces to this one subject: a routine repair,
	// and nowhere a revert.
	base := r.commit("fix(pkg): correct the values")

	for i := 0; i < files; i++ {
		r.write(fmt.Sprintf("pkg/f%02d.go", i), fmt.Sprintf("package pkg\n\nvar v%02d = 2\n", i))
	}
	p := buildProfile(t, r, base)

	if has, known := p.Signals.Has(review.SignalBugfixLines); !has || !known {
		t.Errorf("bugfix-lines = (%v, known=%v), want present; signals were %s", has, known, p.Signals)
	}

	has, known := p.Signals.Has(review.SignalFixRevert)
	if has {
		t.Fatalf("fix-revert fired with no revert anywhere in the history; signals were %s", p.Signals)
	}
	if known {
		t.Errorf("fix-revert reported absent after the search stopped early; a scan that stopped looking must not read as one that looked: %s", p.Signals)
	}
	if reason := p.Signals.Undetermined(review.SignalFixRevert); !strings.Contains(reason, "revert") {
		t.Errorf("undetermined reason does not say the revert search stopped: %q", reason)
	}
}
