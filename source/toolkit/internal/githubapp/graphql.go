package githubapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// graphqlPath is the one endpoint GraphQL is served at. Same origin as the
// REST API, so it travels through the same base URL and the same transport.
const graphqlPath = "/graphql"

// GraphQLError is a refusal GitHub made inside a 200.
//
// GraphQL answers 200 to a query it refused, carrying the refusal in an
// `errors` array beside a `data` that is null or only partly filled. A caller
// reading the status alone reads a refused query as a successful one that
// found nothing — and for the query that decides what a review suppresses,
// those two mean "post this finding again" and "it is already on the pull
// request".
type GraphQLError struct {
	// Operation names the query, so a failure says which read was lost
	// rather than that GraphQL failed.
	Operation string
	Messages  []string
}

func (e *GraphQLError) Error() string {
	if len(e.Messages) == 0 {
		return "GitHub refused the " + e.Operation + " query"
	}
	return fmt.Sprintf("GitHub refused the %s query: %s", e.Operation, strings.Join(e.Messages, "; "))
}

// graphql runs one query and decodes its `data` into out.
//
// The transport error and the in-band one are both errors here. A 502 and a
// 200 carrying `errors` are the same event to every caller — the query did not
// answer — and separating them would leave each caller to rediscover that a
// 200 is not enough.
func (c *Client) graphql(ctx context.Context, operation, query string, vars map[string]any, out any) error {
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	body := map[string]any{"query": query, "variables": vars}
	if err := c.call(ctx, http.MethodPost, graphqlPath, body, &envelope); err != nil {
		return err
	}
	if len(envelope.Errors) > 0 {
		messages := make([]string, 0, len(envelope.Errors))
		for _, e := range envelope.Errors {
			message := e.Message
			if e.Type != "" {
				message = e.Type + ": " + message
			}
			messages = append(messages, message)
		}
		return &GraphQLError{Operation: operation, Messages: messages}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return &GraphQLError{Operation: operation, Messages: []string{"the answer carried no data"}}
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("read GitHub's answer to the %s query: %w", operation, err)
	}
	return nil
}

// ownerRepo splits the slug this client was built for into the two arguments
// GraphQL addresses a repository by.
func (c *Client) ownerRepo() (string, string, error) {
	owner, repo, ok := strings.Cut(c.slug, "/")
	if !ok || owner == "" || repo == "" {
		return "", "", fmt.Errorf("%q is not an owner/repository pair", c.slug)
	}
	return owner, repo, nil
}

// pageInfo is how every GraphQL connection reports that it has more.
type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

// pageSize is how many nodes one page asks for. Interpolated into both
// queries, so it is the page size rather than a second number describing it.
//
// A quarter of GraphQL's own hundred, because a response is read up to maxBody
// and a page of threads carries a whole comment body each. A page that overran
// that cap arrives as a JSON parse failure — a whole read lost to save a round
// trip — and GitHub accepts a comment of 65536 characters, so a hundred bodies
// is six times the cap in the worst case and twenty-five is well under it.
const pageSize = 25

// maxPages bounds a paged read.
//
// A cursor the server keeps handing back unchanged is a loop, and a loop
// against a rate-limited API is worse than a refusal. Reaching the bound is
// reported rather than returned as a complete list, because a partial thread
// list read as a complete one is a finding posted twice.
const maxPages = 40
