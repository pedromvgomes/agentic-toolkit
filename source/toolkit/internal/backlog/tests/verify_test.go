package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pedromvgomes/agentic-toolkit/internal/backlog"
	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const repo = "octo/widgets"

// mustRefuse runs Verify and fails unless it refuses for want, with the kind
// that reason belongs to and no body handed over.
func mustRefuse(t *testing.T, f *fakeReader, trustedApp string, want backlog.Reason, wantKind backlog.Kind) *backlog.Refusal {
	t.Helper()
	got, refusal := backlog.Verify(context.Background(), f, repo, 7, trustedApp)
	if refusal == nil {
		t.Fatalf("Verify trusted the item, want refusal %s", want)
	}
	if refusal.Reason != want {
		t.Fatalf("refusal reason = %s (%s), want %s", refusal.Reason, refusal.Message, want)
	}
	if refusal.Kind != wantKind || want.Kind() != wantKind {
		t.Errorf("refusal kind = %s, reason kind = %s, want %s", refusal.Kind, want.Kind(), wantKind)
	}
	if refusal.Message == "" {
		t.Error("refusal carries no message")
	}
	if got != (backlog.Verified{}) {
		t.Errorf("a refusal handed over a body: %+v", got)
	}
	return refusal
}

// mustTrust runs Verify and fails unless it hands over the body.
func mustTrust(t *testing.T, f *fakeReader, trustedApp string) backlog.Verified {
	t.Helper()
	got, refusal := backlog.Verify(context.Background(), f, repo, 7, trustedApp)
	if refusal != nil {
		t.Fatalf("Verify refused (%s): %s", refusal.Reason, refusal.Message)
	}
	return got
}

func TestATrustedItemHandsOverItsBodyAndTheHashOfItsBytes(t *testing.T) {
	got := mustTrust(t, trustedItem(), "")
	if got.Body != trustedBody {
		t.Errorf("body = %q, want %q", got.Body, trustedBody)
	}
	sum := sha256.Sum256([]byte(trustedBody))
	if want := hex.EncodeToString(sum[:]); got.SHA256 != want {
		t.Errorf("sha256 = %s, want %s", got.SHA256, want)
	}
}

func TestAPullRequestIsRefused(t *testing.T) {
	f := trustedItem()
	f.issue.PullRequest = true
	mustRefuse(t, f, "", backlog.ReasonPullRequest, backlog.Untrusted)
}

func TestAClosedItemIsRefused(t *testing.T) {
	f := trustedItem()
	f.issue.State = "closed"
	mustRefuse(t, f, "", backlog.ReasonClosed, backlog.Untrusted)
}

func TestAStubIsRefusedEvenWhenQueued(t *testing.T) {
	f := trustedItem()
	f.issue.Labels = append(f.issue.Labels, backlog.LabelStub)
	mustRefuse(t, f, "", backlog.ReasonStub, backlog.Untrusted)
}

func TestAnItemWithoutTheQueuedLabelIsRefused(t *testing.T) {
	f := trustedItem()
	f.issue.Labels = []string{"enhancement", backlog.LabelRunning}
	mustRefuse(t, f, "", backlog.ReasonNotQueued, backlog.Untrusted)
	if f.trustReads != 0 {
		t.Errorf("the history of an unqueued item was read %d times", f.trustReads)
	}
}

func TestAQueuedLabelWithNoLabeledEventIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents = f.trust.LabelEvents[:1]
	mustRefuse(t, f, "", backlog.ReasonNoQueuedEvent, backlog.Untrusted)
}

func TestAQueuedLabelWhoseLatestEventRemovedItIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents = append(f.trust.LabelEvents,
		githubapp.LabelEvent{Label: backlog.LabelQueued, Added: false, Actor: user("alice"), CreatedAt: queued.Add(time.Minute)})
	mustRefuse(t, f, "", backlog.ReasonNoQueuedEvent, backlog.Untrusted)
}

func TestALabelFromAnAccountWithoutWriteIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents[1].Actor = user("mallory")
	f.perms["mallory"] = triagePerm
	mustRefuse(t, f, "", backlog.ReasonLabelActorUntrusted, backlog.Untrusted)
}

func TestALabelFromADeletedAccountIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents[1].Actor = nil
	mustRefuse(t, f, "", backlog.ReasonLabelActorUntrusted, backlog.Untrusted)
}

