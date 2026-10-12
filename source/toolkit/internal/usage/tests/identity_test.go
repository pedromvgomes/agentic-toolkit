package tests

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pedromvgomes/agentic-toolkit/internal/usage"
)

// env is an environment lookup over a fixed map, so no test reads or writes
// the process environment.
func env(vars map[string]string) usage.LookupEnv {
	return func(key string) (string, bool) {
		v, ok := vars[key]
		return v, ok
	}
}

// checkout is a git repository in a fresh directory whose base name is name,
// with origin set to url when url is non-empty.
func checkout(t *testing.T, name, url string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	run(t, "", "init", "-b", "main", dir)
	if url != "" {
		run(t, dir, "remote", "add", "origin", url)
	}
	return dir
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// wantPtr fails unless got is nil when want is nil, and points at *want
// otherwise. A pointer to an empty string is never an absent value.
func wantPtr(t *testing.T, field string, got, want *string) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s = %q, want nil", field, *got)
	case want != nil && got == nil:
		t.Errorf("%s = nil, want %q", field, *want)
	case want != nil && *got != *want:
		t.Errorf("%s = %q, want %q", field, *got, *want)
	}
}

func ptr(s string) *string { return &s }

const slug = "pedromvgomes/agentic-toolkit"

func TestTheProjectIsTheVariableTrimmedWhenItHoldsAnything(t *testing.T) {
	dir := checkout(t, "work", "https://github.com/"+slug)
	for _, tc := range []struct {
		name  string
		vars  map[string]string
		want  string
		repoW string
	}{
		{"set", map[string]string{"AGTK_PROJECT": "billing"}, "billing", slug},
		{"padded", map[string]string{"AGTK_PROJECT": "  billing\t\n"}, "billing", slug},
		{"unset", map[string]string{}, slug, slug},
		{"empty", map[string]string{"AGTK_PROJECT": ""}, slug, slug},
		{"whitespace", map[string]string{"AGTK_PROJECT": " \t\n"}, slug, slug},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := usage.ResolveIdentity(dir, env(tc.vars))
			wantPtr(t, "Project", id.Project, ptr(tc.want))
			wantPtr(t, "Repo", id.Repo, ptr(tc.repoW))
		})
	}
}

func TestTheVariableNamesTheProjectWithoutARepoOrADirectory(t *testing.T) {
	id := usage.ResolveIdentity("", env(map[string]string{"AGTK_PROJECT": "billing"}))
	wantPtr(t, "Project", id.Project, ptr("billing"))
	wantPtr(t, "Repo", id.Repo, nil)
}

func TestTheRepoIsTheOriginRemotesOwnerAndName(t *testing.T) {
	for _, url := range []string{
		"git@github.com:" + slug + ".git",
		"https://github.com/" + slug + ".git",
		"https://github.com/" + slug,
	} {
		dir := checkout(t, "work", url)
		id := usage.ResolveIdentity(dir, env(nil))
		wantPtr(t, url+" Repo", id.Repo, ptr(slug))
		wantPtr(t, url+" Project", id.Project, ptr(slug))
	}
}

// Only origin is read: a checkout whose one remote has another name has no
// Repo, and the Project falls back to the directory.
func TestARemoteOtherThanOriginIsNotTheRepo(t *testing.T) {
	dir := checkout(t, "work", "")
	run(t, dir, "remote", "add", "upstream", "https://github.com/"+slug)
	id := usage.ResolveIdentity(dir, env(nil))
	wantPtr(t, "Repo", id.Repo, nil)
	wantPtr(t, "Project", id.Project, ptr("work"))
}

// The remote parser accepts only an owner/repository address. A remote it
// refuses has no Repo, and the Project is the checkout's directory name.
func TestARefusedRemoteFallsBackToTheDirectoryName(t *testing.T) {
	for _, url := range []string{
		"https://gitlab.example.com/group/subgroup/project.git",
		"/srv/git/bare-repo.git",
		"http://local_proxy@127.0.0.1:41234/git/pedromvgomes/agentic-toolkit",
	} {
		dir := checkout(t, "my-checkout", url)
		id := usage.ResolveIdentity(dir, env(nil))
		wantPtr(t, url+" Repo", id.Repo, nil)
		wantPtr(t, url+" Project", id.Project, ptr("my-checkout"))
	}
}

