package tests

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/githubapp"
)

const userPath = "/user"

func TestTheViewerIsTheLoginTheTokenBelongsTo(t *testing.T) {
	c, net := readClient(t, get(userPath, 200, `{"login":"octocat","id":1,"type":"User"}`))
	login, err := c.ReadViewer(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	net.done()
	if login != "octocat" {
		t.Errorf("read the viewer as %q, want octocat", login)
	}
	req := net.seen[0]
	if req.Method != http.MethodGet || net.bodies[0] != "" {
		t.Errorf("the viewer read is %s with body %q, want a GET with none", req.Method, net.bodies[0])
	}
	if req.Header.Get("Authorization") != "Bearer ghp_scripted" {
		t.Error("the viewer read did not carry the caller's token")
	}
}

// An empty login would match every comment whose author's account is gone.
func TestAViewerWithNoLoginIsRefused(t *testing.T) {
	c, net := readClient(t, get(userPath, 200, `{"id":1}`))
	if login, err := c.ReadViewer(context.Background()); err == nil {
		t.Errorf("an answer with no login read as viewer %q", login)
	}
	net.done()
}

func TestAViewerReadGitHubRefusesIsClassified(t *testing.T) {
	c, _ := readClient(t, get(userPath, 403, `{"message":"Resource not accessible by integration"}`))
	if _, err := c.ReadViewer(context.Background()); !errors.Is(err, githubapp.ErrForbidden) {
		t.Errorf("a 403 on the viewer read as %v", err)
	}
	c, _ = readClient(t, get(userPath, 401, `{"message":"Bad credentials"}`))
	_, err := c.ReadViewer(context.Background())
	var apiErr *githubapp.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 401 {
		t.Errorf("a 401 on the viewer read as %v", err)
	}
}

// The App's client reads the viewer with its installation token, which
// GitHub may refuse; the refusal reaches the caller rather than an empty login.
func TestAnAppClientReadsTheViewerWithItsInstallationToken(t *testing.T) {
	c, net := client(t, append(auth(far()),
		exchange{method: http.MethodGet, path: userPath, status: 403, body: `{"message":"Resource not accessible by integration"}`})...)
	login, err := c.ReadViewer(context.Background())
	net.done()
	if !errors.Is(err, githubapp.ErrForbidden) {
		t.Errorf("an App client's refused viewer read returned %q, %v", login, err)
	}
	if got := net.seen[len(net.seen)-1].Header.Get("Authorization"); got != "Bearer ghs_scripted" {
		t.Errorf("the viewer read carried %q, want the installation token", got)
	}
}
