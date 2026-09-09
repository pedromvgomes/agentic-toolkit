package githubapp

import (
	"context"
	"fmt"
)

// ThreadReply is one reply on a comment thread.
//
// Read for approval and never for a review run. A run decides what to post
// from the comment that opened a thread; approval decides what has been
// answered, and an answer is by definition something somebody wrote
// underneath.
type ThreadReply struct {
	// Body is the reply's markdown source.
	Body string
	// AuthorAssociation is what the author is to this repository — OWNER,
	// MEMBER, COLLABORATOR and so on. It is how write access is read, and it
	// is per comment rather than per thread: a thread agtk opened can carry
	// replies from the change's author and from a maintainer, and only one of
	// those clears a finding.
	AuthorAssociation string
}

// Associations that mean the author can push to this repository.
//
// Write access rather than anyone who can comment, because the author of a
// change is the party a review does not trust: a finding its own author could
// dismiss is one an injected instruction can dismiss too.
const (
	AssociationOwner        = "OWNER"
	AssociationMember       = "MEMBER"
	AssociationCollaborator = "COLLABORATOR"
)

// CanWrite reports whether this reply's author can push to the repository.
//
// A closed list of the associations that carry write access rather than a list
// of the ones that do not. GitHub can add an association, and an unknown one
// read as write access would let a stranger clear a finding — where an unknown
// one read as no access refuses an approval somebody can still get by other
// means.
func (r ThreadReply) CanWrite() bool {
	switch r.AuthorAssociation {
	case AssociationOwner, AssociationMember, AssociationCollaborator:
		return true
	}
	return false
}

// AnsweredThread is one comment thread together with the replies on it.
//
// A type of its own rather than a field on ReviewThread that one reader leaves
// empty. "No replies were read" and "there are no replies" are opposite
// answers to the question approval asks, and a caller holding this type knows
// which one an empty slice is.
type AnsweredThread struct {
	ReviewThread
	// Replies are the comments under the one that opened the thread, in the
	// order they were written.
	Replies []ThreadReply
	// Truncated reports that the thread carries more replies than were read.
	// A marking beyond the cut would be invisible, and approval refuses on
	// what it cannot see rather than granting on what it did not read.
	Truncated bool
}

// answeredThreadsQuery reads a pull request's comment threads with their
// replies.
//
// Its own query rather than more fields on the one a review run makes. That
// one asks for a single comment per thread and is read under a size cap; a
// pull request argued over for a week carries more comment text than the
// change, and enlarging every run's read to serve a command nobody has typed
// is how a review starts failing on a busy pull request.
var answeredThreadsQuery = fmt.Sprintf(`query($owner:String!,$repo:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      reviewThreads(first:%d,after:$cursor){
        pageInfo{hasNextPage endCursor}
        nodes{
          path
          isResolved
          isOutdated
          comments(first:%d){
            pageInfo{hasNextPage}
            nodes{body viewerDidAuthor authorAssociation}
          }
        }
      }
    }
  }
}`, answeredThreadsPerPage, commentsPerThread)

// How much of a pull request's conversation one page of this query carries.
//
// Smaller than the review run's page, because each node here brings a whole
// thread rather than one comment. The product is what has to stay under the
// response cap: a root comment agtk wrote is long, and the replies under it
// are usually a sentence.
const (
	answeredThreadsPerPage = 10
	commentsPerThread      = 20
)

