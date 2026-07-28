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
		githubmock.ExpectMergePullRequestDirect(c3, genclient.PullRequestMergeMethod_REBASE, "")
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequestDirect(c2, genclient.PullRequestMergeMethod_REBASE, "")
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

func TestMergeAllCascadeMergeQueue(t *testing.T) {
	testMergeAllCascadeMergeQueue(t, true)
	testMergeAllCascadeMergeQueue(t, false)
}

// testMergeAllCascadeMergeQueue mirrors testMergeAllCascadeThreeCommits but
// with mergeQueue enabled: internal merges still go direct (identical to
// the non-queue case - internal merges never touch the queue), but the
// final merge routes through MergePullRequest's config-driven queue path
// (no wait for br_0's checks - the queue runs its own CI), and PR_0 is
// reported as enqueued rather than merged.
func testMergeAllCascadeMergeQueue(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeQueue = true
		assert := require.New(t)
		ctx := context.Background()
		var stateSaves int
		s.saveState = func() { stateSaves++ }

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		c3 := git.Commit{CommitID: "00000003", CommitHash: "c300000000000000000000000000000000000000", Subject: "test commit 3"}
		populateThreePRs(t, s, gitmock, githubmock, output, c1, c2, c3)
		key := githubmock.Info.Key()

		// cascade: comment+direct-merge top-down for the internal PRs, then
		// the final PR is enqueued (plain MergePullRequest, config routing
		// handles the queue) - no wait-loop GetInfo at all.
		githubmock.ExpectGetInfo()
		githubmock.ExpectCommentPullRequest(c3)
		githubmock.ExpectMergePullRequestDirect(c3, genclient.PullRequestMergeMethod_REBASE, "")
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequestDirect(c2, genclient.PullRequestMergeMethod_REBASE, "")
		githubmock.ExpectMergePullRequest(c1, genclient.PullRequestMergeMethod_REBASE)
		s.MergePullRequests(ctx, nil)
		lines := strings.Split(output.String(), "\n")
		// c2/c3 are genuinely merged already; c1 (PR_0) is only enqueued,
		// printed last.
		assert.Equal("MERGED   1 : test commit 2", lines[0])
		assert.Equal("MERGED   1 : test commit 3", lines[1])
		assert.True(strings.HasPrefix(lines[2], "added to merge queue:"), "got: %q", lines[2])
		// the queue branch clears the in-progress flag at its own successful
		// end too - not just the non-queue path (Task 10's invariant).
		assert.False(s.config.State.MergeAllInProgress[key])
		assert.Equal(2, stateSaves, "mark once, clear once")
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
		githubmock.ExpectMergePullRequestDirect(c2, genclient.PullRequestMergeMethod_REBASE, "")
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
		githubmock.ExpectMergePullRequestDirect(c3, genclient.PullRequestMergeMethod_SQUASH, "test commit 3")
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequestDirect(c2, genclient.PullRequestMergeMethod_SQUASH, "test commit 2")
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
		githubmock.ExpectMergePullRequestDirect(c3, genclient.PullRequestMergeMethod_REBASE, "")
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequestDirect(c2, genclient.PullRequestMergeMethod_REBASE, "")
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

// TestMergeAllBottomUpMergeQueue mirrors TestMergeAllBottomUpDisjoint but
// with mergeQueue enabled: each landing's MergePullRequest call only
// enqueues the PR (config-driven routing), so bottomUpMerge must call
// waitUntilMerged and poll GetInfo until the queue actually merges it
// before treating the PR as landed and moving on.
func TestMergeAllBottomUpMergeQueue(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeMethod = "squash"
		s.config.Repo.SquashGranularity = "perPR"
		s.config.Repo.MergeQueue = true
		assert := require.New(t)
		ctx := context.Background()
		var slept []time.Duration
		s.sleep = func(d time.Duration) { slept = append(slept, d) }

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
		githubmock.InfoQueue = []*github.GitHubInfo{
			githubmock.Info, // iteration 1
			githubmock.Info, // waitUntilMerged(c1) poll 1: c1 still open
			infoAfterC1,     // waitUntilMerged(c1) poll 2: c1 gone -> merged
			infoAfterC1,     // iteration 2
			infoEmpty,       // waitUntilMerged(c2) poll 1: c2 gone -> merged
			infoEmpty,       // iteration 3: empty -> loop ends
		}

		githubmock.ExpectGetInfo()                                                                                 // iteration 1
		githubmock.ExpectUpdatePullRequest(c2, nil)                                                                // retarget next BEFORE merging bottom
		githubmock.ExpectMergePullRequestWithMessage(c1, genclient.PullRequestMergeMethod_SQUASH, "test commit 1") // queue path: enqueue
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged(c1) poll 1
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged(c1) poll 2
		githubmock.ExpectGetInfo()                                                                                 // iteration 2
		githubmock.ExpectMergePullRequestWithMessage(c2, genclient.PullRequestMergeMethod_SQUASH, "test commit 2") // queue path: enqueue
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged(c2) poll 1
		githubmock.ExpectGetInfo()                                                                                 // iteration 3: empty -> loop ends
		s.MergePullRequests(ctx, nil)
		lines := strings.Split(output.String(), "\n")
		// waitUntilMerged(c1)'s first poll finds c1 still open (not yet
		// actually merged by the queue), so it prints a "waiting" line
		// before its retry finds it gone; waitUntilMerged(c2) finds it gone
		// on its very first poll, so no such line precedes c2's MERGED line.
		assert.Contains(lines[0], "waiting for the merge queue to merge PR #1")
		assert.Equal("MERGED   1 : test commit 1", lines[1])
		assert.Equal("MERGED   1 : test commit 2", lines[2])
		assert.NotEmpty(slept, "waitUntilMerged(c1) must sleep between its two polls")
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

// TestMergeAllBottomUpMergeQueueDeletesBranchesAfterWait mirrors
// TestMergeAllBottomUpMergeQueue but with DeleteMergedBranches enabled: the
// branch delete for a landed PR must fire only AFTER waitUntilMerged has
// confirmed the queue actually merged it, never before (deleting the
// branch of a PR that's merely enqueued, not yet merged, would be
// premature and could race the queue).
func TestMergeAllBottomUpMergeQueueDeletesBranchesAfterWait(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeMethod = "squash"
		s.config.Repo.SquashGranularity = "perPR"
		s.config.Repo.MergeQueue = true
		s.config.User.DeleteMergedBranches = true
		assert := require.New(t)
		ctx := context.Background()
		s.sleep = func(d time.Duration) {}

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
		githubmock.InfoQueue = []*github.GitHubInfo{
			githubmock.Info, // iteration 1
			githubmock.Info, // waitUntilMerged(c1) poll 1: c1 still open
			infoAfterC1,     // waitUntilMerged(c1) poll 2: c1 gone -> merged
			infoAfterC1,     // iteration 2
			infoEmpty,       // waitUntilMerged(c2) poll 1: c2 gone -> merged
			infoEmpty,       // iteration 3: empty -> loop ends
		}

		githubmock.ExpectGetInfo()                                                                                 // iteration 1
		githubmock.ExpectUpdatePullRequest(c2, nil)                                                                // retarget next BEFORE merging bottom
		githubmock.ExpectMergePullRequestWithMessage(c1, genclient.PullRequestMergeMethod_SQUASH, "test commit 1") // queue path: enqueue
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged(c1) poll 1
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged(c1) poll 2 - confirms merged
		gitmock.ExpectDeleteBranch("from_branch")                                                                 // delete happens AFTER the wait confirms c1 merged
		githubmock.ExpectGetInfo()                                                                                 // iteration 2
		githubmock.ExpectMergePullRequestWithMessage(c2, genclient.PullRequestMergeMethod_SQUASH, "test commit 2") // queue path: enqueue
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged(c2) poll 1 - confirms merged
		gitmock.ExpectDeleteBranch("from_branch")                                                                 // delete happens AFTER the wait confirms c2 merged
		githubmock.ExpectGetInfo()                                                                                 // iteration 3: empty -> loop ends
		s.MergePullRequests(ctx, nil)
		lines := strings.Split(output.String(), "\n")
		assert.Contains(lines[0], "waiting for the merge queue to merge PR #1")
		assert.Equal("MERGED   1 : test commit 1", lines[1])
		assert.Equal("MERGED   1 : test commit 2", lines[2])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

// TestMergeAllBottomUpMergeQueueTimeout mirrors TestMergeAllCascadeWaitTimeout
// for the bottom-up + mergeQueue combination: the queue never actually
// lands the bottom PR (InfoQueue keeps serving it as still open), so
// waitUntilMerged must eventually time out instead of polling forever.
func TestMergeAllBottomUpMergeQueueTimeout(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		t.Setenv("SPR_DEBUG", "1") // check() panics instead of os.Exit
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeMethod = "squash"
		s.config.Repo.SquashGranularity = "perPR"
		s.config.Repo.MergeQueue = true
		s.config.Repo.MergeWaitTimeout = "5s" // one 5s sleep exhausts it
		assert := require.New(t)
		ctx := context.Background()
		s.sleep = func(d time.Duration) {}

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2)

		// the bottom PR (c1) never disappears from the open PRs - the queue
		// never actually lands it.
		githubmock.InfoQueue = []*github.GitHubInfo{
			githubmock.Info, // iteration 1
			githubmock.Info, // waitUntilMerged poll 1: still open
			githubmock.Info, // waitUntilMerged poll 2: still open -> timeout
		}
		githubmock.ExpectGetInfo()                                                                                 // iteration 1
		githubmock.ExpectUpdatePullRequest(c2, nil)                                                                // retarget next BEFORE merging bottom
		githubmock.ExpectMergePullRequestWithMessage(c1, genclient.PullRequestMergeMethod_SQUASH, "test commit 1") // queue path: enqueue
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged poll 1
		githubmock.ExpectGetInfo()                                                                                 // waitUntilMerged poll 2
		defer func() {
			r := recover()
			assert.NotNil(r, "expected timeout panic under SPR_DEBUG")
			assert.Contains(fmt.Sprint(r), "timed out waiting for the merge queue")
		}()
		s.MergePullRequests(ctx, nil)
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
		githubmock.ExpectMergePullRequestDirect(c2, genclient.PullRequestMergeMethod_REBASE, "")
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

func TestMergeAllSetsAndClearsStateAndBlocksUpdate(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		t.Setenv("SPR_DEBUG", "1")
		s.config.Repo.StackMergeMode = "mergeAll"
		assert := require.New(t)
		ctx := context.Background()
		var stateSaves int
		s.saveState = func() { stateSaves++ }

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2)
		key := githubmock.Info.Key()

		// a successful cascade sets the flag then clears it (2 saves)
		githubmock.ExpectGetInfo()
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectMergePullRequestDirect(c2, genclient.PullRequestMergeMethod_REBASE, "")
		githubmock.ExpectGetInfo() // final wait iteration
		githubmock.ExpectMergePullRequest(c1, genclient.PullRequestMergeMethod_REBASE)
		s.MergePullRequests(ctx, nil)
		assert.False(s.config.State.MergeAllInProgress[key])
		assert.Equal(2, stateSaves)
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// with the flag set (simulating a crashed run), spr update refuses
		s.config.State.MergeAllInProgress[key] = true
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		func() {
			defer func() {
				r := recover()
				assert.NotNil(r, "update must refuse during an in-progress mergeAll")
				assert.Contains(fmt.Sprint(r), "merge in progress")
			}()
			s.UpdatePullRequests(ctx, nil, nil)
		}()
	})
}

