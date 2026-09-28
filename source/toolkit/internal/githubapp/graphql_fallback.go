package githubapp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// graphQLBlockedByProxy reports whether err is GitHub's own refusal of a
// GraphQL query made from inside a network that blocks it, as opposed to a
// query GitHub refused on its merits (bad syntax, insufficient scope,
// a repository or pull request that does not exist).
//
// This is not a general network-egress denial: the proxy that answers it
// names a REST-shaped route on the same host that carries the same
// information, so a caller that recognises this one refusal can read on
// through it rather than fail the way any other GraphQL error must.
func graphQLBlockedByProxy(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden &&
		strings.Contains(apiErr.Message, "GraphQL is not available")
}

// ccrThreadNode is one comment thread's resolved/outdated/path state and the
// ids of every comment on it, root first, as the substitute route a blocked
// GraphQL query is told to use instead reports it.
//
// Named for the route it is read from (`.../ccr/review_threads`) rather than
// for GitHub, because nothing about this shape is part of GitHub's own REST
// API — it exists only inside the network that refuses the GraphQL query it
// stands in for, and reading it is this package's one concession to running
// there.
type ccrThreadNode struct {
	Resolved   bool    `json:"resolved"`
	Outdated   bool    `json:"outdated"`
	Path       string  `json:"path"`
	CommentIDs []int64 `json:"comment_ids"`
}

// readThreadNodesViaProxy reads every thread's resolved/outdated/path state
// and comment ids through the substitute route a blocked GraphQL refusal
// names, in one request: unlike the GraphQL connection it stands in for, it
// is observed to answer the whole list rather than a page of it.
func (c *Client) readThreadNodesViaProxy(ctx context.Context, owner, repo string, number int) ([]ccrThreadNode, error) {
	var nodes []ccrThreadNode
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/ccr/review_threads", owner, repo, number)
	if err := c.call(ctx, http.MethodGet, path, nil, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// prComment is one review comment as GitHub's ordinary, always-available
// REST API reports it — read to fill in what the substitute thread route
// does not carry: a comment's body and who wrote it.
type prComment struct {
	ID                int64  `json:"id"`
	Body              string `json:"body"`
	AuthorAssociation string `json:"author_association"`
	User              struct {
		Login string `json:"login"`
	} `json:"user"`
}

// commentsPerPage bounds one page of the paginated comment read that backs
// both proxy readers. A hundred is GitHub's own page maximum, so a pull
// request whose comments fit in one page — the overwhelming case — costs one
// request rather than one per comment.
const commentsPerPage = 100

// readAllPRComments reads every review comment on a pull request, once,
// indexed by id — over REST rather than GraphQL, since a comment's body and
// author have always had a plain REST answer, and paginated, since GraphQL's
// own connection is. This is what both proxy readers below fill a thread's
// root and replies from, instead of the one sequential REST GET per comment
// id an earlier version of this fallback made.
func (c *Client) readAllPRComments(ctx context.Context, owner, repo string, number int) (map[int64]prComment, error) {
	index := make(map[int64]prComment)
	for page := 1; ; page++ {
		if page > maxPages {
			return nil, fmt.Errorf("the comments on %s/%s#%d did not end after %d pages of %d",
				owner, repo, number, maxPages, commentsPerPage)
		}
		var batch []prComment
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments?per_page=%d&page=%d", owner, repo, number, commentsPerPage, page)
		if err := c.call(ctx, http.MethodGet, path, nil, &batch); err != nil {
			return nil, err
		}
		for _, comment := range batch {
			index[comment.ID] = comment
		}
		if len(batch) < commentsPerPage {
			return index, nil
		}
	}
}

// checkThreadNodesComplete refuses a thread list the substitute route
// answered if it is missing a comment the pull request's own, paginated
// comment list says exists.
//
// The route a blocked GraphQL query is redirected to answers the whole
// thread list in one unpaginated response, unlike the GraphQL connection it
// stands in for, which follows hasNextPage and refuses outright past
// maxPages rather than silently stopping short. A route that ever answered a
// partial list here would make an unresolved thread disappear rather than
// read as a failure — and to the approval gate, which trusts "no thread was
// read as open" the same way it trusts "no thread was found", disappearing
// and resolved are indistinguishable. Checked against comments rather than
// threads, because a comment is what the paginated REST list can actually
// enumerate; a missing thread is exactly a comment this check finds with no
// thread naming it.
func checkThreadNodesComplete(nodes []ccrThreadNode, comments map[int64]prComment) error {
	seen := make(map[int64]bool, len(comments))
	for _, node := range nodes {
		for _, id := range node.CommentIDs {
			seen[id] = true
		}
	}
	for id := range comments {
		if !seen[id] {
			return fmt.Errorf("comment %d is on this pull request but is on no thread the substitute route reported: "+
				"its list may be incomplete, and a comment thread's resolved state read through it is not safe to trust", id)
		}
	}
	return nil
}
