package cloudinit

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTheScrubberWithholdsEveryKeyLineOfEightCharactersOrMore(t *testing.T) {
	scrub := scrubber("enc0ded1 short", []byte(armorLine+"\nabcdefgh\nabcdefg\n"))
	const withheld = "[output withheld: it quoted the key]"

	for msg, want := range map[string]string{
		"quotes enc0ded1 here":   withheld,
		"quotes abcdefgh here":   withheld,
		"quotes abcdefg here":    "quotes abcdefg here",
		"quotes short here":      "quotes short here",
		"quotes " + armorLine:    "quotes " + armorLine,
		"names nothing of a key": "names nothing of a key",
	} {
		if got := scrub(msg); got != want {
			t.Errorf("scrub(%q) = %q, want %q", msg, got, want)
		}
	}
}

// armorLine stands in for a key file's header: a line of dashes the scrubber
// never treats as key material.
const armorLine = "-----HEADER-----"

func TestSSHKeygenNamesALoneSubcommandFlagWithoutReadingPastIt(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Fatalf("ssh-keygen is required by these tests: %v", err)
	}
	_, err := sshKeygen(context.Background(), 5*time.Second, func(s string) string { return s }, "-Y")
	if err == nil || !strings.Contains(err.Error(), "ssh-keygen -Y") {
		t.Errorf("err = %v, want a failure naming ssh-keygen -Y", err)
	}
}

func TestWriteAtomicIntoAMissingDirectoryFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")

	err := writeAtomic(dir, "key", []byte("data"), nil)
	if err == nil || !strings.HasPrefix(err.Error(), "cloud init: create a temp file for key: ") {
		t.Errorf("err = %v, want a failure to create the temp file", err)
	}
}