// ReadAnsweredThreads reads every comment thread on a pull request together
// with the replies on it.
//
// The whole list or an error, the way the run's thread read is. A list that is
// silently short is a thread approval does not know is unresolved, and an
// approval granted over an open conversation is the failure reading threads at
// all exists to prevent.
func (c *Client) ReadAnsweredThreads(ctx context.Context, number int) ([]AnsweredThread, error) {
	if number < 1 {
		return nil, fmt.Errorf("%d is not a pull request number", number)
	}
	owner, repo, err := c.ownerRepo()
	if err != nil {
		return nil, err
	}

	var out []AnsweredThread
	cursor := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("the comment threads on %s#%d did not end after %d pages of %d",
				c.slug, number, maxPages, answeredThreadsPerPage)
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
								PageInfo pageInfo `json:"pageInfo"`
								Nodes    []struct {
									Body              string `json:"body"`
									ViewerDidAuthor   bool   `json:"viewerDidAuthor"`
									AuthorAssociation string `json:"authorAssociation"`
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
		if err := c.graphql(ctx, "answered threads", answeredThreadsQuery, vars, &answer); err != nil {
			return nil, err
		}
		if answer.Repository == nil || answer.Repository.PullRequest == nil {
			return nil, fmt.Errorf("GitHub reported no pull request %d on %s", number, c.slug)
		}
		threads := answer.Repository.PullRequest.ReviewThreads
		for _, node := range threads.Nodes {
			thread := AnsweredThread{
				ReviewThread: ReviewThread{Path: node.Path, Resolved: node.IsResolved, Outdated: node.IsOutdated},
				Truncated:    node.Comments.PageInfo.HasNextPage,
			}
			for i, comment := range node.Comments.Nodes {
				// The first comment is the one that opened the thread, and it
				// is the only one identity is read from: a reply is written by
				// whoever replied, so it may assert that a finding is wrong
				// and may never assert which finding it is.
				if i == 0 {
					thread.Body = comment.Body
					thread.ByViewer = comment.ViewerDidAuthor
					continue
				}
				thread.Replies = append(thread.Replies, ThreadReply{
					Body:              comment.Body,
					AuthorAssociation: comment.AuthorAssociation,
				})
			}
			out = append(out, thread)
		}
		if !threads.PageInfo.HasNextPage || threads.PageInfo.EndCursor == "" {
			return out, nil
		}
		cursor = threads.PageInfo.EndCursor
	}
}

// submittedReviewsQuery reads the reviews on a pull request with their bodies.
var submittedReviewsQuery = fmt.Sprintf(`query($owner:String!,$repo:String!,$number:Int!,$cursor:String){
  repository(owner:$owner,name:$repo){
    pullRequest(number:$number){
      reviews(first:%d,after:$cursor){
        pageInfo{hasNextPage endCursor}
        nodes{body commit{oid} viewerDidAuthor}
      }
    }
  }
}`, reviewsPerPage)

// reviewsPerPage is how many review bodies one page carries. A review body
// states everything the review found that no comment carries, so it is the
// largest single document on a pull request.
const reviewsPerPage = 10

// SubmittedReview is one review on a pull request, as approval reads it.
type SubmittedReview struct {
	// CommitSHA is the head the review was bound to, which is what makes
	// "this commit was reviewed" a fact rather than "this pull request was".
	CommitSHA string
	// Body is the review's markdown source, which is where the review marker
	// lives. An HTML comment is present in the source and absent from anything
	// rendered.
	Body string
	// ByViewer reports whether this installation posted it. A marker is only
	// ever believed from the account that writes it: anyone who can review a
	// pull request can type the characters that open one.
	ByViewer bool
}

// ReadSubmittedReviews reads every review on a pull request, in the order they
// were submitted.
//
// Separate from ReadPriorReviews, which asks the same connection for the
// commit alone. A review run reads that one before spending a panel and does
// not need a byte of any body; approval needs the body and is one command a
// person typed.
func (c *Client) ReadSubmittedReviews(ctx context.Context, number int) ([]SubmittedReview, error) {
	if number < 1 {
		return nil, fmt.Errorf("%d is not a pull request number", number)
	}
	owner, repo, err := c.ownerRepo()
	if err != nil {
		return nil, err
	}

	var out []SubmittedReview
	cursor := ""
	for page := 0; ; page++ {
		if page >= maxPages {
			return nil, fmt.Errorf("the reviews on %s#%d did not end after %d pages of %d",
				c.slug, number, maxPages, reviewsPerPage)
		}
		var answer struct {
			Repository *struct {
				PullRequest *struct {
					Reviews struct {
						PageInfo pageInfo `json:"pageInfo"`
						Nodes    []struct {
							Body   string `json:"body"`
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
		if err := c.graphql(ctx, "submitted reviews", submittedReviewsQuery, vars, &answer); err != nil {
			return nil, err
		}
		if answer.Repository == nil || answer.Repository.PullRequest == nil {
			return nil, fmt.Errorf("GitHub reported no pull request %d on %s", number, c.slug)
		}
		reviews := answer.Repository.PullRequest.Reviews
		for _, node := range reviews.Nodes {
			review := SubmittedReview{Body: node.Body, ByViewer: node.ViewerDidAuthor}
			if node.Commit != nil {
				review.CommitSHA = node.Commit.OID
			}
			out = append(out, review)
		}
		if !reviews.PageInfo.HasNextPage || reviews.PageInfo.EndCursor == "" {
			return out, nil
		}
		cursor = reviews.PageInfo.EndCursor
	}
}
