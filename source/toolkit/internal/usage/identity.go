package usage

import (
	"path/filepath"
	"strings"

	"github.com/pedromvgomes/agentic-toolkit/internal/gitremote"
)

// Environment variables the identity is read from.
const (
	// ProjectEnv names the Project explicitly, overriding the one derived
	// from the repository.
	ProjectEnv = "AGTK_PROJECT"
	// RemoteEnv is non-empty when Claude Code runs in a cloud session.
	RemoteEnv = "CLAUDE_CODE_REMOTE"
	// AccountUUIDEnv, OrganizationUUIDEnv and UserEmailEnv carry the Claude
	// account the Claude Code process runs under.
	AccountUUIDEnv      = "CLAUDE_CODE_ACCOUNT_UUID"
	OrganizationUUIDEnv = "CLAUDE_CODE_ORGANIZATION_UUID"
	UserEmailEnv        = "CLAUDE_CODE_USER_EMAIL"
)

// Values of Identity.Host.
const (
	HostCloud = "cloud"
	HostLocal = "local"
)

// Identity is who and where a session's rows belong to. A field that cannot
// be determined is nil and serialises as null, never as an empty string.
type Identity struct {
	// Project is the grouping the rows are costed under.
	Project *string `json:"project"`
	// Repo is the origin remote's owner/name.
	Repo *string `json:"repo"`
	// AccountUUID, OrganizationUUID and UserEmail are the Claude account of
	// the process that resolved the identity, not one recorded in the
	// transcript: rows parsed from another account's session carry the
	// parsing process's account.
	AccountUUID      *string `json:"account_uuid"`
	OrganizationUUID *string `json:"organization_uuid"`
	UserEmail        *string `json:"user_email"`
	// Host is HostCloud or HostLocal, and is always set.
	Host string `json:"host"`
}

// LookupEnv reads one environment variable, reporting whether it is set. It
// has the shape of os.LookupEnv.
type LookupEnv func(key string) (string, bool)

// ResolveIdentity resolves the identity of a session that ran in cwd, reading
// the environment only through lookup, which must be non-nil.
//
// Repo is the origin remote's owner/name as gitremote.RemoteSlug reads it,
// and nil when cwd is empty, is not a git checkout, has no origin, or has an
// origin RemoteSlug refuses. RemoteSlug accepts only a two-segment
// owner/repository address, so a GitLab subgroup, a nested GitHub Enterprise
// organisation, a local path and Claude Code's cloud proxy form
// http://local_proxy@127.0.0.1:PORT/git/owner/repo all give a nil Repo.
//
// Project is ProjectEnv trimmed of surrounding whitespace when that leaves
// anything, else Repo, else the base name of cwd, else nil. A refused remote
// therefore names the Project after the checkout's directory rather than
// after the repository it was cloned from.
//
// Host is HostCloud when RemoteEnv is non-empty and HostLocal otherwise. The
// Claude account fields are the values of AccountUUIDEnv,
// OrganizationUUIDEnv and UserEmailEnv, each nil when unset or empty.
func ResolveIdentity(cwd string, lookup LookupEnv) Identity {
	id := Identity{
		Repo:             repoOf(cwd),
		AccountUUID:      envValue(lookup, AccountUUIDEnv),
		OrganizationUUID: envValue(lookup, OrganizationUUIDEnv),
		UserEmail:        envValue(lookup, UserEmailEnv),
		Host:             HostLocal,
	}
	if envValue(lookup, RemoteEnv) != nil {
		id.Host = HostCloud
	}
	id.Project = projectOf(cwd, id.Repo, lookup)
	return id
}

// repoOf reads the origin remote of the checkout at cwd. An empty cwd is not
// looked up: git would run in this process's own directory and report a
// repository the session never ran in.
func repoOf(cwd string) *string {
	if cwd == "" {
		return nil
	}
	slug, err := gitremote.RemoteSlug(cwd, gitremote.DefaultRemote)
	if err != nil {
		return nil
	}
	s := slug.String()
	return &s
}

// projectOf applies the Project precedence: the explicit variable, then the
// repo, then the directory name.
func projectOf(cwd string, repo *string, lookup LookupEnv) *string {
	if v, _ := lookup(ProjectEnv); strings.TrimSpace(v) != "" {
		p := strings.TrimSpace(v)
		return &p
	}
	if repo != nil {
		p := *repo
		return &p
	}
	if cwd == "" {
		return nil
	}
	// filepath.Base reduces a root directory to a lone separator, which
	// names no project.
	name := filepath.Base(cwd)
	if name == string(filepath.Separator) {
		return nil
	}
	return &name
}

// envValue is the value of key, or nil when it is unset or empty.
func envValue(lookup LookupEnv, key string) *string {
	v, _ := lookup(key)
	if v == "" {
		return nil
	}
	return &v
}