func TestOnlyTheLatestQueuingDecidesWhoQueuedTheItem(t *testing.T) {
	f := trustedItem()
	f.perms["mallory"] = triagePerm
	f.trust.LabelEvents = append(f.trust.LabelEvents,
		githubapp.LabelEvent{Label: backlog.LabelQueued, Added: false, Actor: user("alice"), CreatedAt: queued.Add(time.Minute)},
		githubapp.LabelEvent{Label: backlog.LabelQueued, Added: true, Actor: user("mallory"), CreatedAt: queued.Add(2 * time.Minute)})
	mustRefuse(t, f, "", backlog.ReasonLabelActorUntrusted, backlog.Untrusted)

	f = trustedItem()
	f.perms["mallory"] = triagePerm
	f.trust.LabelEvents = []githubapp.LabelEvent{
		{Label: backlog.LabelQueued, Added: true, Actor: user("mallory"), CreatedAt: queued},
		{Label: backlog.LabelQueued, Added: false, Actor: user("alice"), CreatedAt: queued.Add(time.Minute)},
		{Label: backlog.LabelQueued, Added: true, Actor: user("alice"), CreatedAt: queued.Add(2 * time.Minute)},
	}
	mustTrust(t, f, "")
}

func TestAnAppsLabelIsRefusedWithoutATrustedApp(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents[1].Actor = bot("factory")
	mustRefuse(t, f, "", backlog.ReasonLabelActorUntrusted, backlog.Untrusted)
	if slices.Contains(f.permReads, "factory") {
		t.Error("a bot's slug was looked up as a collaborator, which reads the user of that name")
	}
}

func TestTheTrustedAppsLabelIsAcceptedWhenConfigured(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents[1].Actor = bot("factory")
	mustTrust(t, f, "factory")

	f = trustedItem()
	f.trust.LabelEvents[1].Actor = bot("factory[bot]")
	mustTrust(t, f, "factory")
}

func TestAnotherAppsLabelIsRefusedWhenATrustedAppIsConfigured(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents[1].Actor = bot("lookalike")
	mustRefuse(t, f, "factory", backlog.ReasonLabelActorUntrusted, backlog.Untrusted)
}

func TestAUserNamedLikeTheTrustedAppIsNotTheApp(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents[1].Actor = user("factory")
	mustRefuse(t, f, "factory", backlog.ReasonLabelActorUntrusted, backlog.Untrusted)
}

func TestAnAuthorWithoutWriteIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Author = user("mallory")
	f.perms["mallory"] = githubapp.Permission{Permission: "read", RoleName: "read"}
	mustRefuse(t, f, "", backlog.ReasonAuthorLacksWrite, backlog.Untrusted)
}

func TestABotAuthorIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Author = bot("factory")
	mustRefuse(t, f, "factory", backlog.ReasonAuthorLacksWrite, backlog.Untrusted)
}

func TestADeletedAuthorIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Author = nil
	mustRefuse(t, f, "", backlog.ReasonAuthorDeleted, backlog.Untrusted)
}

func TestAnEditorWithoutWriteIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Edits = append(f.trust.Edits, githubapp.BodyEdit{Editor: user("mallory"), EditedAt: edited.Add(time.Minute)})
	f.perms["mallory"] = triagePerm
	mustRefuse(t, f, "", backlog.ReasonEditorLacksWrite, backlog.Untrusted)
}

func TestADeletedEditorIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Edits[1].Editor = nil
	mustRefuse(t, f, "", backlog.ReasonEditorDeleted, backlog.Untrusted)
}

func TestADeletedRevisionIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Edits[1].Deleted = true
	f.trust.Edits[1].DeletedAt = edited.Add(time.Minute)
	f.trust.Edits[1].DeletedBy = user("alice")
	mustRefuse(t, f, "", backlog.ReasonRevisionDeleted, backlog.Untrusted)
}

func TestAnEditAfterTheQueuingIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Edits = append(f.trust.Edits, githubapp.BodyEdit{Editor: user("bob"), EditedAt: queued.Add(time.Second)})
	mustRefuse(t, f, "", backlog.ReasonEditedAfterQueued, backlog.Untrusted)
}

func TestAnEditInTheSameSecondAsTheQueuingIsRefused(t *testing.T) {
	f := trustedItem()
	f.trust.Edits = append(f.trust.Edits, githubapp.BodyEdit{Editor: user("bob"), EditedAt: queued})
	mustRefuse(t, f, "", backlog.ReasonEditedAfterQueued, backlog.Untrusted)
}

func TestTriageReadAndReadBasedRolesNeverCountAsWrite(t *testing.T) {
	for _, perm := range []githubapp.Permission{
		triagePerm,
		{Permission: "write", RoleName: "triage"},
		{Permission: "admin", RoleName: "read"},
		{Permission: "read", RoleName: "custom-reviewer"},
	} {
		t.Run(fmt.Sprintf("%s/%s", perm.Permission, perm.RoleName), func(t *testing.T) {
			f := trustedItem()
			f.perms["bob"] = perm
			mustRefuse(t, f, "", backlog.ReasonEditorLacksWrite, backlog.Untrusted)
		})
	}
}

func TestWriteMaintainAdminAndWriteBasedCustomRolesCountAsWrite(t *testing.T) {
	for _, perm := range []githubapp.Permission{
		writePerm,
		maintainPerm,
		adminPerm,
		{Permission: "write", RoleName: "custom-engineer"},
	} {
		t.Run(fmt.Sprintf("%s/%s", perm.Permission, perm.RoleName), func(t *testing.T) {
			f := trustedItem()
			f.perms["bob"] = perm
			mustTrust(t, f, "")
		})
	}
}