// TestMergeAllBottomUpResumeNoOpClearsStaleFlag covers the CRITICAL fix:
// a resumed bottom-up run whose very first GetInfo already finds nothing
// open (e.g. both PRs landed elsewhere before the crash) must still clear
// a stale persisted flag, even though this call's own local `marked` never
// went true.
func TestMergeAllBottomUpResumeNoOpClearsStaleFlag(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		s.config.Repo.StackMergeMode = "mergeAll"
		s.config.Repo.MergeMethod = "squash"
		s.config.Repo.SquashGranularity = "perPR"
		assert := require.New(t)
		ctx := context.Background()
		var stateSaves int
		s.saveState = func() { stateSaves++ }

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2)
		key := githubmock.Info.Key()
		output.Reset()

		// simulate a crashed prior run: flag stuck true, but by the time we
		// resume both PRs are already gone (landed / closed elsewhere) -
		// nothing left open for bottomUpMerge to do.
		s.config.State.MergeAllInProgress[key] = true
		emptyInfo := &github.GitHubInfo{
			UserName:     githubmock.Info.UserName,
			RepositoryID: githubmock.Info.RepositoryID,
			LocalBranch:  githubmock.Info.LocalBranch,
		}
		githubmock.InfoQueue = []*github.GitHubInfo{emptyInfo}
		githubmock.ExpectGetInfo()
		s.MergePullRequests(ctx, nil)
		assert.False(s.config.State.MergeAllInProgress[key])
		assert.Equal(1, stateSaves, "only the clear should save - never marked, nothing to land")
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

