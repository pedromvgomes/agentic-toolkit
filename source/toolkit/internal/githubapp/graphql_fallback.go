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
	Body              string `json:"body"`
	AuthorAssociation string `json:"author_association"`
	User              struct {
		Login string `json:"login"`
	} `json:"user"`
}

// readComment reads one review comment by id, over REST rather than GraphQL:
// a single comment has always had a REST answer, blocked GraphQL query or
// not, so this call is never the one a blocked query forces a substitute
// for.
func (c *Client) readComment(ctx context.Context, owner, repo string, id int64) (prComment, error) {
	var out prComment
	path := fmt.Sprintf("/repos/%s/%s/pulls/comments/%d", owner, repo, id)
	if err := c.call(ctx, http.MethodGet, path, nil, &out); err != nil {
		return prComment{}, err
	}
	return out, nil
}
