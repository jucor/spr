package spr

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/git/mockgit"
	"github.com/ejoffe/spr/github"
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

// populateTwoPRs is populateThreePRs with two commits instead of three.
func populateTwoPRs(t *testing.T, s *stackediff, gitmock *mockgit.Mock,
	githubmock *mockclient.MockClient, output *bytes.Buffer,
	c1, c2 git.Commit) {
	githubmock.ExpectGetInfo()
	gitmock.ExpectFetch()
	gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
	gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2})
	githubmock.ExpectCreatePullRequest(c1, nil)
	githubmock.ExpectCreatePullRequest(c2, &c1)
	githubmock.ExpectUpdatePullRequest(c1, nil)
	githubmock.ExpectUpdatePullRequest(c2, &c1)
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
		githubmock.ExpectGetInfo() // wait loop's first, immediately-green iteration
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
		githubmock.ExpectGetInfo() // wait loop's first, immediately-green iteration
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

func TestMergeAllCascadeSquashMessages(t *testing.T) {
	testMergeAllCascadeSquashMessages(t, true)
	testMergeAllCascadeSquashMessages(t, false)
}

func testMergeAllCascadeSquashMessages(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeMethod = "squash"
		require.False(t, s.config.SquashPerPR(), "default granularity is perStack")
		ctx := context.Background()

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		c3 := git.Commit{CommitID: "00000003", CommitHash: "c300000000000000000000000000000000000000", Subject: "test commit 3"}
		populateThreePRs(t, s, gitmock, githubmock, output, c1, c2, c3)

		githubmock.ExpectGetInfo()
		githubmock.ExpectCommentPullRequest(c3)
		githubmock.ExpectMergePullRequestWithMessage(c3, genclient.PullRequestMergeMethod_SQUASH, "test commit 3")
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequestWithMessage(c2, genclient.PullRequestMergeMethod_SQUASH, "test commit 2")
		githubmock.ExpectGetInfo() // wait loop's first, immediately-green iteration
		githubmock.ExpectMergePullRequestWithMessage(c1, genclient.PullRequestMergeMethod_SQUASH, "test commit 1")
		s.MergePullRequests(ctx, nil)
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

// pendingBottomInfo returns a GitHubInfo whose PRs mirror src but with the
// bottom PR's checks still pending.
func pendingBottomInfo(src *github.GitHubInfo) *github.GitHubInfo {
	clone := *src
	clone.PullRequests = make([]*github.PullRequest, len(src.PullRequests))
	for i, pr := range src.PullRequests {
		prCopy := *pr
		clone.PullRequests[i] = &prCopy
	}
	clone.PullRequests[0].MergeStatus.ChecksPass = github.CheckStatusPending
	return &clone
}

// conflictedBottomInfo returns a GitHubInfo whose PRs mirror src but with the
// bottom PR now flagged as conflicting (same clone technique as
// pendingBottomInfo).
func conflictedBottomInfo(src *github.GitHubInfo) *github.GitHubInfo {
	clone := *src
	clone.PullRequests = make([]*github.PullRequest, len(src.PullRequests))
	for i, pr := range src.PullRequests {
		prCopy := *pr
		clone.PullRequests[i] = &prCopy
	}
	clone.PullRequests[0].MergeStatus.NoConflicts = false
	return &clone
}

func TestMergeAllCascadeWaitsForFinalChecks(t *testing.T) {
	// Synchronized only: the wait loop's GetInfo sequence must be ordered.
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		s.config.Repo.StackMergeMode = "mergeAll"
		assert := require.New(t)
		ctx := context.Background()
		var slept []time.Duration
		s.sleep = func(d time.Duration) { slept = append(slept, d) }

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		c3 := git.Commit{CommitID: "00000003", CommitHash: "c300000000000000000000000000000000000000", Subject: "test commit 3"}
		populateThreePRs(t, s, gitmock, githubmock, output, c1, c2, c3)

		// wait loop sees pending checks once, then green.
		githubmock.InfoQueue = []*github.GitHubInfo{
			githubmock.Info,                    // mergeAll's initial GetInfo
			pendingBottomInfo(githubmock.Info), // wait iteration 1: pending
			githubmock.Info,                    // wait iteration 2: green
		}
		githubmock.ExpectGetInfo()
		githubmock.ExpectCommentPullRequest(c3)
		githubmock.ExpectMergePullRequest(c3, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectGetInfo() // wait iteration 1
		githubmock.ExpectGetInfo() // wait iteration 2
		githubmock.ExpectMergePullRequest(c1, genclient.PullRequestMergeMethod_REBASE)
		s.MergePullRequests(ctx, nil)
		assert.Len(slept, 1, "one sleep between the two wait iterations")
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

func TestMergeAllBottomUpDisjoint(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeMethod = "squash"
		s.config.Repo.SquashGranularity = "perPR"
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2)

		infoAfterC1 := &github.GitHubInfo{
			UserName:     githubmock.Info.UserName,
			RepositoryID: githubmock.Info.RepositoryID,
			LocalBranch:  githubmock.Info.LocalBranch,
			// c1 landed: only c2's PR remains open, no conflicts (disjoint stack)
			PullRequests: []*github.PullRequest{githubmock.Info.PullRequests[1]},
		}
		infoEmpty := &github.GitHubInfo{
			UserName:     githubmock.Info.UserName,
			RepositoryID: githubmock.Info.RepositoryID,
			LocalBranch:  githubmock.Info.LocalBranch,
		}
		githubmock.InfoQueue = []*github.GitHubInfo{githubmock.Info, infoAfterC1, infoEmpty}

		githubmock.ExpectGetInfo()                  // iteration 1
		githubmock.ExpectUpdatePullRequest(c2, nil) // retarget next BEFORE merging bottom
		githubmock.ExpectMergePullRequestWithMessage(c1, genclient.PullRequestMergeMethod_SQUASH, "test commit 1")
		githubmock.ExpectGetInfo() // iteration 2
		githubmock.ExpectMergePullRequestWithMessage(c2, genclient.PullRequestMergeMethod_SQUASH, "test commit 2")
		githubmock.ExpectGetInfo() // iteration 3: empty -> loop ends
		s.MergePullRequests(ctx, nil)
		lines := strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		assert.Equal("MERGED   1 : test commit 2", lines[1])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

func TestMergeAllCascadeWaitTimeout(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		t.Setenv("SPR_DEBUG", "1") // check() panics instead of os.Exit
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeWaitTimeout = "5s" // one 5s sleep exhausts it
		assert := require.New(t)
		ctx := context.Background()
		s.sleep = func(d time.Duration) {}

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2) // same as populateThreePRs but two commits
		githubmock.InfoQueue = []*github.GitHubInfo{
			githubmock.Info,
			pendingBottomInfo(githubmock.Info), // pending forever
			pendingBottomInfo(githubmock.Info),
		}
		githubmock.ExpectGetInfo()
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectGetInfo()
		githubmock.ExpectGetInfo()
		defer func() {
			r := recover()
			assert.NotNil(r, "expected timeout panic under SPR_DEBUG")
			assert.Contains(fmt.Sprint(r), "timed out")
		}()
		s.MergePullRequests(ctx, nil)
	})
}