// TestMergeAllCascadeNoMergeableResumeClearsStaleFlag covers the CRITICAL fix
// for the cascade route: topMergeableIndex returning -1 (no PR list is
// even mergeable) used to bail out via check() without ever touching a
// stale persisted flag. It must now clear the flag BEFORE the fatal
// check() panic, so the guard's own "re-run 'git spr merge'" hint is true.
func TestMergeAllCascadeNoMergeableResumeClearsStaleFlag(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		t.Setenv("SPR_DEBUG", "1")
		s.config.Repo.StackMergeMode = "mergeAll"
		assert := require.New(t)
		ctx := context.Background()
		var stateSaves int
		s.saveState = func() { stateSaves++ }

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2)
		key := githubmock.Info.Key()
		output.Reset()

		// simulate a crashed prior run: flag stuck true, but by the time we
		// resume there's nothing mergeable at all (empty PR list -> -1).
		s.config.State.MergeAllInProgress[key] = true
		emptyInfo := &github.GitHubInfo{
			UserName:     githubmock.Info.UserName,
			RepositoryID: githubmock.Info.RepositoryID,
			LocalBranch:  githubmock.Info.LocalBranch,
		}
		githubmock.InfoQueue = []*github.GitHubInfo{emptyInfo}
		githubmock.ExpectGetInfo()
		func() {
			defer func() {
				r := recover()
				assert.NotNil(r, "expected the no-mergeable-PRs panic under SPR_DEBUG")
				assert.Contains(fmt.Sprint(r), "no mergeable pull requests found")
			}()
			s.MergePullRequests(ctx, nil)
		}()
		assert.False(s.config.State.MergeAllInProgress[key])
		assert.Equal(1, stateSaves, "the flag was repaired before the fatal check() panic")
		assert.Contains(output.String(), "cleared stale mergeAll-in-progress flag")
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}

