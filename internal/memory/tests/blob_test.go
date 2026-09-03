package tests

import (
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/memory"
)

// TestBlobHashMatchesGit pins the in-process hash to git's own blob id.
// The value is `git hash-object` of "hello\n"; if this drifts, every anchor
// in every store silently stops interoperating with git's plumbing.
func TestBlobHashMatchesGit(t *testing.T) {
	const gitBlobOfHello = "ce013625030ba8dba906f756967f9e9ca394464a"

	got := memory.BlobHash([]byte("hello\n"))
	if want := gitBlobOfHello[:memory.BlobHashLen]; got != want {
		t.Errorf("BlobHash = %q, want %q (git hash-object of \"hello\\n\")", got, want)
	}
}

// TestBlobHashDistinguishesContent guards the length prefix: hashing the
// bare content would collide for inputs that differ only in framing.
func TestBlobHashDistinguishesContent(t *testing.T) {
	if memory.BlobHash([]byte("a")) == memory.BlobHash([]byte("b")) {
		t.Error("distinct content produced the same blob id")
	}
	if memory.BlobHash(nil) != memory.BlobHash([]byte("")) {
		t.Error("empty and nil content should hash identically")
	}
}
