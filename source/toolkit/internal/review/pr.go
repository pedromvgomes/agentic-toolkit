package review

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// DefaultRemote is the remote a pull request is read from when nobody names
// one.
const DefaultRemote = "origin"

// Slug is the repository a pull request belongs to, as GitHub addresses it.
type Slug struct {
	Owner string
	Repo  string
}

// String renders the slug the way GitHub writes it.
func (s Slug) String() string { return s.Owner + "/" + s.Repo }

// slugRE reads the owner and repository out of a remote URL, in either of the
// two forms git writes: an SSH scp-style address and an HTTPS URL. The `.git`
// suffix is optional because both forms are used without it.
//
// Anchored at the end rather than searched for, so a URL whose path holds more
// than two segments is refused instead of having its last two taken: a host
// that nests repositories is not github.com, and guessing there would address
// a review at a repository nobody named.
//
// Both captures are held to the characters a GitHub account or repository name
// may actually contain. Accepting anything up to the next `/` would accept `?`,
// `#` and `%2F` — and the slug is interpolated into the API path, so any of the
// three re-points the request at a resource the remote does not name: `a?x=y/b`
// turns the rest of the path into a query, and `owner/repo#x` truncates it.
var slugRE = regexp.MustCompile(`^(?:(?:https?|ssh|git)://)?(?:[^@/]+@)?[^/:]+(?::\d+)?[:/]([A-Za-z0-9][A-Za-z0-9._-]*)/([A-Za-z0-9._-]+?)(?:\.git)?/?$`)

// RemoteSlug reads the owner and repository out of a remote's URL.
//
// Read from git rather than asked for, because the repository a review posts to
// is the one the change was measured against, and a flag would let the two
// diverge silently.
func RemoteSlug(dir, remote string) (Slug, error) {
	if remote == "" {
		remote = DefaultRemote
	}
	// The configured URL rather than `git remote get-url`, which applies
	// url.<base>.insteadOf. That rewrite is about transport — an operator who
	// pushes over SSH what they cloned over HTTPS, or who abbreviates a host
	// — and it can turn a GitHub address into a local mirror path. Where the
	// bytes travel is git's business; which repository a review is posted to
	// is the address the remote was configured with.
	out, err := git(dir, "config", "--get", "remote."+remote+".url")
	if err != nil {
		return Slug{}, fmt.Errorf("read the %s remote: %w", remote, err)
	}
	url := strings.TrimSpace(string(out))
	m := slugRE.FindStringSubmatch(url)
	if m == nil {
		return Slug{}, fmt.Errorf("the %s remote is %q, which is not an owner/repository address agtk can post a review to", remote, url)
	}
	return Slug{Owner: m[1], Repo: m[2]}, nil
}

