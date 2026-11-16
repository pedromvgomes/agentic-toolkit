package githubapp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrGraphQLUnavailable is what ReadIssueTrust reports when the network it
// runs in refuses GraphQL outright, as opposed to GitHub refusing the query.
//
// A body's edit history is exposed only over GraphQL, and no REST route
// stands in for it, so this refusal leaves the history unknown. It is kept
// apart from ErrForbidden because it says nothing about the token: the same
// token reads the history from a network that lets GraphQL through.
var ErrGraphQLUnavailable = errors.New("GitHub's GraphQL API is not available from this network, " +
	"and an issue's edit history is exposed nowhere else")

// Actor is an account GitHub names as having done something.
type Actor struct {
	Login string
	// Type is GitHub's own name for the kind of account: "User", "Bot",
	// "Organization", "Mannequin", or another type GitHub reports. A Bot's
	// Login is the App's slug without the "[bot]" suffix.
	Type string
}

// BodyEdit is one revision in the issue body's edit history.
type BodyEdit struct {
	// Editor is the account that made the revision, and nil when GitHub names
	// nobody — the account has been deleted. Nil is never an Actor with an
	// empty Login.
	Editor   *Actor
	EditedAt time.Time
	// Deleted reports that the revision has been deleted from the history,
	// which hides what the body said at that point.
	Deleted bool
	// DeletedAt is when the revision was deleted, and zero when Deleted is
	// false.
	DeletedAt time.Time
	// DeletedBy is the account that deleted the revision, and nil when
	// Deleted is false or GitHub names nobody.
	DeletedBy *Actor
}

// LabelEvent is a label applied to or removed from the issue.
type LabelEvent struct {
	Label string
	// Added is true for a labeled event and false for an unlabeled one.
	Added bool
	// Actor is the account that applied or removed the label, and nil when
	// GitHub names nobody.
	Actor     *Actor
	CreatedAt time.Time
}

// IssueTrust is an issue's body together with the record of who shaped it.
type IssueTrust struct {
	Number int
	// Body is the issue body's markdown source, exactly as GitHub stores it.
	Body string
	// Author is the account that opened the issue, and nil when GitHub names
	// nobody.
	Author *Actor
	// Edits is the body's whole edit history, in the order GitHub reports it.
	Edits []BodyEdit
	// LabelEvents are every labeled and unlabeled event on the issue, in the
	// order GitHub's timeline reports them, which is oldest first.
	LabelEvents []LabelEvent
}

// trustPageSize is how many nodes one page of edits or label events asks
// for: GraphQL's own maximum. A node in either connection is a few
// timestamps and logins, so a full page of each beside a body of the
// largest size GitHub accepts stays far below maxBody.
const trustPageSize = 100

// trustActorFields is what every account in these queries is read as.
const trustActorFields = `{__typename login}`

// trustEditsPage is one page of the body's edit history. No diff is read:
// who edited and when is what is decided on, and a diff per edit would
// enlarge a response read under a size cap.
var trustEditsPage = fmt.Sprintf(`userContentEdits(first:%d,after:$editsCursor){
        totalCount
        pageInfo{hasNextPage endCursor}
        nodes{editedAt deletedAt editor%s deletedBy%s}
      }`, trustPageSize, trustActorFields, trustActorFields)

// trustTimelinePage is one page of the labeled and unlabeled events.
var trustTimelinePage = fmt.Sprintf(`timelineItems(first:%d,after:$timelineCursor,itemTypes:[LABELED_EVENT,UNLABELED_EVENT]){
        totalCount
        pageInfo{hasNextPage endCursor}
        nodes{
          __typename
          ... on LabeledEvent{createdAt actor%s label{name}}
          ... on UnlabeledEvent{createdAt actor%s label{name}}
        }
      }`, trustPageSize, trustActorFields, trustActorFields)

// trustEditsCount and trustTimelineCount read a connection's size without
// its nodes, which is what a page of the other connection compares against
// the first answer.
const (
	trustEditsCount    = `userContentEdits(first:1){totalCount}`
	trustTimelineCount = `timelineItems(first:1,itemTypes:[LABELED_EVENT,UNLABELED_EVENT]){totalCount}`
)

// issueTrustQuery builds a query reading the issue's body, its last edit
// time and the two given connection selections.
func issueTrustQuery(cursors, edits, timeline string) string {
	return `query($owner:String!,$repo:String!,$number:Int!` + cursors + `){
  repository(owner:$owner,name:$repo){
    issue(number:$number){
      number
      body
      lastEditedAt
      author` + trustActorFields + `
      ` + edits + `
      ` + timeline + `
    }
  }
}`
}

