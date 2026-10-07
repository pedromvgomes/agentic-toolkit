package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cloudinit"
)

func TestTheReportNamesEveryIdentityKeyAndWhereItWent(t *testing.T) {
	home := fakeHome(t)
	repo := newRepo(t)
	gitIn(t, repo, "config", "--local", "user.email", "ada@work.example")
	top := gitIn(t, repo, "rev-parse", "--show-toplevel")

	out, err := run(t, home, repo, map[string]string{
		cloudinit.EnvUser:  "Ada Lovelace",
		cloudinit.EnvEmail: "ada@example.com",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "agtk cloud init: user.name set to \"Ada Lovelace\" globally\n" +
		"agtk cloud init: user.name set to \"Ada Lovelace\" in " + top + "\n" +
		"agtk cloud init: user.email set to \"ada@example.com\" globally\n" +
		"agtk cloud init: " + top + " keeps its own user.email\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant\n%s", out, want)
	}
}

func TestTheReportOutsideACheckoutNamesOnlyTheGlobalWrites(t *testing.T) {
	home := fakeHome(t)

	out, err := run(t, home, t.TempDir(), map[string]string{
		cloudinit.EnvUser:  "Ada Lovelace",
		cloudinit.EnvEmail: "ada@example.com",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "agtk cloud init: user.name set to \"Ada Lovelace\" globally\n" +
		"agtk cloud init: user.email set to \"ada@example.com\" globally\n"
	if out != want {
		t.Errorf("stdout =\n%s\nwant\n%s", out, want)
	}
}
