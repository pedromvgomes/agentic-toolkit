package githubapp

import (
	"context"
	"fmt"
)

// ReviewThread is one comment thread on a pull request, as GitHub reports it.
//
// Read over GraphQL rather than REST because two of its four fields have no
// REST answer: the comments endpoint reports `position: null` for a comment
// whose code has moved and says nothing at all about resolution, so it cannot
// tell an outdated thread from a resolved one — and those two states oblige
// opposite things.
type ReviewThread struct {
	// Path is the file the thread hangs off, as it is named now.
	Path string
	// Resolved reports that somebody read this thread and closed it.
	Resolved bool
	// Outdated reports that the code the thread hangs off has moved, which is
	// what makes GitHub collapse it out of sight.
	Outdated bool
	// Body is the thread's first comment — the one that opened it. A reply is
	// written by whoever replied, so identity is read from the root or from
	// nowhere.
	//
	// The comment's markdown source, which is what GitHub stores and what it
	// answers `body` with. A fingerprint marker is an HTML comment, so it is
	// present in the source and absent from anything rendered: reading
	// `bodyHTML` instead would return a document the marker had been rendered
	// out of, and every finding would look new.
	Body string
	// ByViewer reports whether this installation wrote that first comment.
	// A fingerprint marker in a comment somebody else wrote is a claim about
	// identity from an author who does not hold it.
	ByViewer bool
}

// reviewThreadsQuery reads one page of a pull request's comment threads.
//
// One comment per thread: the root is what agtk posted and therefore the only
// comment that carries a fingerprint marker agtk wrote. Asking for replies
// would fetch text no decision is made from, and enlarge a response that is
// read under a size cap.
const reviewThreadsQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      reviewThreads(first:25,after:$cursor){
        pageInfo{hasNextPage endCursor}
        nodes{
          path
          isResolved
          isOutdated
          comments(first:1){nodes{body viewerDidAuthor}}
        }
      }
    }
  }
}`

// ReadReviewThreads reads every comment thread on a pull request.
//
// The whole list or an error. A thread list that is silently short is a
// finding whose thread was not seen and is therefore posted a second time,
// which is the failure reading threads at all exists to prevent.
func (c *Client) ReadReviewThreads(ctx context.Context, number int) ([]ReviewThread, error) {
	if number < 1 {
		return nil, fmt.Errorf("%d is not a pull request number", number)
	}
	owner, repo, err := c.ownerRepo()
	if err != nil {
		return nil, err
	}

	var out []ReviewThread
	cursor := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("the comment threads on %s#%d did not end after %d pages of %d",
				c.slug, number, maxPages, pageSize)
		}
		var answer struct {
			Repository *struct {
				PullRequest *struct {
					ReviewThreads struct {
						PageInfo pageInfo `json:"pageInfo"`
						Nodes    []struct {
							Path       string `json:"path"`
							IsResolved bool   `json:"isResolved"`
							IsOutdated bool   `json:"isOutdated"`
							Comments   struct {
								Nodes []struct {
									Body            string `json:"body"`
									ViewerDidAuthor bool   `json:"viewerDidAuthor"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		vars := map[string]any{"owner": owner, "repo": repo, "number": number}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		if err := c.graphql(ctx, "review threads", reviewThreadsQuery, vars, &answer); err != nil {
			return nil, err
		}
		if answer.Repository == nil || answer.Repository.PullRequest == nil {
			return nil, fmt.Errorf("GitHub reported no pull request %d on %s", number, c.slug)
		}
		threads := answer.Repository.PullRequest.ReviewThreads
		for _, node := range threads.Nodes {
			thread := ReviewThread{Path: node.Path, Resolved: node.IsResolved, Outdated: node.IsOutdated}
			if len(node.Comments.Nodes) > 0 {
				thread.Body = node.Comments.Nodes[0].Body
				thread.ByViewer = node.Comments.Nodes[0].ViewerDidAuthor
			}
			out = append(out, thread)
		}
		if !threads.PageInfo.HasNextPage || threads.PageInfo.EndCursor == "" {
			return out, nil
		}
		cursor = threads.PageInfo.EndCursor
	}
}

// reviewedCommitsQuery reads which commits this installation has reviewed.
const reviewedCommitsQuery = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      reviews(first:25,after:$cursor){
        pageInfo{hasNextPage endCursor}
        nodes{commit{oid} viewerDidAuthor}
      }
    }
  }
}`

// ReadReviewedCommits reports which of a pull request's commits this
// installation has already posted a review for.
//
// The commit rather than the pull request, because a review is bound to a head
// — that binding is what makes "this commit was reviewed" a fact — and it is
// what decides whether re-running would re-derive what the pull request
// already displays.
func (c *Client) ReadReviewedCommits(ctx context.Context, number int) (map[string]bool, error) {
	if number < 1 {
		return nil, fmt.Errorf("%d is not a pull request number", number)
	}
	owner, repo, err := c.ownerRepo()
	if err != nil {
		return nil, err
	}

	reviewed := map[string]bool{}
	cursor := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("the reviews on %s#%d did not end after %d pages of %d",
				c.slug, number, maxPages, pageSize)
		}
		var answer struct {
			Repository *struct {
				PullRequest *struct {
					Reviews struct {
						PageInfo pageInfo `json:"pageInfo"`
						Nodes    []struct {
							Commit *struct {
								OID string `json:"oid"`
							} `json:"commit"`
							ViewerDidAuthor bool `json:"viewerDidAuthor"`
						} `json:"nodes"`
					} `json:"reviews"`
				} `json:"pullRequest"`
			} `json:"repository"`
		}
		vars := map[string]any{"owner": owner, "repo": repo, "number": number}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		if err := c.graphql(ctx, "posted reviews", reviewedCommitsQuery, vars, &answer); err != nil {
			return nil, err
		}
		if answer.Repository == nil || answer.Repository.PullRequest == nil {
			return nil, fmt.Errorf("GitHub reported no pull request %d on %s", number, c.slug)
		}
		reviews := answer.Repository.PullRequest.Reviews
		for _, node := range reviews.Nodes {
			if node.ViewerDidAuthor && node.Commit != nil && node.Commit.OID != "" {
				reviewed[node.Commit.OID] = true
			}
		}
		if !reviews.PageInfo.HasNextPage || reviews.PageInfo.EndCursor == "" {
			return reviewed, nil
		}
		cursor = reviews.PageInfo.EndCursor
	}
}