var (
	// issueTrustFirstQuery reads the body, its author and the first page of
	// both connections in one answer.
	issueTrustFirstQuery = issueTrustQuery(",$editsCursor:String,$timelineCursor:String",
		trustEditsPage, trustTimelinePage)
	// issueTrustEditsQuery reads a later page of edits.
	issueTrustEditsQuery = issueTrustQuery(",$editsCursor:String!", trustEditsPage, trustTimelineCount)
	// issueTrustTimelineQuery reads a later page of label events.
	issueTrustTimelineQuery = issueTrustQuery(",$timelineCursor:String!", trustEditsCount, trustTimelinePage)
)

// trustActor is an account as these queries read it; a null in the answer
// decodes to a nil pointer.
type trustActor struct {
	Typename string `json:"__typename"`
	Login    string `json:"login"`
}

func (a *trustActor) actor() *Actor {
	if a == nil {
		return nil
	}
	return &Actor{Login: a.Login, Type: a.Typename}
}

type trustConnection struct {
	TotalCount int      `json:"totalCount"`
	PageInfo   pageInfo `json:"pageInfo"`
}

type trustEditNode struct {
	EditedAt  time.Time   `json:"editedAt"`
	DeletedAt *time.Time  `json:"deletedAt"`
	Editor    *trustActor `json:"editor"`
	DeletedBy *trustActor `json:"deletedBy"`
}

type trustTimelineNode struct {
	Typename  string      `json:"__typename"`
	CreatedAt time.Time   `json:"createdAt"`
	Actor     *trustActor `json:"actor"`
	Label     *struct {
		Name string `json:"name"`
	} `json:"label"`
}

type trustIssue struct {
	Number           int         `json:"number"`
	Body             string      `json:"body"`
	LastEditedAt     *time.Time  `json:"lastEditedAt"`
	Author           *trustActor `json:"author"`
	UserContentEdits struct {
		trustConnection
		Nodes []trustEditNode `json:"nodes"`
	} `json:"userContentEdits"`
	TimelineItems struct {
		trustConnection
		Nodes []trustTimelineNode `json:"nodes"`
	} `json:"timelineItems"`
}

// ReadIssueTrust reads an issue's body, its author, the body's edit history
// and the issue's labeled and unlabeled events.
//
// The body, its author and the first page of both histories come from one
// query, so the body returned is the one whose history was read. A history
// longer than one page is read on with further queries, each of which reads
// the body, its last edit time and the size of both histories again; any of
// them differing from the first answer refuses the read, since the pages
// would then describe a body other than the one returned. A history is
// refused, too, when the pages read do not add up to the size GitHub
// reports for it.
//
// The whole record or an error, never a short one. A GraphQL refusal from a
// network that blocks GraphQL is ErrGraphQLUnavailable; an issue GitHub
// reports as missing is ErrNotFound; any other refusal, in-band or not, is
// returned as it is.
func (c *Client) ReadIssueTrust(ctx context.Context, number int) (IssueTrust, error) {
	if number < 1 {
		return IssueTrust{}, fmt.Errorf("%d is not an issue number", number)
	}
	owner, repo, err := c.ownerRepo()
	if err != nil {
		return IssueTrust{}, err
	}
	vars := map[string]any{"owner": owner, "repo": repo, "number": number}

	first, err := c.readIssueTrustPage(ctx, number, issueTrustFirstQuery, vars)
	if err != nil {
		return IssueTrust{}, err
	}
	out := IssueTrust{Number: first.Number, Body: first.Body, Author: first.Author.actor()}
	if out.Number != number {
		return IssueTrust{}, fmt.Errorf("GitHub answered issue %d on %s with issue %d", number, c.slug, out.Number)
	}

	edits := first.UserContentEdits.Nodes
	for page, info := 1, first.UserContentEdits.PageInfo; info.HasNextPage; page++ {
		if page >= maxPages || info.EndCursor == "" {
			return IssueTrust{}, fmt.Errorf("the edit history of %s#%d did not end after %d pages of %d",
				c.slug, number, page, trustPageSize)
		}
		next, err := c.readIssueTrustPage(ctx, number, issueTrustEditsQuery, withVar(vars, "editsCursor", info.EndCursor))
		if err != nil {
			return IssueTrust{}, err
		}
		if err := sameIssueState(first, next); err != nil {
			return IssueTrust{}, fmt.Errorf("%s#%d: %w", c.slug, number, err)
		}
		edits = append(edits, next.UserContentEdits.Nodes...)
		info = next.UserContentEdits.PageInfo
	}
	if len(edits) != first.UserContentEdits.TotalCount {
		return IssueTrust{}, fmt.Errorf("GitHub reports %d edits to the body of %s#%d and its pages held %d",
			first.UserContentEdits.TotalCount, c.slug, number, len(edits))
	}

	events := first.TimelineItems.Nodes
	for page, info := 1, first.TimelineItems.PageInfo; info.HasNextPage; page++ {
		if page >= maxPages || info.EndCursor == "" {
			return IssueTrust{}, fmt.Errorf("the label events of %s#%d did not end after %d pages of %d",
				c.slug, number, page, trustPageSize)
		}
		next, err := c.readIssueTrustPage(ctx, number, issueTrustTimelineQuery, withVar(vars, "timelineCursor", info.EndCursor))
		if err != nil {
			return IssueTrust{}, err
		}
		if err := sameIssueState(first, next); err != nil {
			return IssueTrust{}, fmt.Errorf("%s#%d: %w", c.slug, number, err)
		}
		events = append(events, next.TimelineItems.Nodes...)
		info = next.TimelineItems.PageInfo
	}
	if len(events) != first.TimelineItems.TotalCount {
		return IssueTrust{}, fmt.Errorf("GitHub reports %d label events on %s#%d and its pages held %d",
			first.TimelineItems.TotalCount, c.slug, number, len(events))
	}

	for _, node := range edits {
		edit := BodyEdit{Editor: node.Editor.actor(), EditedAt: node.EditedAt}
		if node.DeletedAt != nil {
			edit.Deleted = true
			edit.DeletedAt = *node.DeletedAt
			edit.DeletedBy = node.DeletedBy.actor()
		}
		out.Edits = append(out.Edits, edit)
	}
	for _, node := range events {
		event := LabelEvent{Actor: node.Actor.actor(), CreatedAt: node.CreatedAt}
		switch node.Typename {
		case "LabeledEvent":
			event.Added = true
		case "UnlabeledEvent":
		default:
			return IssueTrust{}, fmt.Errorf("GitHub reported a %q among the label events of %s#%d", node.Typename, c.slug, number)
		}
		if node.Label == nil || node.Label.Name == "" {
			return IssueTrust{}, fmt.Errorf("GitHub reported a label event on %s#%d naming no label", c.slug, number)
		}
		event.Label = node.Label.Name
		out.LabelEvents = append(out.LabelEvents, event)
	}
	return out, nil
}