// TestMergeAllCascadeMidResumeKeepsFlagWhenNotYetMergeable covers the
// CRITICAL fix: topMergeableIndex returning -1 while PRs are still open
// (none mergeable yet) must NOT be treated as automatically stale just
// because the persisted flag is set. This is the primary resume scenario
// the guard protects: a previous mergeAll cascade already landed its
// internal merges and the final wait timed out (the flag was deliberately
// left set), and the user re-ran while the bottom PR's checks are still
// pending. Clearing the flag here would let a subsequent 'spr update'
// force-push and duplicate PRs, so the flag must stay set and the panic
// message must point at the actual blocker instead of the generic
// "nothing mergeable" fatal.
func TestMergeAllCascadeMidResumeKeepsFlagWhenNotYetMergeable(t *testing.T) {
	t.Run("Sync: true", func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, true)
		t.Setenv("SPR_DEBUG", "1")
		s.config.Repo.StackMergeMode = "mergeAll"
		assert := require.New(t)
		ctx := context.Background()
		var stateSaves int
		s.saveState = func() { stateSaves++ }

		c1 := git.Commit{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit 1"}
		c2 := git.Commit{CommitID: "00000002", CommitHash: "c200000000000000000000000000000000000000", Subject: "test commit 2"}
		populateTwoPRs(t, s, gitmock, githubmock, output, c1, c2)
		key := githubmock.Info.Key()
		output.Reset()

		// simulate a resumed run mid-cascade: flag stuck true (a previous
		// cascade's internal merges already landed and the final wait timed
		// out, leaving the flag deliberately set), and both remaining PRs
		// are still pending checks - none mergeable yet.
		s.config.State.MergeAllInProgress[key] = true
		bothPending := pendingBottomInfo(githubmock.Info)
		bothPending.PullRequests[1].MergeStatus.ChecksPass = github.CheckStatusPending
		githubmock.InfoQueue = []*github.GitHubInfo{bothPending}
		githubmock.ExpectGetInfo()
		func() {
			defer func() {
				r := recover()
				assert.NotNil(r, "expected the not-yet-mergeable panic under SPR_DEBUG")
				assert.Contains(fmt.Sprint(r), "not yet mergeable")
			}()
			s.MergePullRequests(ctx, nil)
		}()
		assert.True(s.config.State.MergeAllInProgress[key], "flag must stay set - the cascade is still mid-resume")
		assert.Equal(0, stateSaves, "no state write on this path - the flag was already correct")
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
	})
}