func TestMergeAllBottomUpRestackOnConflict(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeMethod = "squash"
		s.config.Repo.SquashGranularity = "perPR"
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		// c2Rebased models the same commit (same commit-id trailer) after the
		// restack's rebase rewrote it onto the new base: the hash changes even
		// though content/subject didn't, which is what makes syncCommitStackToGitHub
		// treat it as updated and push it again.
		c2Rebased := git.Commit{CommitID: "00000002", CommitHash: "c2b0000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2)

		onlyC2 := &github.GitHubInfo{UserName: githubmock.Info.UserName,
			RepositoryID: githubmock.Info.RepositoryID, LocalBranch: githubmock.Info.LocalBranch,
			PullRequests: []*github.PullRequest{githubmock.Info.PullRequests[1]}}
		conflicted := conflictedBottomInfo(onlyC2)
		infoEmpty := &github.GitHubInfo{UserName: githubmock.Info.UserName,
			RepositoryID: githubmock.Info.RepositoryID, LocalBranch: githubmock.Info.LocalBranch}
		githubmock.InfoQueue = []*github.GitHubInfo{
			githubmock.Info, // iter 1: both open
			conflicted,      // iter 2: c2 conflicted -> restack
			onlyC2,          // restack's own GetInfo (fetchAndGetGitHubInfo)
			onlyC2,          // restack's trailing StatusPullRequests GetInfo
			onlyC2,          // iter 3: c2 clean -> land it
			infoEmpty,       // iter 4: done
		}

		githubmock.ExpectGetInfo() // iter 1
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectMergePullRequestWithMessage(c1, genclient.PullRequestMergeMethod_SQUASH, "test commit 1")
		githubmock.ExpectGetInfo() // iter 2: conflicted
		// restack = UpdatePullRequests: GetInfo, fetch+rebase (drops c1), push, update, status GetInfo
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c2Rebased})
		gitmock.ExpectPushCommits([]*git.Commit{&c2Rebased})
		githubmock.ExpectUpdatePullRequest(c2Rebased, nil)
		githubmock.ExpectGetInfo()
		githubmock.ExpectGetInfo() // iter 3
		githubmock.ExpectMergePullRequestWithMessage(c2Rebased, genclient.PullRequestMergeMethod_SQUASH, "test commit 2")
		githubmock.ExpectGetInfo() // iter 4
		s.MergePullRequests(ctx, nil)
		lines := strings.Split(strings.TrimRight(output.String(), "\n"), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		// restack (the full UpdatePullRequests flow) ends with StatusPullRequests,
		// which prints c2's pre-landing status line into the same output buffer
		// before c2 itself lands below.
		assert.Equal("[vvvv]   1 : test commit 2", lines[1])
		assert.Equal("MERGED   1 : test commit 2", lines[2])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}