// readIssueTrustPage runs one of the issue trust queries.
func (c *Client) readIssueTrustPage(ctx context.Context, number int, query string, vars map[string]any) (trustIssue, error) {
	var answer struct {
		Repository *struct {
			Issue *trustIssue `json:"issue"`
		} `json:"repository"`
	}
	if err := c.graphql(ctx, "issue trust", query, vars, &answer); err != nil {
		return trustIssue{}, classifyGraphQLRead(err)
	}
	if answer.Repository == nil || answer.Repository.Issue == nil {
		return trustIssue{}, fmt.Errorf("%w: GitHub reported no issue %d on %s", ErrNotFound, number, c.slug)
	}
	return *answer.Repository.Issue, nil
}

// sameIssueState refuses a later page whose issue no longer matches the
// first answer: a different body, a different last edit time, or a history
// of a different size.
func sameIssueState(first, later trustIssue) error {
	switch {
	case later.Body != first.Body || !sameTime(later.LastEditedAt, first.LastEditedAt):
		return errors.New("the issue body changed while its history was read")
	case later.UserContentEdits.TotalCount != first.UserContentEdits.TotalCount:
		return errors.New("the issue body's edit history changed while it was read")
	case later.TimelineItems.TotalCount != first.TimelineItems.TotalCount:
		return errors.New("the issue's label events changed while they were read")
	}
	return nil
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// withVar copies vars with one more variable set.
func withVar(vars map[string]any, name string, value any) map[string]any {
	out := make(map[string]any, len(vars)+1) // [lydite:exclude_from_mutation][the size is only a capacity hint: a map grows past it, so a smaller hint yields the same map]
	for k, v := range vars {
		out[k] = v
	}
	out[name] = value
	return out
}

// classifyGraphQLRead marks a GraphQL failure with the sentinel a caller
// tells it apart by: a network that blocks GraphQL, a missing issue, or a
// refusal of the token. Every other failure is returned as it is.
func classifyGraphQLRead(err error) error {
	if graphQLBlockedByProxy(err) {
		return fmt.Errorf("%w: %w", ErrGraphQLUnavailable, err)
	}
	var gqlErr *GraphQLError
	if errors.As(err, &gqlErr) {
		for _, message := range gqlErr.Messages {
			if strings.HasPrefix(message, "NOT_FOUND: ") {
				return fmt.Errorf("%w: %w", ErrNotFound, err)
			}
		}
		return err
	}
	return classifyRead(err)
}

// ReadIssueTrust reads an issue's body, its author, the body's edit history
// and the issue's label events, from one query and its continuation pages.
func (r *ReadClient) ReadIssueTrust(ctx context.Context, number int) (IssueTrust, error) {
	return r.c.ReadIssueTrust(ctx, number)
}
