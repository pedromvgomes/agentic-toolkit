package tests

import (
	"os"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/cli"
)

// actAsAgtk, set in a child's environment, makes this test binary run the
// agtk CLI instead of the tests. cloudinit.Run renders through
// os.Executable, which under `go test` is this binary, so a test that sets it
// exercises the real `agtk render` without building agtk first.
const actAsAgtk = "AGTK_CLOUDINIT_TEST_ACT_AS_AGTK"

func TestMain(m *testing.M) {
	if os.Getenv(actAsAgtk) == "1" {
		os.Exit(cli.Execute())
	}
	os.Exit(m.Run())
}
