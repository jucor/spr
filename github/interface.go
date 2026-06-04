package github

import (
	"context"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
)

type GitHubInterface interface {
	// GetInfo returns the list of pull requests from GitHub which match the local stack of commits
	GetInfo(ctx context.Context, gitcmd git.GitInterface) *GitHubInfo

	// GetClosedOrphanPRs returns PRs in state CLOSED-not-merged that match
	// the user's spr branch prefix. These are "orphan" PRs whose content
	// was absorbed by an upstream squash (the user ran `spr merge` on a
	// PR higher up the stack, and GitHub closed the lower ones without a
	// real merge event). Returned PRs have .Commit.CommitID populated
	// from the HeadRefName so callers can match them to local commits
	// without re-parsing trailers. Returns an empty slice if the query
	// returns no matches; returns nil on transport error so callers can
	// distinguish "no orphans" from "couldn't tell".
	//
	// Used by spr's update flow (cfg.User.NoPruneOrphans=false, default)
	// to identify local change IDs that should be `jj abandon`-ed before
	// rebase, avoiding the cascade-orphan conflict cascade.
	GetClosedOrphanPRs(ctx context.Context) []*PullRequest

	// GetAssignableUsers returns a list of valid GitHub users that can review the pull request
	GetAssignableUsers(ctx context.Context) []RepoAssignee

	// CreatePullRequest creates a pull request
	CreatePullRequest(ctx context.Context, gitcmd git.GitInterface, info *GitHubInfo, commit git.Commit, prevCommit *git.Commit) *PullRequest

	// UpdatePullRequest updates a pull request with current commit
	UpdatePullRequest(ctx context.Context, gitcmd git.GitInterface, info *GitHubInfo, pullRequests []*PullRequest, pr *PullRequest, commit git.Commit, prevCommit *git.Commit)

	// AddReviewers adds a reviewer to the given pull request
	AddReviewers(ctx context.Context, pr *PullRequest, userIDs []string)

	// CommentPullRequest add a comment to the given pull request
	CommentPullRequest(ctx context.Context, pr *PullRequest, comment string)

	// MergePullRequest merged the given pull request
	MergePullRequest(ctx context.Context, pr *PullRequest, mergeMethod genclient.PullRequestMergeMethod)

	// ClosePullRequest closes the given pull request
	ClosePullRequest(ctx context.Context, pr *PullRequest)
}

type GitHubInfo struct {
	UserName     string
	RepositoryID string
	LocalBranch  string
	PullRequests []*PullRequest
}

type RepoAssignee struct {
	ID    string
	Login string
	Name  string
}

func (i *GitHubInfo) Key() string {
	return i.RepositoryID + "_" + i.LocalBranch
}