// FetchPullRequest brings a pull request's head into the local repository and
// returns the commit it resolved to.
//
// The head is fetched by its commit id rather than by the branch name the pull
// request carries. A branch name is chosen by the change's author, and the
// commit the review is posted against has to be the one the API reported: a
// fetch by name would follow a force-push between the two calls and review a
// commit the review then claims is a different one.
func FetchPullRequest(dir string, number int, headSHA string) (string, error) {
	if number < 1 {
		return "", fmt.Errorf("%d is not a pull request number", number)
	}
	if !isCommitID(headSHA) {
		return "", fmt.Errorf("%q is not a commit id", headSHA)
	}
	// The pull ref rather than the bare commit id: a repository whose remote
	// refuses `uploadpack.allowReachableSHA1InWant` cannot be asked for a
	// commit by name, and every GitHub pull request has this ref.
	spec := fmt.Sprintf("refs/pull/%d/head", number)
	if _, err := git(dir, "fetch", "--quiet", "--no-tags", DefaultRemote, spec); err != nil {
		return "", fmt.Errorf("fetch pull request %d: %w", number, err)
	}
	out, err := git(dir, "rev-parse", "--verify", "--quiet", headSHA+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("pull request %d's head %s is not in the repository after fetching %s: %w",
			number, headSHA, spec, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// FetchCommit brings one commit into the local repository, for a base the
// pull request names that the checkout has never seen.
func FetchCommit(dir, sha string) error {
	if !isCommitID(sha) {
		return fmt.Errorf("%q is not a commit id", sha)
	}
	if _, err := git(dir, "rev-parse", "--verify", "--quiet", sha+"^{commit}"); err == nil {
		return nil
	}
	if _, err := git(dir, "fetch", "--quiet", "--no-tags", DefaultRemote, sha); err != nil {
		return fmt.Errorf("fetch %s: %w", sha, err)
	}
	return nil
}

// commitIDRE is a full-length hexadecimal object name, in either of git's two
// hash lengths.
var commitIDRE = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// isCommitID reports whether s is an object name rather than a ref.
//
// Every commit reaching git from the API goes through this. The API's own
// answer is not the reason — it is well formed — but the value travels as a
// command argument, and a name that is only ever forty hex characters cannot
// be an option, a revision expression or a pathspec whatever else is true.
func isCommitID(s string) bool { return commitIDRE.MatchString(s) }

// AddedLines reports, per file the diff touches, which lines of the post-image
// it adds.
//
// Every path with a hunk is a key, including one the change only deletes from,
// whose set of added lines is empty. The two questions the result answers are
// different: whether an inline comment may name a line, and whether the diff
// touches a path at all — and GitHub refuses a file-level comment on a path
// the change does not touch, which is what makes a finding there unanswerable.
//
// It is what decides whether a finding can be an inline comment. GitHub
// rejects the entire review with a 422 when one comment names a line outside
// the pull request's diff, so a line this does not hold is a line no comment
// may carry — and one bad line costs every comment in the batch, including the
// ones that were right.
//
// Added lines only, never context. A context line's presence depends on how
// much context the diff was rendered with, which is a property of the command
// that produced this patch rather than of the pull request GitHub is holding.
func AddedLines(patch string) map[string]map[int]bool {
	out := map[string]map[int]bool{}
	var section diffSection
	line := 0
	for _, text := range strings.Split(patch, "\n") {
		if section.track(text) {
			line = 0
			continue
		}
		if m := newSideRE.FindStringSubmatch(text); m != nil {
			// The path is registered on the hunk header rather than on the
			// first added line, so a file the change only deletes from is
			// still a path the diff touches. GitHub accepts a file-level
			// comment there and refuses one on a path the change never names,
			// and that difference is what decides whether a finding with no
			// line can be answered at all.
			if path := section.path(); path != "" && out[path] == nil {
				out[path] = map[int]bool{}
			}
			start, err := strconv.Atoi(m[1])
			if err != nil {
				line = 0
				continue
			}
			line = start
			continue
		}
		if line == 0 || section.path() == "" {
			continue
		}
		switch {
		case strings.HasPrefix(text, "+"):
			file := out[section.path()]
			if file == nil {
				file = map[int]bool{}
				out[section.path()] = file
			}
			file[line] = true
			line++
		case strings.HasPrefix(text, "-"), strings.HasPrefix(text, "\\"):
			// A removed line advances the pre-image, not the post-image, and
			// `\ No newline at end of file` annotates the line before it.
		case strings.HasPrefix(text, " "), text == "":
			line++
		default:
			// Anything else ends the hunk: the next header re-anchors.
			line = 0
		}
	}
	return out
}

// newSideRE reads the post-image start out of `@@ -a,b +c,d @@`.
var newSideRE = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// AddedLinesAt reports which lines each file of a range adds, as GitHub sees
// them.
//
// Taken over the whole range rather than over the files the profile considers
// worth reviewing. Mechanical exclusions decide what a panel is asked to read;
// they say nothing about where GitHub will accept a comment, and a finding
// pointing at an excluded file has to be recognised as unpositionable rather
// than sent and refused.
func AddedLinesAt(dir, base, head string) (map[string]map[int]bool, error) {
	args := append(append([]string(nil), diffArgs...), "-U0", rangeArg(base, head), "--")
	out, err := git(dir, args...)
	if err != nil {
		return nil, err
	}
	return AddedLines(string(out)), nil
}
