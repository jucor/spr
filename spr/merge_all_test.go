package spr

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/git/mockgit"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/ejoffe/spr/github/mockclient"
	"github.com/stretchr/testify/require"
)

// populateThreePRs runs the standard update flow so the mock Info holds
// PRs for c1..c3 (same technique as TestSPRMergeCount).
func populateThreePRs(t *testing.T, s *stackediff, gitmock *mockgit.Mock,
	githubmock *mockclient.MockClient, output *bytes.Buffer,
	c1, c2, c3 git.Commit) {
	githubmock.ExpectGetInfo()
	gitmock.ExpectFetch()
	gitmock.ExpectLogAndRespond([]*git.Commit{&c3, &c2, &c1})
	gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2, &c3})
	githubmock.ExpectCreatePullRequest(c1, nil)
	githubmock.ExpectCreatePullRequest(c2, &c1)
	githubmock.ExpectCreatePullRequest(c3, &c2)
	githubmock.ExpectUpdatePullRequest(c1, nil)
	githubmock.ExpectUpdatePullRequest(c2, &c1)
	githubmock.ExpectUpdatePullRequest(c3, &c2)
	githubmock.ExpectGetInfo()
	s.UpdatePullRequests(context.Background(), nil, nil)
	gitmock.ExpectationsMet()
	githubmock.ExpectationsMet()
	output.Reset()
}

func TestMergeAllCascadeThreeCommits(t *testing.T) {
	testMergeAllCascadeThreeCommits(t, true)
	testMergeAllCascadeThreeCommits(t, false)
}

func testMergeAllCascadeThreeCommits(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		s.config.Repo.StackMergeMode = "mergeAll"
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		c3 := git.Commit{CommitID: "00000003", CommitHash: "c300000000000000000000000000000000000000", Subject: "test commit 3"}
		populateThreePRs(t, s, gitmock, githubmock, output, c1, c2, c3)

		// cascade: comment+merge top-down into existing bases, then final merge.
		// No UpdatePullRequest (no retarget!) and no ClosePullRequest, ever.
		githubmock.ExpectGetInfo()
		githubmock.ExpectCommentPullRequest(c3)
		githubmock.ExpectMergePullRequest(c3, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectMergePullRequest(c1, genclient.PullRequestMergeMethod_REBASE)
		s.MergePullRequests(ctx, nil)
		lines := strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		assert.Equal("MERGED   1 : test commit 2", lines[1])
		assert.Equal("MERGED   1 : test commit 3", lines[2])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

func TestMergeAllCascadeCountWithSurvivor(t *testing.T) {
	testMergeAllCascadeCountWithSurvivor(t, true)
	testMergeAllCascadeCountWithSurvivor(t, false)
}

func testMergeAllCascadeCountWithSurvivor(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.User.DeleteMergedBranches = true
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		c3 := git.Commit{CommitID: "00000003", CommitHash: "c300000000000000000000000000000000000000", Subject: "test commit 3"}
		populateThreePRs(t, s, gitmock, githubmock, output, c1, c2, c3)

		githubmock.ExpectGetInfo()
		// survivor c3 explicitly retargeted to master BEFORE the first merge
		githubmock.ExpectUpdatePullRequest(c3, nil)
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		gitmock.ExpectDeleteBranch("from_branch")
		githubmock.ExpectMergePullRequest(c1, genclient.PullRequestMergeMethod_REBASE)
		gitmock.ExpectDeleteBranch("from_branch")
		count := uint(2)
		s.MergePullRequests(ctx, &count)
		lines := strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		assert.Equal("MERGED   1 : test commit 2", lines[1])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}
