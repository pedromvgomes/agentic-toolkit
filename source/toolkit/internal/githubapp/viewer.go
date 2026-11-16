package githubapp

import (
	"context"
	"errors"
	"net/http"
)

// ReadViewer reads the login of the account the client's credential belongs
// to, from GET /user.
//
// That is the token's owner, which is a different question from the one
// ByViewer answers: ByViewer is whether the App's installation wrote
// something, and nothing derives it from this login. An installation token
// belongs to no user, so GitHub refuses this read for one; it answers only for
// a token a person or a bot account holds.
//
// A refusal is classified the way every other read's is, so a caller can tell
// ErrForbidden and ErrNotFound apart from an outage. An answer with no login
// is refused rather than returned: an empty login would compare equal to every
// comment whose author's account has been deleted.
func (c *Client) ReadViewer(ctx context.Context) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := c.call(ctx, http.MethodGet, "/user", nil, &user); err != nil {
		return "", classifyRead(err)
	}
	if user.Login == "" {
		return "", errors.New("GitHub reported no login for the account this token belongs to")
	}
	return user.Login, nil
}

// ReadViewer reads the login of the account the token belongs to.
func (r *ReadClient) ReadViewer(ctx context.Context) (string, error) {
	return r.c.ReadViewer(ctx)
}
