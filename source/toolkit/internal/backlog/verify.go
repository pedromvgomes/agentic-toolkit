package backlog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

// Reader is what this package reads GitHub through. A *githubapp.ReadClient
// satisfies it; nothing in it writes, and nothing in it reads a comment.
type Reader interface {
	ReadIssue(ctx context.Context, number int) (githubapp.Issue, error)
	ReadPermission(ctx context.Context, login string) (githubapp.Permission, error)
	ReadIssueTrust(ctx context.Context, number int) (githubapp.IssueTrust, error)
	ReadLabelledIssues(ctx context.Context, label string) ([]githubapp.Issue, error)
	ReadPredecessors(ctx context.Context, number int) ([]githubapp.Issue, error)
	ReadParent(ctx context.Context, number int) (githubapp.Issue, bool, error)
}

// Verified is a trusted item's work: the body exactly as verified, and the
// SHA-256 of its bytes. A run compares the hash against the body it is about
// to act on, so an edit landing after Verify changes the hash, not the work.
type Verified struct {
	Body string `json:"body"`
	// SHA256 is the lower-case hex SHA-256 of Body's bytes.
	SHA256 string `json:"sha256"`
}

// Kind separates the two answers a refusal can be.
//
// They lead to different actions — Undetermined means fix the reading,
// Untrusted means a person has to look at the item — and an answer that
// merged them would make an outage indistinguishable from an attack.
type Kind string

const (
	// Undetermined is a refusal because a read failed: nothing about the
	// item was learned.
	Undetermined Kind = "could-not-determine"
	// Untrusted is a refusal because GitHub's record shows the item fails a
	// condition.
	Untrusted Kind = "not-trusted"
)

// Reason names why an item was refused.
type Reason string

// Reasons a read failed, each of Kind Undetermined.
const (
	// ReasonIssueUnreadable is a failed read of the issue itself.
	ReasonIssueUnreadable Reason = "issue-unreadable"
	// ReasonHistoryUnreadable is a failed read of the body's edit history or
	// the label events, or a record missing a timestamp the rule compares.
	ReasonHistoryUnreadable Reason = "history-unreadable"
	// ReasonGraphQLUnavailable is a network that refuses GraphQL, the only
	// place the body's edit history is exposed.
	ReasonGraphQLUnavailable Reason = "graphql-unavailable"
	// ReasonPermissionUnreadable is a permission lookup that did not answer.
	ReasonPermissionUnreadable Reason = "permission-unreadable"
)

// Reasons the record refuses the item, each of Kind Untrusted.
const (
	ReasonPullRequest         Reason = "pull-request"
	ReasonClosed              Reason = "closed"
	ReasonStub                Reason = "stub"
	ReasonNotQueued           Reason = "not-queued"
	ReasonNoQueuedEvent       Reason = "no-queued-event"
	ReasonLabelActorUntrusted Reason = "label-actor-untrusted"
	ReasonAuthorDeleted       Reason = "author-deleted"
	ReasonAuthorLacksWrite    Reason = "author-lacks-write"
	ReasonEditorDeleted       Reason = "editor-deleted"
	ReasonEditorLacksWrite    Reason = "editor-lacks-write"
	ReasonRevisionDeleted     Reason = "revision-deleted"
	ReasonEditedAfterQueued   Reason = "edited-after-queued"
)

// Kind reports whether the reason is a failed read or a failed condition.
// A reason this package does not name is Undetermined, which refuses
// without claiming anything about the item.
func (r Reason) Kind() Kind {
	switch r {
	case ReasonPullRequest, ReasonClosed, ReasonStub, ReasonNotQueued, ReasonNoQueuedEvent,
		ReasonLabelActorUntrusted, ReasonAuthorDeleted, ReasonAuthorLacksWrite,
		ReasonEditorDeleted, ReasonEditorLacksWrite, ReasonRevisionDeleted, ReasonEditedAfterQueued:
		return Untrusted
	}
	return Undetermined
}

// Refusal is why an item is not handed over.
type Refusal struct {
	Reason Reason `json:"reason"`
	Kind   Kind   `json:"kind"`
	// Message says, for a person, what was found. Logins and labels in it
	// come from GitHub and are quoted.
	Message string `json:"message"`
	// Err is the read failure behind an Undetermined refusal, and nil for an
	// Untrusted one.
	Err error `json:"-"`
}

