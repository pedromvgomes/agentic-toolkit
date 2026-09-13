package tests

import (
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

// A filename is a claim about a file, not about the change made to it. The
// most expensive panel must not be bought by renaming a settings file.
func TestAnAuthPathWithoutAuthContentDoesNotFire(t *testing.T) {
	r := newRepo(t)
	r.write("seed.txt", "x\n")
	base := r.commit("base")

	// The path matches `**/*guard*`; nothing in the body is about gating a
	// request. A file's name is not what the change did to it.
	r.write("config/guard-settings.yaml", "name: guard-settings\nvalue:\n  timeout: 30\n")
	p := buildProfile(t, r, base)

	if has, _ := p.Signals.Has(review.SignalAuth); has {
		t.Errorf("auth fired on a filename alone; signals were %s", p.Signals)
	}
}
