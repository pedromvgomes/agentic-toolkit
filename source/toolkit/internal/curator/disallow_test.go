package curator

import (
	"slices"
	"testing"

	"github.com/pedromvgomes/agentic-driver"
)

// withoutDeny hides the wrapped provider's Disallower: embedding the interface
// exposes only what agentic.Provider itself declares.
type withoutDeny struct{ agentic.Provider }

func TestDisallowRefusesARealRunOnAProviderThatCannotDeny(t *testing.T) {
	p, err := newProvider("claudecode")
	if err != nil {
		t.Fatalf("newProvider: %v", err)
	}

	if _, ok := p.(agentic.Disallower); !ok {
		t.Fatal("the fixture provider can deny tools, so it proves nothing about one that cannot")
	}
	if _, err := disallow(withoutDeny{p}, false); err == nil {
		t.Error("a real run on a provider that cannot deny tools was allowed")
	}
	if got, err := disallow(withoutDeny{p}, true); err != nil || got != nil {
		t.Errorf("disallow(dry run) = %v, %v; a preview proceeds without a deny list", got, err)
	}
	if got, err := disallow(p, false); err != nil || !slices.Equal(got, disallowedDelegationTools) {
		t.Errorf("disallow = %v, %v; want the delegation tools", got, err)
	}
}