func TestACheckoutWithoutOriginFallsBackToTheDirectoryName(t *testing.T) {
	dir := checkout(t, "no-origin", "")
	id := usage.ResolveIdentity(dir, env(nil))
	wantPtr(t, "Repo", id.Repo, nil)
	wantPtr(t, "Project", id.Project, ptr("no-origin"))
}

func TestADirectoryThatIsNotACheckoutFallsBackToItsName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plain")
	id := usage.ResolveIdentity(dir, env(nil))
	wantPtr(t, "Repo", id.Repo, nil)
	wantPtr(t, "Project", id.Project, ptr("plain"))
}

// An empty cwd is not resolved against the process's own directory, which is
// itself a checkout with an origin while these tests run.
func TestAnEmptyCwdHasNoRepoAndNoProject(t *testing.T) {
	id := usage.ResolveIdentity("", env(nil))
	wantPtr(t, "Repo", id.Repo, nil)
	wantPtr(t, "Project", id.Project, nil)
}

func TestTheRootDirectoryNamesNoProject(t *testing.T) {
	id := usage.ResolveIdentity(string(filepath.Separator), env(nil))
	wantPtr(t, "Repo", id.Repo, nil)
	wantPtr(t, "Project", id.Project, nil)
}

func TestTheHostIsCloudOnlyWhenTheRemoteVariableHoldsAValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		vars map[string]string
		want string
	}{
		{"set", map[string]string{"CLAUDE_CODE_REMOTE": "true"}, usage.HostCloud},
		{"empty", map[string]string{"CLAUDE_CODE_REMOTE": ""}, usage.HostLocal},
		{"unset", map[string]string{}, usage.HostLocal},
	} {
		if got := usage.ResolveIdentity("", env(tc.vars)).Host; got != tc.want {
			t.Errorf("%s: Host = %q, want %q", tc.name, got, tc.want)
		}
	}
	if usage.HostCloud != "cloud" || usage.HostLocal != "local" {
		t.Errorf("hosts are %q and %q, want cloud and local", usage.HostCloud, usage.HostLocal)
	}
}

func TestEachAccountValueComesFromItsOwnVariable(t *testing.T) {
	all := map[string]string{
		"CLAUDE_CODE_ACCOUNT_UUID":      "acct-1",
		"CLAUDE_CODE_ORGANIZATION_UUID": "org-1",
		"CLAUDE_CODE_USER_EMAIL":        "someone@example.com",
	}
	id := usage.ResolveIdentity("", env(all))
	wantPtr(t, "AccountUUID", id.AccountUUID, ptr("acct-1"))
	wantPtr(t, "OrganizationUUID", id.OrganizationUUID, ptr("org-1"))
	wantPtr(t, "UserEmail", id.UserEmail, ptr("someone@example.com"))

	for key := range all {
		for _, absent := range []string{"unset", "empty"} {
			vars := map[string]string{}
			for k, v := range all {
				vars[k] = v
			}
			if absent == "unset" {
				delete(vars, key)
			} else {
				vars[key] = ""
			}
			id := usage.ResolveIdentity("", env(vars))
			want := func(k string) *string {
				if k == key {
					return nil
				}
				return ptr(all[k])
			}
			wantPtr(t, key+" "+absent+" AccountUUID", id.AccountUUID, want("CLAUDE_CODE_ACCOUNT_UUID"))
			wantPtr(t, key+" "+absent+" OrganizationUUID", id.OrganizationUUID, want("CLAUDE_CODE_ORGANIZATION_UUID"))
			wantPtr(t, key+" "+absent+" UserEmail", id.UserEmail, want("CLAUDE_CODE_USER_EMAIL"))
		}
	}
}

func TestAnAbsentValueSerialisesAsNull(t *testing.T) {
	b, err := json.Marshal(usage.ResolveIdentity("", env(nil)))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"project":null,"repo":null,"account_uuid":null,"organization_uuid":null,"user_email":null,"host":"local"}`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}