func refuse(reason Reason, err error, format string, args ...any) *Refusal {
	return &Refusal{Reason: reason, Kind: reason.Kind(), Message: fmt.Sprintf(format, args...), Err: err}
}

// Verify decides whether issue number of repo may be acted on, and returns
// its body and the body's SHA-256 when it may.
//
// It is trusted only when all of these hold, and refused otherwise:
//
//  1. The body is the work: only the body is returned, and no comment is read.
//  2. The issue's author and every editor in the body's edit history have
//     write access, read now rather than at the time of the edit.
//  3. The latest agtk:queued labeled event was applied by trustedApp's bot
//     account, when trustedApp is set, or by an account with write access.
//  4. No body edit is at or after that event.
//
// A deleted account or a deleted revision refuses, since it names nobody
// whose access can be checked. Any read that fails refuses as Undetermined.
// repo names the repository in messages; reader is already bound to it.
func Verify(ctx context.Context, reader Reader, repo string, number int, trustedApp string) (Verified, *Refusal) {
	item := fmt.Sprintf("%s#%d", repo, number)

	issue, err := reader.ReadIssue(ctx, number)
	if err != nil {
		return Verified{}, refuse(ReasonIssueUnreadable, err, "could not read %s: %v", item, err)
	}
	switch {
	case issue.PullRequest:
		return Verified{}, refuse(ReasonPullRequest, nil, "%s is a pull request, not an issue", item)
	case issue.State != "open":
		return Verified{}, refuse(ReasonClosed, nil, "%s is %q, and only an open item is acted on", item, issue.State)
	case slices.Contains(issue.Labels, LabelStub):
		return Verified{}, refuse(ReasonStub, nil, "%s carries %s: it holds nothing to implement yet", item, LabelStub)
	case !slices.Contains(issue.Labels, LabelQueued):
		return Verified{}, refuse(ReasonNotQueued, nil, "%s does not carry %s", item, LabelQueued)
	}

	trust, err := reader.ReadIssueTrust(ctx, number)
	if errors.Is(err, githubapp.ErrGraphQLUnavailable) {
		return Verified{}, refuse(ReasonGraphQLUnavailable, err,
			"could not read the edit history of %s: GraphQL is not reachable from this network: %v", item, err)
	}
	if err != nil {
		return Verified{}, refuse(ReasonHistoryUnreadable, err, "could not read the edit history of %s: %v", item, err)
	}

	// The record is judged on what it says before any permission is read, so
	// an item the record already refuses is reported as untrusted rather than
	// as unreadable when a lookup fails.
	queued, found := latestQueuedEvent(trust.LabelEvents)
	if !found {
		return Verified{}, refuse(ReasonNoQueuedEvent, nil,
			"%s carries %s, but its timeline holds no %s labeled event that is still in force", item, LabelQueued, LabelQueued)
	}
	if queued.CreatedAt.IsZero() {
		return Verified{}, refuse(ReasonHistoryUnreadable, nil, "GitHub reported the %s event on %s with no time", LabelQueued, item)
	}
	if trust.Author == nil {
		return Verified{}, refuse(ReasonAuthorDeleted, nil, "the author of %s is a deleted account, whose access cannot be checked", item)
	}
	for i, edit := range trust.Edits {
		switch {
		case edit.Deleted:
			return Verified{}, refuse(ReasonRevisionDeleted, nil,
				"revision %d of the body of %s has been deleted, hiding what it said and who wrote it", i+1, item)
		case edit.Editor == nil:
			return Verified{}, refuse(ReasonEditorDeleted, nil,
				"revision %d of the body of %s was made by a deleted account, whose access cannot be checked", i+1, item)
		case edit.EditedAt.IsZero():
			return Verified{}, refuse(ReasonHistoryUnreadable, nil, "GitHub reported revision %d of the body of %s with no time", i+1, item)
		case !edit.EditedAt.Before(queued.CreatedAt):
			return Verified{}, refuse(ReasonEditedAfterQueued, nil,
				"the body of %s was edited by %q at %s, not before it was queued at %s",
				item, edit.Editor.Login, edit.EditedAt.UTC().Format("2006-01-02T15:04:05Z"),
				queued.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"))
		}
	}

	access := newAccess(reader)
	if !appliedByTrustedApp(queued.Actor, trustedApp) {
		ok, err := access.write(ctx, queued.Actor)
		if err != nil {
			return Verified{}, refuse(ReasonPermissionUnreadable, err,
				"could not read the permission of %q, who queued %s: %v", actorLogin(queued.Actor), item, err)
		}
		if !ok {
			return Verified{}, refuse(ReasonLabelActorUntrusted, nil,
				"%s was queued by %s, which is neither the trusted app nor an account with write access",
				item, describeActor(queued.Actor))
		}
	}
	ok, err := access.write(ctx, trust.Author)
	if err != nil {
		return Verified{}, refuse(ReasonPermissionUnreadable, err,
			"could not read the permission of %q, the author of %s: %v", trust.Author.Login, item, err)
	}
	if !ok {
		return Verified{}, refuse(ReasonAuthorLacksWrite, nil,
			"the author of %s, %s, does not have write access", item, describeActor(trust.Author))
	}
	for i, edit := range trust.Edits {
		ok, err := access.write(ctx, edit.Editor)
		if err != nil {
			return Verified{}, refuse(ReasonPermissionUnreadable, err,
				"could not read the permission of %q, who made revision %d of %s: %v", edit.Editor.Login, i+1, item, err)
		}
		if !ok {
			return Verified{}, refuse(ReasonEditorLacksWrite, nil,
				"revision %d of the body of %s was made by %s, who does not have write access", i+1, item, describeActor(edit.Editor))
		}
	}

	sum := sha256.Sum256([]byte(trust.Body))
	return Verified{Body: trust.Body, SHA256: hex.EncodeToString(sum[:])}, nil
}

// latestQueuedEvent returns the latest agtk:queued labeled event, and false
// when there is none or a later unlabeled event removed it. Events are in
// the timeline's order, oldest first.
func latestQueuedEvent(events []githubapp.LabelEvent) (githubapp.LabelEvent, bool) {
	for _, event := range slices.Backward(events) {
		if event.Label != LabelQueued {
			continue
		}
		return event, event.Added
	}
	return githubapp.LabelEvent{}, false
}

// appliedByTrustedApp reports whether actor is the bot account of the App
// named trustedApp. GitHub reports a bot's login as the App's slug, with or
// without the "[bot]" suffix depending on the API, so both are matched. An
// unset trustedApp matches nothing.
func appliedByTrustedApp(actor *githubapp.Actor, trustedApp string) bool {
	if actor == nil || trustedApp == "" || actor.Type != "Bot" {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(actor.Login, "[bot]"), trustedApp)
}

// access reads accounts' write access, each account once.
type access struct {
	reader Reader
	seen   map[string]bool
}

func newAccess(reader Reader) *access {
	return &access{reader: reader, seen: map[string]bool{}}
}

// write reports whether actor has write access to the repository.
//
// Only a User is looked up. A bot's login is its App's slug, which the
// collaborator endpoint would read as a user of the same name, and no other
// kind of account is a collaborator; none of them counts.
func (a *access) write(ctx context.Context, actor *githubapp.Actor) (bool, error) {
	if actor == nil || actor.Type != "User" || actor.Login == "" {
		return false, nil
	}
	key := strings.ToLower(actor.Login)
	if ok, found := a.seen[key]; found {
		return ok, nil
	}
	perm, err := a.reader.ReadPermission(ctx, actor.Login)
	if err != nil {
		return false, err
	}
	ok := grantsWrite(perm)
	a.seen[key] = ok
	return ok, nil
}

// grantsWrite reports whether perm includes write access.
//
// GitHub's coarse Permission folds maintain into "write" and triage into
// "read"; a custom role reports the level it inherits from. Only "admin" and
// "write" count, and a RoleName of triage or read refuses even under those,
// so a lower role is never counted however the two fields disagree.
func grantsWrite(perm githubapp.Permission) bool {
	if perm.Permission != "admin" && perm.Permission != "write" {
		return false
	}
	switch perm.RoleName {
	case "triage", "read", "none":
		return false
	}
	return true
}

func actorLogin(actor *githubapp.Actor) string {
	if actor == nil {
		return ""
	}
	return actor.Login
}

// describeActor names an account for a message, quoting what GitHub reported.
func describeActor(actor *githubapp.Actor) string {
	if actor == nil {
		return "a deleted account"
	}
	return fmt.Sprintf("%q (type %q)", actor.Login, actor.Type)
}