func TestAnUnreadablePermissionCannotBeDetermined(t *testing.T) {
	f := trustedItem()
	f.permErr = map[string]error{"bob": errRead}
	refusal := mustRefuse(t, f, "", backlog.ReasonPermissionUnreadable, backlog.Undetermined)
	if !errors.Is(refusal.Err, errRead) {
		t.Errorf("refusal error = %v, want the read failure", refusal.Err)
	}
}

func TestARecordThatRefusesOutranksAnUnreadablePermission(t *testing.T) {
	f := trustedItem()
	f.permErr = map[string]error{"alice": errRead, "bob": errRead}
	f.trust.Edits = append(f.trust.Edits, githubapp.BodyEdit{Editor: user("bob"), EditedAt: queued})
	mustRefuse(t, f, "", backlog.ReasonEditedAfterQueued, backlog.Untrusted)
}

func TestAnUnavailableGraphQLCannotBeDetermined(t *testing.T) {
	f := trustedItem()
	f.trustErr = fmt.Errorf("%w: 403 from the proxy", githubapp.ErrGraphQLUnavailable)
	refusal := mustRefuse(t, f, "", backlog.ReasonGraphQLUnavailable, backlog.Undetermined)
	if !errors.Is(refusal.Err, githubapp.ErrGraphQLUnavailable) {
		t.Errorf("refusal error = %v, want ErrGraphQLUnavailable", refusal.Err)
	}
}

func TestAnUnreadableHistoryCannotBeDetermined(t *testing.T) {
	f := trustedItem()
	f.trustErr = errRead
	mustRefuse(t, f, "", backlog.ReasonHistoryUnreadable, backlog.Undetermined)
}

func TestAnUnreadableIssueCannotBeDetermined(t *testing.T) {
	f := trustedItem()
	f.issueErr = fmt.Errorf("%w: 404", githubapp.ErrNotFound)
	refusal := mustRefuse(t, f, "", backlog.ReasonIssueUnreadable, backlog.Undetermined)
	if !errors.Is(refusal.Err, githubapp.ErrNotFound) {
		t.Errorf("refusal error = %v, want ErrNotFound", refusal.Err)
	}
}

func TestAnUntimedEventCannotBeDetermined(t *testing.T) {
	f := trustedItem()
	f.trust.LabelEvents[1].CreatedAt = time.Time{}
	mustRefuse(t, f, "", backlog.ReasonHistoryUnreadable, backlog.Undetermined)

	f = trustedItem()
	f.trust.Edits[1].EditedAt = time.Time{}
	mustRefuse(t, f, "", backlog.ReasonHistoryUnreadable, backlog.Undetermined)
}

func TestEachAccountsPermissionIsReadOnce(t *testing.T) {
	f := trustedItem()
	mustTrust(t, f, "")
	slices.Sort(f.permReads)
	if want := []string{"alice", "bob"}; !slices.Equal(f.permReads, want) {
		t.Errorf("permission reads = %v, want %v", f.permReads, want)
	}
}

// Comments are the one place on an issue anybody who can see it writes, so
// the interface Verify reads through has no way to reach one.
func TestTheReaderCannotReadAComment(t *testing.T) {
	reader := reflect.TypeFor[backlog.Reader]()
	for i := range reader.NumMethod() {
		if name := reader.Method(i).Name; strings.Contains(strings.ToLower(name), "comment") {
			t.Errorf("backlog.Reader has %s, and no comment is ever read as work", name)
		}
	}
}

func TestEveryReasonHasTheKindItsCauseImplies(t *testing.T) {
	undetermined := []backlog.Reason{
		backlog.ReasonIssueUnreadable, backlog.ReasonHistoryUnreadable,
		backlog.ReasonGraphQLUnavailable, backlog.ReasonPermissionUnreadable,
		backlog.Reason("a-reason-nobody-named"),
	}
	untrusted := []backlog.Reason{
		backlog.ReasonPullRequest, backlog.ReasonClosed, backlog.ReasonStub, backlog.ReasonNotQueued,
		backlog.ReasonNoQueuedEvent, backlog.ReasonLabelActorUntrusted, backlog.ReasonAuthorDeleted,
		backlog.ReasonAuthorLacksWrite, backlog.ReasonEditorDeleted, backlog.ReasonEditorLacksWrite,
		backlog.ReasonRevisionDeleted, backlog.ReasonEditedAfterQueued,
	}
	for _, reason := range undetermined {
		if reason.Kind() != backlog.Undetermined {
			t.Errorf("%s is %s, want %s", reason, reason.Kind(), backlog.Undetermined)
		}
	}
	for _, reason := range untrusted {
		if reason.Kind() != backlog.Untrusted {
			t.Errorf("%s is %s, want %s", reason, reason.Kind(), backlog.Untrusted)
		}
	}
}
