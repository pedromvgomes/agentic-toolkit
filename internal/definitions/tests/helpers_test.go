package tests

import (
	"io/fs"
	"os"
)

// validFS returns an fs.FS rooted at testdata/valid (so paths like
// "definitions/skills/example/SKILL.md" resolve directly).
func validFS() fs.FS { return os.DirFS("testdata/valid") }

// invalidFS returns an fs.FS rooted at testdata/invalid.
func invalidFS() fs.FS { return os.DirFS("testdata/invalid") }
