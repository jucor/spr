package spr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/git/mockgit"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
	"github.com/ejoffe/spr/github/mockclient"
	"github.com/ejoffe/spr/vcs"
	"github.com/stretchr/testify/require"
)

func makeTestObjects(t *testing.T, synchronized bool) (
	s *stackediff, gitmock *mockgit.Mock, githubmock *mockclient.MockClient,
	input *bytes.Buffer, output *bytes.Buffer) {
	cfg := config.EmptyConfig()
	cfg.Repo.RequireChecks = true
	cfg.Repo.RequireApproval = true
	cfg.Repo.GitHubRemote = "origin"
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.MergeMethod = "rebase"
	cfg.User.BranchPrefix = "spr"
	gitmock = mockgit.NewMockGit(t)
	githubmock = mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{
		UserName:     "TestSPR",
		RepositoryID: "RepoID",
		LocalBranch:  "master",
	}
	s = NewStackedPR(cfg, githubmock, gitmock, vcs.NewGitOps(cfg, gitmock))
	output = &bytes.Buffer{}
	s.output = output
	input = &bytes.Buffer{}
	s.input = input
	s.synchronized = synchronized
	githubmock.Synchronized = synchronized
	return
}

func TestSPRBasicFlowFourCommitsQueue(t *testing.T) {
	testSPRBasicFlowFourCommitsQueue(t, true)
	testSPRBasicFlowFourCommitsQueue(t, false)
}

func testSPRBasicFlowFourCommitsQueue(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}
		c3 := git.Commit{
			CommitID:   "00000003",
			CommitHash: "c300000000000000000000000000000000000000",
			Subject:    "test commit 3",
		}
		c4 := git.Commit{
			CommitID:   "00000004",
			CommitHash: "c400000000000000000000000000000000000000",
			Subject:    "test commit 4",
		}

		// 'git spr status' :: StatusPullRequest
		githubmock.ExpectGetInfo()
		s.StatusPullRequests(ctx)
		assert.Equal("pull request stack is empty\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1})
		githubmock.ExpectCreatePullRequest(c1, nil)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("[vvvv]   1 : test commit 1\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c2})
		githubmock.ExpectCreatePullRequest(c2, &c1)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		lines := strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("warning: not updating reviewers for PR #1", lines[0])
		assert.Equal("[vvvv]   1 : test commit 2", lines[1])
		assert.Equal("[vvvv]   1 : test commit 1", lines[2])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2, c3, c4]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c4, &c3, &c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c3, &c4})

		// For the first "create" call we should call GetAssignableUsers
		githubmock.ExpectCreatePullRequest(c3, &c2)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})

		// For the first "create" call we should *not* call GetAssignableUsers
		githubmock.ExpectCreatePullRequest(c4, &c3)
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})

		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectUpdatePullRequest(c3, &c2)
		githubmock.ExpectUpdatePullRequest(c4, &c3)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		lines = strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal([]string{
			"warning: not updating reviewers for PR #1",
			"warning: not updating reviewers for PR #1",
			"[vvvv]   1 : test commit 4",
			"[vvvv]   1 : test commit 3",
			"[vvvv]   1 : test commit 2",
			"[vvvv]   1 : test commit 1",
		}, lines[:6])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr merge' :: MergePullRequest :: commits=[a1, a2]
		githubmock.ExpectGetInfo()
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectCommentPullRequest(c1)
		githubmock.ExpectClosePullRequest(c1)
		count := uint(2)
		s.MergePullRequests(ctx, &count)
		lines = strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		assert.Equal("MERGED   1 : test commit 2", lines[1])
		fmt.Printf("OUT: %s\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		githubmock.Info.PullRequests = githubmock.Info.PullRequests[1:]
		githubmock.Info.PullRequests[0].Merged = false
		githubmock.Info.PullRequests[0].Commits = append(githubmock.Info.PullRequests[0].Commits, c1, c2)
		githubmock.ExpectGetInfo()
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectUpdatePullRequest(c3, &c2)
		githubmock.ExpectUpdatePullRequest(c4, &c3)
		githubmock.ExpectGetInfo()

		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c4, &c3, &c2, &c1})
		gitmock.ExpectStatus()

		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		lines = strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal([]string{
			"warning: not updating reviewers for PR #1",
			"warning: not updating reviewers for PR #1",
			"warning: not updating reviewers for PR #1",
			"[vvvv]   1 : test commit 4",
			"[vvvv]   1 : test commit 3",
			"[vvvv] !   1 : test commit 2",
		}, lines[:6])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr merge' :: MergePullRequest :: commits=[a2, a3, a4]
		githubmock.ExpectGetInfo()
		githubmock.ExpectUpdatePullRequest(c4, nil)
		githubmock.ExpectMergePullRequest(c4, genclient.PullRequestMergeMethod_REBASE)

		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectClosePullRequest(c2)
		githubmock.ExpectCommentPullRequest(c3)
		githubmock.ExpectClosePullRequest(c3)

		githubmock.Info.PullRequests[0].InQueue = true

		s.MergePullRequests(ctx, nil)
		lines = strings.Split(output.String(), "\n")
		assert.Equal("MERGED .   1 : test commit 2", lines[0])
		assert.Equal("MERGED   1 : test commit 3", lines[1])
		assert.Equal("MERGED   1 : test commit 4", lines[2])
		fmt.Printf("OUT: %s\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()
	})
}

func TestSPRBasicFlowFourCommits(t *testing.T) {
	testSPRBasicFlowFourCommits(t, true)
	testSPRBasicFlowFourCommits(t, false)
}

func testSPRBasicFlowFourCommits(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}
		c3 := git.Commit{
			CommitID:   "00000003",
			CommitHash: "c300000000000000000000000000000000000000",
			Subject:    "test commit 3",
		}
		c4 := git.Commit{
			CommitID:   "00000004",
			CommitHash: "c400000000000000000000000000000000000000",
			Subject:    "test commit 4",
		}

		// 'git spr status' :: StatusPullRequest
		githubmock.ExpectGetInfo()
		s.StatusPullRequests(ctx)
		assert.Equal("pull request stack is empty\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1})
		githubmock.ExpectCreatePullRequest(c1, nil)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("[vvvv]   1 : test commit 1\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c2})
		githubmock.ExpectCreatePullRequest(c2, &c1)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		lines := strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("warning: not updating reviewers for PR #1", lines[0])
		assert.Equal("[vvvv]   1 : test commit 2", lines[1])
		assert.Equal("[vvvv]   1 : test commit 1", lines[2])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2, c3, c4]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c4, &c3, &c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c3, &c4})

		// For the first "create" call we should call GetAssignableUsers
		githubmock.ExpectCreatePullRequest(c3, &c2)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})

		// For the first "create" call we should *not* call GetAssignableUsers
		githubmock.ExpectCreatePullRequest(c4, &c3)
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})

		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectUpdatePullRequest(c3, &c2)
		githubmock.ExpectUpdatePullRequest(c4, &c3)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		lines = strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal([]string{
			"warning: not updating reviewers for PR #1",
			"warning: not updating reviewers for PR #1",
			"[vvvv]   1 : test commit 4",
			"[vvvv]   1 : test commit 3",
			"[vvvv]   1 : test commit 2",
			"[vvvv]   1 : test commit 1",
		}, lines[:6])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr merge' :: MergePullRequest :: commits=[a1, a2, a3, a4]
		githubmock.ExpectGetInfo()
		githubmock.ExpectUpdatePullRequest(c4, nil)
		githubmock.ExpectMergePullRequest(c4, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectCommentPullRequest(c1)
		githubmock.ExpectClosePullRequest(c1)
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectClosePullRequest(c2)
		githubmock.ExpectCommentPullRequest(c3)
		githubmock.ExpectClosePullRequest(c3)
		s.MergePullRequests(ctx, nil)
		lines = strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		assert.Equal("MERGED   1 : test commit 2", lines[1])
		assert.Equal("MERGED   1 : test commit 3", lines[2])
		assert.Equal("MERGED   1 : test commit 4", lines[3])
		fmt.Printf("OUT: %s\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()
	})
}

func TestSPRBasicFlowDeleteBranch(t *testing.T) {
	testSPRBasicFlowDeleteBranch(t, true)
	testSPRBasicFlowDeleteBranch(t, false)
}

func testSPRBasicFlowDeleteBranch(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		s.config.User.DeleteMergedBranches = true
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}

		// 'git spr update' :: UpdatePullRequest :: commits=[c1]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1})
		githubmock.ExpectCreatePullRequest(c1, nil)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("[vvvv]   1 : test commit 1\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c2})
		githubmock.ExpectCreatePullRequest(c2, &c1)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		lines := strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("warning: not updating reviewers for PR #1", lines[0])
		assert.Equal("[vvvv]   1 : test commit 2", lines[1])
		assert.Equal("[vvvv]   1 : test commit 1", lines[2])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr merge' :: MergePullRequest :: commits=[a1, a2]
		githubmock.ExpectGetInfo()
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		gitmock.ExpectDeleteBranch("from_branch") // <--- This is the key expectation of this test.
		githubmock.ExpectCommentPullRequest(c1)
		githubmock.ExpectClosePullRequest(c1)
		gitmock.ExpectDeleteBranch("from_branch") // <--- This is the key expectation of this test.
		s.MergePullRequests(ctx, nil)
		lines = strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		fmt.Printf("OUT: %s\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()
	})
}

func TestSPRMergeCount(t *testing.T) {
	testSPRMergeCount(t, true)
	testSPRMergeCount(t, false)
}

func testSPRMergeCount(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}
		c3 := git.Commit{
			CommitID:   "00000003",
			CommitHash: "c300000000000000000000000000000000000000",
			Subject:    "test commit 3",
		}
		c4 := git.Commit{
			CommitID:   "00000004",
			CommitHash: "c400000000000000000000000000000000000000",
			Subject:    "test commit 4",
		}

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2, c3, c4]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c4, &c3, &c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2, &c3, &c4})
		// For the first "create" call we should call GetAssignableUsers
		githubmock.ExpectCreatePullRequest(c1, nil)
		githubmock.ExpectGetAssignableUsers()
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectCreatePullRequest(c2, &c1)
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectCreatePullRequest(c3, &c2)
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectCreatePullRequest(c4, &c3)
		githubmock.ExpectAddReviewers([]string{mockclient.NobodyUserID})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectUpdatePullRequest(c3, &c2)
		githubmock.ExpectUpdatePullRequest(c4, &c3)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, []string{mockclient.NobodyLogin}, nil)
		lines := strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal([]string{
			"[vvvv]   1 : test commit 4",
			"[vvvv]   1 : test commit 3",
			"[vvvv]   1 : test commit 2",
			"[vvvv]   1 : test commit 1",
		}, lines[:4])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr merge --count 2' :: MergePullRequest :: commits=[a1, a2, a3, a4]
		githubmock.ExpectGetInfo()
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectCommentPullRequest(c1)
		githubmock.ExpectClosePullRequest(c1)
		s.MergePullRequests(ctx, uintptr(2))
		lines = strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		assert.Equal("MERGED   1 : test commit 2", lines[1])
		fmt.Printf("OUT: %s\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()
	})
}

func TestSPRAmendCommit(t *testing.T) {
	testSPRAmendCommit(t, true)
	testSPRAmendCommit(t, false)
}

func testSPRAmendCommit(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}

		// 'git spr state' :: StatusPullRequest
		githubmock.ExpectGetInfo()
		s.StatusPullRequests(ctx)
		assert.Equal("pull request stack is empty\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2})
		githubmock.ExpectCreatePullRequest(c1, nil)
		githubmock.ExpectCreatePullRequest(c2, &c1)
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, nil, nil)
		fmt.Printf("OUT: %s\n", output.String())
		lines := strings.Split(output.String(), "\n")
		assert.Equal("[vvvv]   1 : test commit 2", lines[0])
		assert.Equal("[vvvv]   1 : test commit 1", lines[1])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// amend commit c2
		c2.CommitHash = "c201000000000000000000000000000000000000"
		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c2})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, nil, nil)
		lines = strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("[vvvv]   1 : test commit 2", lines[0])
		assert.Equal("[vvvv]   1 : test commit 1", lines[1])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// amend commit c1
		c1.CommitHash = "c101000000000000000000000000000000000000"
		c2.CommitHash = "c202000000000000000000000000000000000000"
		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, nil, nil)
		lines = strings.Split(output.String(), "\n")
		fmt.Printf("OUT: %s\n", output.String())
		assert.Equal("[vvvv]   1 : test commit 2", lines[0])
		assert.Equal("[vvvv]   1 : test commit 1", lines[1])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr merge' :: MergePullRequest :: commits=[a1, a2]
		githubmock.ExpectGetInfo()
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectMergePullRequest(c2, genclient.PullRequestMergeMethod_REBASE)
		githubmock.ExpectCommentPullRequest(c1)
		githubmock.ExpectClosePullRequest(c1)
		s.MergePullRequests(ctx, nil)
		lines = strings.Split(output.String(), "\n")
		assert.Equal("MERGED   1 : test commit 1", lines[0])
		assert.Equal("MERGED   1 : test commit 2", lines[1])
		fmt.Printf("OUT: %s\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()
	})
}

func TestSPRReorderCommit(t *testing.T) {
	testSPRReorderCommit(t, true)
	testSPRReorderCommit(t, false)
}

func testSPRReorderCommit(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}
		c3 := git.Commit{
			CommitID:   "00000003",
			CommitHash: "c300000000000000000000000000000000000000",
			Subject:    "test commit 3",
		}
		c4 := git.Commit{
			CommitID:   "00000004",
			CommitHash: "c400000000000000000000000000000000000000",
			Subject:    "test commit 4",
		}
		c5 := git.Commit{
			CommitID:   "00000005",
			CommitHash: "c500000000000000000000000000000000000000",
			Subject:    "test commit 5",
		}

		// 'git spr status' :: StatusPullRequest
		githubmock.ExpectGetInfo()
		s.StatusPullRequests(ctx)
		assert.Equal("pull request stack is empty\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2, c3, c4]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c4, &c3, &c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2, &c3, &c4})
		githubmock.ExpectCreatePullRequest(c1, nil)
		githubmock.ExpectCreatePullRequest(c2, &c1)
		githubmock.ExpectCreatePullRequest(c3, &c2)
		githubmock.ExpectCreatePullRequest(c4, &c3)
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectUpdatePullRequest(c3, &c2)
		githubmock.ExpectUpdatePullRequest(c4, &c3)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, nil, nil)
		fmt.Printf("OUT: %s\n", output.String())
		lines := strings.Split(output.String(), "\n")
		assert.Equal("[vvvv]   1 : test commit 4", lines[0])
		assert.Equal("[vvvv]   1 : test commit 3", lines[1])
		assert.Equal("[vvvv]   1 : test commit 2", lines[2])
		assert.Equal("[vvvv]   1 : test commit 1", lines[3])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c2, c4, c1, c3]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c3, &c1, &c4, &c2})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectUpdatePullRequest(c3, nil)
		githubmock.ExpectUpdatePullRequest(c4, nil)
		// reorder commits
		c1.CommitHash = "c101000000000000000000000000000000000000"
		c2.CommitHash = "c201000000000000000000000000000000000000"
		c3.CommitHash = "c301000000000000000000000000000000000000"
		c4.CommitHash = "c401000000000000000000000000000000000000"
		gitmock.ExpectPushCommits([]*git.Commit{&c2, &c4, &c1, &c3})
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectUpdatePullRequest(c4, &c2)
		githubmock.ExpectUpdatePullRequest(c1, &c4)
		githubmock.ExpectUpdatePullRequest(c3, &c1)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, nil, nil)
		fmt.Printf("OUT: %s\n", output.String())
		// TODO : Need to update pull requests in GetInfo expect to get this check to work
		// lines = strings.Split(output.String(), "\n")
		//assert.Equal("[vvvv]   1 : test commit 3", lines[0])
		//assert.Equal("[vvvv]   1 : test commit 1", lines[1])
		//assert.Equal("[vvvv]   1 : test commit 4", lines[2])
		//assert.Equal("[vvvv]   1 : test commit 2", lines[3])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c5, c1, c2, c3, c4]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c1, &c2, &c3, &c4, &c5})
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, nil)
		githubmock.ExpectUpdatePullRequest(c3, nil)
		githubmock.ExpectUpdatePullRequest(c4, nil)
		// reorder commits
		c1.CommitHash = "c102000000000000000000000000000000000000"
		c2.CommitHash = "c202000000000000000000000000000000000000"
		c3.CommitHash = "c302000000000000000000000000000000000000"
		c4.CommitHash = "c402000000000000000000000000000000000000"
		gitmock.ExpectPushCommits([]*git.Commit{&c5, &c4, &c3, &c2, &c1})
		githubmock.ExpectCreatePullRequest(c5, nil)
		githubmock.ExpectUpdatePullRequest(c5, nil)
		githubmock.ExpectUpdatePullRequest(c4, &c5)
		githubmock.ExpectUpdatePullRequest(c3, &c4)
		githubmock.ExpectUpdatePullRequest(c2, &c3)
		githubmock.ExpectUpdatePullRequest(c1, &c2)
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, nil, nil)
		fmt.Printf("OUT: %s\n", output.String())
		// TODO : Need to update pull requests in GetInfo expect to get this check to work
		// lines = strings.Split(output.String(), "\n")
		//assert.Equal("[vvvv]   1 : test commit 5", lines[0])
		//assert.Equal("[vvvv]   1 : test commit 4", lines[1])
		//assert.Equal("[vvvv]   1 : test commit 3", lines[2])
		//assert.Equal("[vvvv]   1 : test commit 2", lines[3])
		//assert.Equal("[vvvv]   1 : test commit 1", lines[4])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// TODO : add a call to merge and check merge order
	})
}

func TestSPRDeleteCommit(t *testing.T) {
	testSPRDeleteCommit(t, true)
	testSPRDeleteCommit(t, false)
}

func testSPRDeleteCommit(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, githubmock, _, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}
		c3 := git.Commit{
			CommitID:   "00000003",
			CommitHash: "c300000000000000000000000000000000000000",
			Subject:    "test commit 3",
		}
		c4 := git.Commit{
			CommitID:   "00000004",
			CommitHash: "c400000000000000000000000000000000000000",
			Subject:    "test commit 4",
		}

		// 'git spr status' :: StatusPullRequest
		githubmock.ExpectGetInfo()
		s.StatusPullRequests(ctx)
		assert.Equal("pull request stack is empty\n", output.String())
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c1, c2, c3, c4]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c4, &c3, &c2, &c1})
		gitmock.ExpectPushCommits([]*git.Commit{&c1, &c2, &c3, &c4})
		githubmock.ExpectCreatePullRequest(c1, nil)
		githubmock.ExpectCreatePullRequest(c2, &c1)
		githubmock.ExpectCreatePullRequest(c3, &c2)
		githubmock.ExpectCreatePullRequest(c4, &c3)
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c2, &c1)
		githubmock.ExpectUpdatePullRequest(c3, &c2)
		githubmock.ExpectUpdatePullRequest(c4, &c3)
		githubmock.ExpectGetInfo()

		s.UpdatePullRequests(ctx, nil, nil)
		fmt.Printf("OUT: %s\n", output.String())
		lines := strings.Split(output.String(), "\n")
		assert.Equal("[vvvv]   1 : test commit 4", lines[0])
		assert.Equal("[vvvv]   1 : test commit 3", lines[1])
		assert.Equal("[vvvv]   1 : test commit 2", lines[2])
		assert.Equal("[vvvv]   1 : test commit 1", lines[3])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// 'git spr update' :: UpdatePullRequest :: commits=[c2, c4, c1, c3]
		githubmock.ExpectGetInfo()
		gitmock.ExpectFetch()
		gitmock.ExpectLogAndRespond([]*git.Commit{&c4, &c1})
		githubmock.ExpectCommentPullRequest(c2)
		githubmock.ExpectClosePullRequest(c2)
		githubmock.ExpectCommentPullRequest(c3)
		githubmock.ExpectClosePullRequest(c3)
		// update commits
		c1.CommitHash = "c101000000000000000000000000000000000000"
		c4.CommitHash = "c401000000000000000000000000000000000000"
		githubmock.ExpectUpdatePullRequest(c1, nil)
		githubmock.ExpectUpdatePullRequest(c4, &c1)
		gitmock.ExpectPushCommits([]*git.Commit{&c1, &c4})
		githubmock.ExpectGetInfo()
		s.UpdatePullRequests(ctx, nil, nil)
		fmt.Printf("OUT: %s\n", output.String())
		// TODO : Need to update pull requests in GetInfo expect to get this check to work
		// lines = strings.Split(output.String(), "\n")
		//assert.Equal("[vvvv]   1 : test commit 3", lines[0])
		//assert.Equal("[vvvv]   1 : test commit 1", lines[1])
		//assert.Equal("[vvvv]   1 : test commit 4", lines[2])
		//assert.Equal("[vvvv]   1 : test commit 2", lines[3])
		gitmock.ExpectationsMet()
		githubmock.ExpectationsMet()
		output.Reset()

		// TODO : add a call to merge and check merge order
	})
}

func TestAmendNoCommits(t *testing.T) {
	testAmendNoCommits(t, true)
	testAmendNoCommits(t, false)
}

func testAmendNoCommits(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, _, _, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		gitmock.ExpectLogAndRespond([]*git.Commit{})
		s.AmendCommit(ctx)
		assert.Equal("No commits to amend\n", output.String())
	})
}

func TestAmendOneCommit(t *testing.T) {
	testAmendOneCommit(t, true)
	testAmendOneCommit(t, false)
}

func testAmendOneCommit(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, _, input, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
		gitmock.ExpectFixup(c1.CommitHash)
		input.WriteString("1")
		s.AmendCommit(ctx)
		assert.Equal(" 1 : 00000001 : test commit 1\nCommit to amend (1): ", output.String())
	})
}

func TestAmendTwoCommits(t *testing.T) {
	testAmendTwoCommits(t, true)
	testAmendTwoCommits(t, false)
}

func testAmendTwoCommits(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, _, input, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}
		c2 := git.Commit{
			CommitID:   "00000002",
			CommitHash: "c200000000000000000000000000000000000000",
			Subject:    "test commit 2",
		}
		gitmock.ExpectLogAndRespond([]*git.Commit{&c1, &c2})
		gitmock.ExpectFixup(c2.CommitHash)
		input.WriteString("1")
		s.AmendCommit(ctx)
		assert.Equal(" 2 : 00000001 : test commit 1\n 1 : 00000002 : test commit 2\nCommit to amend (1-2): ", output.String())
	})
}

func TestAmendInvalidInput(t *testing.T) {
	testAmendInvalidInput(t, true)
	testAmendInvalidInput(t, false)
}

func testAmendInvalidInput(t *testing.T, sync bool) {
	t.Run(fmt.Sprintf("Sync: %v", sync), func(t *testing.T) {
		s, gitmock, _, input, output := makeTestObjects(t, sync)
		assert := require.New(t)
		ctx := context.Background()

		c1 := git.Commit{
			CommitID:   "00000001",
			CommitHash: "c100000000000000000000000000000000000000",
			Subject:    "test commit 1",
		}

		gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
		input.WriteString("a")
		s.AmendCommit(ctx)
		assert.Equal(" 1 : 00000001 : test commit 1\nCommit to amend (1): Invalid input\n", output.String())
		gitmock.ExpectationsMet()
		output.Reset()

		gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
		input.WriteString("0")
		s.AmendCommit(ctx)
		assert.Equal(" 1 : 00000001 : test commit 1\nCommit to amend (1): Invalid input\n", output.String())
		gitmock.ExpectationsMet()
		output.Reset()

		gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
		input.WriteString("2")
		s.AmendCommit(ctx)
		assert.Equal(" 1 : 00000001 : test commit 1\nCommit to amend (1): Invalid input\n", output.String())
		gitmock.ExpectationsMet()
		output.Reset()
	})
}

func TestSPRFetchOverridesNoFetchConfig(t *testing.T) {
	// Test that --fetch flag overrides noFetch config (sets NoFetch back to false)
	// This simulates: yaml has noFetch: true, user passes --fetch on CLI
	s, gitmock, githubmock, _, output := makeTestObjects(t, true)
	assert := require.New(t)
	ctx := context.Background()

	// Start with NoFetch true (as if loaded from yaml config)
	s.config.User.NoFetch = true
	// Then override it back to false (as the --fetch flag would do)
	s.config.User.NoFetch = false

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	// With NoFetch=false, fetch should run
	githubmock.ExpectGetInfo()
	gitmock.ExpectFetch()
	gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
	gitmock.ExpectPushCommits([]*git.Commit{&c1})
	githubmock.ExpectCreatePullRequest(c1, nil)
	githubmock.ExpectUpdatePullRequest(c1, nil)
	githubmock.ExpectGetInfo()
	s.UpdatePullRequests(ctx, nil, nil)
	assert.Equal("[vvvv]   1 : test commit 1\n", output.String())
	gitmock.ExpectationsMet()
	githubmock.ExpectationsMet()
	output.Reset()
}

func uintptr(a uint) *uint {
	return &a
}

// setupEditTest creates test objects with a temp directory as the git root,
// including the .git subdirectory, so edit state files can be written.
func setupEditTest(t *testing.T) (
	s *stackediff, gitmock *mockgit.Mock,
	input *bytes.Buffer, output *bytes.Buffer, tmpDir string) {
	t.Helper()
	s, gitmock, _, input, output = makeTestObjects(t, true)
	tmpDir = t.TempDir()
	err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755)
	require.NoError(t, err)
	gitmock.SetRootDir(tmpDir)
	return
}

func TestEditCommitNoCommits(t *testing.T) {
	s, gitmock, _, output, _ := setupEditTest(t)
	ctx := context.Background()

	gitmock.ExpectLogAndRespond([]*git.Commit{})
	s.EditCommit(ctx)
	require.Equal(t, "No commits to edit\n", output.String())
	gitmock.ExpectationsMet()
}

func TestEditCommitSelectCommit(t *testing.T) {
	s, gitmock, input, output, tmpDir := setupEditTest(t)
	ctx := context.Background()

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}
	c2 := git.Commit{
		CommitID:   "00000002",
		CommitHash: "c200000000000000000000000000000000000000",
		Subject:    "test commit 2",
	}

	gitmock.ExpectLogAndRespond([]*git.Commit{&c2, &c1})
	gitmock.ExpectEditStart()

	// Select commit 1 (bottom of stack)
	input.WriteString("1\n")
	s.EditCommit(ctx)

	// Verify state file was created
	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	_, err := os.Stat(stateFile)
	require.NoError(t, err, "state file should exist after starting edit")

	// Verify output includes editing message
	require.Contains(t, output.String(), "Editing commit 1")
	require.Contains(t, output.String(), "git spr edit --done")

	gitmock.ExpectationsMet()
}

func TestEditCommitAlreadyEditing(t *testing.T) {
	s, _, _, output, tmpDir := setupEditTest(t)
	ctx := context.Background()

	// Create the state file to simulate an active edit session
	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	err := os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644)
	require.NoError(t, err)

	s.EditCommit(ctx)
	require.Contains(t, output.String(), "Already editing a commit")
}

func TestEditCommitInvalidInput(t *testing.T) {
	s, gitmock, input, output, _ := setupEditTest(t)
	ctx := context.Background()

	c1 := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		Subject:    "test commit 1",
	}

	gitmock.ExpectLogAndRespond([]*git.Commit{&c1})
	input.WriteString("abc\n")
	s.EditCommit(ctx)
	require.Contains(t, output.String(), "Invalid input")
	gitmock.ExpectationsMet()
}

func TestEditCommitDoneNotEditing(t *testing.T) {
	s, _, _, output, _ := setupEditTest(t)
	ctx := context.Background()

	// No state file exists — not editing
	s.EditCommitDone(ctx, false)
	require.Equal(t, "No edit session in progress.\n", output.String())
}

func TestEditCommitDoneHappyPath(t *testing.T) {
	s, gitmock, _, output, tmpDir := setupEditTest(t)
	ctx := context.Background()

	// Create state file to simulate active edit session
	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	err := os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644)
	require.NoError(t, err)

	// No REBASE_HEAD — we're at the initial edit stop
	gitmock.ExpectEditDoneAmend()

	s.EditCommitDone(ctx, false)

	require.Contains(t, output.String(), "Stack restored successfully")

	// Verify state file was cleaned up
	_, err = os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should be removed after successful done")

	gitmock.ExpectationsMet()
}

func TestEditCommitDoneWithConflict(t *testing.T) {
	s, gitmock, _, output, tmpDir := setupEditTest(t)
	ctx := context.Background()

	// Create state file to simulate active edit session
	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	err := os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644)
	require.NoError(t, err)

	// No REBASE_HEAD — initial edit stop, but rebase --continue will conflict
	gitmock.ExpectEditDoneAmendWithConflict()

	s.EditCommitDone(ctx, false)

	require.Contains(t, output.String(), "Rebase conflict detected")

	// State file should still exist (session not complete)
	_, err = os.Stat(stateFile)
	require.NoError(t, err, "state file should still exist after conflict")

	gitmock.ExpectationsMet()
}

func TestEditCommitDoneConflictResolution(t *testing.T) {
	s, gitmock, _, output, tmpDir := setupEditTest(t)
	ctx := context.Background()

	// Create state file to simulate active edit session
	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	err := os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644)
	require.NoError(t, err)

	// Create REBASE_HEAD to simulate that we're in a conflict resolution state.
	// This is the critical distinction: REBASE_HEAD exists means git stopped due
	// to a conflict, NOT at an edit point. The fix should NOT amend here.
	rebaseHeadFile := filepath.Join(tmpDir, ".git", "REBASE_HEAD")
	err = os.WriteFile(rebaseHeadFile, []byte("abc123\n"), 0644)
	require.NoError(t, err)

	// Expect the conflict resolution path: add -A then rebase --continue (NO amend)
	gitmock.ExpectEditDoneConflictResolved()

	s.EditCommitDone(ctx, false)

	require.Contains(t, output.String(), "Stack restored successfully")

	// Verify state file was cleaned up
	_, err = os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should be removed after successful done")

	gitmock.ExpectationsMet()
}

func TestEditCommitDoneConflictThenResolution(t *testing.T) {
	// This tests the full scenario that was buggy:
	// 1. User edits bottom commit, runs --done
	// 2. Amend succeeds, but rebase --continue conflicts on a later commit
	// 3. User resolves conflict, runs --done again
	// 4. This time it should NOT amend — just rebase --continue

	s, gitmock, _, output, tmpDir := setupEditTest(t)
	ctx := context.Background()

	// --- Phase 1: Initial --done, rebase hits conflict ---

	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	err := os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644)
	require.NoError(t, err)

	// No REBASE_HEAD — at the initial edit stop
	gitmock.ExpectEditDoneAmendWithConflict()

	s.EditCommitDone(ctx, false)
	require.Contains(t, output.String(), "Rebase conflict detected")
	gitmock.ExpectationsMet()
	output.Reset()

	// --- Phase 2: User resolves conflict, runs --done again ---

	// Now REBASE_HEAD exists (git created it when the conflict occurred)
	rebaseHeadFile := filepath.Join(tmpDir, ".git", "REBASE_HEAD")
	err = os.WriteFile(rebaseHeadFile, []byte("abc123\n"), 0644)
	require.NoError(t, err)

	// Expect the conflict resolution path: NO amend, just rebase --continue
	gitmock.ExpectEditDoneConflictResolved()

	s.EditCommitDone(ctx, false)
	require.Contains(t, output.String(), "Stack restored successfully")

	// Verify state file was cleaned up
	_, err = os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should be removed after final done")

	gitmock.ExpectationsMet()
}

func TestEditCommitAbort(t *testing.T) {
	s, gitmock, _, output, tmpDir := setupEditTest(t)
	ctx := context.Background()

	// Create state file
	stateFile := filepath.Join(tmpDir, ".git", "spr_edit_state")
	err := os.WriteFile(stateFile, []byte("commit_id=00000001\n"), 0644)
	require.NoError(t, err)

	gitmock.ExpectEditAbort()

	s.EditCommitAbort(ctx)

	require.Contains(t, output.String(), "Edit session aborted")

	// Verify state file was cleaned up
	_, err = os.Stat(stateFile)
	require.True(t, os.IsNotExist(err), "state file should be removed after abort")

	gitmock.ExpectationsMet()
}

func TestEditCommitAbortNotEditing(t *testing.T) {
	s, _, _, output, _ := setupEditTest(t)
	ctx := context.Background()

	s.EditCommitAbort(ctx)
	require.Equal(t, "No edit session in progress.\n", output.String())
}

func TestStatusPullRequestsTextMode(t *testing.T) {
	s, _, githubmock, _, output := makeTestObjects(t, true)
	assert := require.New(t)
	ctx := context.Background()
	s.TextEnabled = true

	s.config.Repo.GitHubHost = "github.com"
	s.config.Repo.GitHubRepoOwner = "testowner"
	s.config.Repo.GitHubRepoName = "testrepo"

	// Text mode with empty stack should print nothing
	githubmock.ExpectGetInfo()
	s.StatusPullRequests(ctx)
	assert.Equal("", output.String())
	githubmock.ExpectationsMet()
	output.Reset()

	// Text mode with PRs should print "<url> : <title>" for each PR
	githubmock.Info.PullRequests = []*github.PullRequest{
		{
			Number: 1,
			Title:  "first PR",
			Commit: git.Commit{CommitID: "00000001"},
		},
		{
			Number: 2,
			Title:  "second PR",
			Commit: git.Commit{CommitID: "00000002"},
		},
	}
	githubmock.ExpectGetInfo()
	s.StatusPullRequests(ctx)
	lines := strings.Split(strings.TrimRight(output.String(), "\n"), "\n")
	assert.Equal(2, len(lines))
	// PRs are printed in reverse order (top of stack first)
	assert.Equal("https://github.com/testowner/testrepo/pull/2 : second PR", lines[0])
	assert.Equal("https://github.com/testowner/testrepo/pull/1 : first PR", lines[1])
	githubmock.ExpectationsMet()
}

// --- Edit command name tests ---

// stubVcsOps is a minimal VCSOperations stub for testing user-facing messages.
type stubVcsOps struct {
	editing      bool
	commandName  string
	commits      []git.Commit
	stackWarning string // returned by CheckStackCompleteness

	// spy flags: set to true when the corresponding method is called.
	editStartCalled  bool
	editFinishCalled bool
	editAbortCalled  bool
	fetchCalled      bool

	// injectable errors: returned by the corresponding method when non-nil.
	editStartError  error
	editFinishError error
	editAbortError  error
	fetchError      error
}

func (s *stubVcsOps) FetchAndRebase(cfg *config.Config) error { return nil }
func (s *stubVcsOps) Fetch() error                            { s.fetchCalled = true; return s.fetchError }
func (s *stubVcsOps) GetLocalCommitStack() ([]git.Commit, error) {
	return s.commits, nil
}
func (s *stubVcsOps) AmendInto(commit git.Commit) error { return nil }
func (s *stubVcsOps) EditStart(commit git.Commit) error {
	s.editStartCalled = true
	return s.editStartError
}
func (s *stubVcsOps) EditFinish() error { s.editFinishCalled = true; return s.editFinishError }
func (s *stubVcsOps) EditAbort() error  { s.editAbortCalled = true; return s.editAbortError }
func (s *stubVcsOps) PrepareForPush() (func(), error)            { return func() {}, nil }
func (s *stubVcsOps) PushBranches(cfg *config.Config, commits []git.Commit, individually bool) error {
	return nil
}
func (s *stubVcsOps) IsEditing() bool            { return s.editing }
func (s *stubVcsOps) EditStatePath() string       { return "" }
func (s *stubVcsOps) CheckStackCompleteness() string { return s.stackWarning }
func (s *stubVcsOps) CommandName() string         { return s.commandName }

func TestEditCommit_AlreadyEditing_UsesCommandName(t *testing.T) {
	tests := []struct {
		name        string
		commandName string
		wantPrefix  string
	}{
		{"jj mode", "jj spr", "jj spr"},
		{"git mode", "git spr", "git spr"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.EmptyConfig()
			cfg.Repo.GitHubBranch = "master"
			gitmock := mockgit.NewMockGit(t)
			githubmock := mockclient.NewMockClient(t)
			githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}

			stub := &stubVcsOps{editing: true, commandName: tt.commandName}
			s := NewStackedPR(cfg, githubmock, gitmock, stub)
			output := &bytes.Buffer{}
			s.output = output

			s.EditCommit(context.Background())

			require.Contains(t, output.String(), tt.wantPrefix+" edit --done")
			require.Contains(t, output.String(), tt.wantPrefix+" edit --abort")
		})
	}
}

// TestEditCommit_GitMode_CallsEditStart pins that git mode's behavior is
// unchanged: EditStart is called and the session-style instructions are
// printed.
func TestEditCommit_GitMode_CallsEditStart(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}

	stub := &stubVcsOps{
		editing:     false,
		commandName: "git spr",
		commits: []git.Commit{
			{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test commit"},
		},
	}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output
	input := bytes.NewBufferString("1\n")
	s.input = input

	s.EditCommit(context.Background())

	out := output.String()
	require.Contains(t, out, "git spr edit --done")
	require.Contains(t, out, "git spr edit --abort")
	require.True(t, stub.editStartCalled, "EditStart must be called in git mode")
}

func TestEditCommitDone_GitMode_EditFinishError(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}
	stub := &stubVcsOps{
		commandName:     "git spr",
		editing:         true,
		editFinishError: fmt.Errorf("amend failed: working tree dirty"),
	}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.EditCommitDone(context.Background(), false)

	out := output.String()
	require.Contains(t, out, "Edit finish failed")
	require.Contains(t, out, "amend failed")
}

// TestEditCommitDone_GitMode_RebaseConflict pins the pre-refactor UX for
// the rebase-conflict path: when EditFinish returns vcs.ErrRebaseConflict,
// spr.go must print the specific recovery hint ("Resolve conflicts and
// run '<cmd> edit --done' again.") rather than a generic failure message.
func TestEditCommitDone_GitMode_RebaseConflict(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}
	stub := &stubVcsOps{
		commandName:     "git spr",
		editing:         true,
		editFinishError: fmt.Errorf("%w: exit status 1", vcs.ErrRebaseConflict),
	}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.EditCommitDone(context.Background(), false)

	out := output.String()
	require.Contains(t, out, "Rebase conflict detected")
	require.Contains(t, out, "git spr edit --done")
	require.NotContains(t, out, "Edit finish failed",
		"conflict path must use the specific hint, not the generic failure message")
}

func TestEditCommitAbort_GitMode_EditAbortError(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}
	stub := &stubVcsOps{
		commandName:    "git spr",
		editing:        true,
		editAbortError: fmt.Errorf("rebase --abort failed: dirty working tree"),
	}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.EditCommitAbort(context.Background())

	out := output.String()
	require.Contains(t, out, "Failed to abort")
	require.Contains(t, out, "dirty working tree")
	require.NotContains(t, out, "Edit session aborted", "must NOT report success after error")
}

func TestEditCommitDone_GitMode_NoSessionInProgress(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}
	stub := &stubVcsOps{commandName: "git spr", editing: false}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.EditCommitDone(context.Background(), false)

	require.Contains(t, output.String(), "No edit session in progress")
	require.False(t, stub.editFinishCalled)
}

func TestEditCommitAbort_GitMode_NoSessionInProgress(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}
	stub := &stubVcsOps{commandName: "git spr", editing: false}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.EditCommitAbort(context.Background())

	require.Contains(t, output.String(), "No edit session in progress")
	require.False(t, stub.editAbortCalled)
}

func TestEditCommitDone_GitMode_CallsEditFinish(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{UserName: "test", RepositoryID: "repo", LocalBranch: "master"}
	stub := &stubVcsOps{commandName: "git spr", editing: true}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.EditCommitDone(context.Background(), false)

	require.True(t, stub.editFinishCalled, "EditFinish must be called in git mode")
}

// --- MergePullRequests: MergeCheck gating ---
//
// When MergeCheck is configured, spr refuses to merge unless `spr check` has
// been run successfully against the current top commit (or the user has
// explicitly recorded "SKIP"). These tests pin all three branches.

func makeMergeCheckTest(t *testing.T, lastCommitHash, checkedCommit string, found bool) (*stackediff, *bytes.Buffer, *mockclient.MockClient) {
	t.Helper()
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.MergeCheck = "echo ok"
	if found {
		cfg.State.MergeCheckCommit = map[string]string{
			"repo_master": checkedCommit, // key = RepositoryID + "_" + LocalBranch
		}
	}
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{
		UserName:     "test",
		RepositoryID: "repo",
		LocalBranch:  "master",
	}
	stub := &stubVcsOps{
		commandName: "git spr",
		commits: []git.Commit{
			{CommitID: "00000001", CommitHash: lastCommitHash, Subject: "the top"},
		},
	}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output
	return s, output, githubmock
}

func TestMergePullRequests_MergeCheckGate_NeverRun(t *testing.T) {
	t.Setenv("SPR_DEBUG", "1") // make check() panic instead of os.Exit
	s, _, githubmock := makeMergeCheckTest(t, "topHash", "", false)
	githubmock.ExpectGetInfo()

	require.PanicsWithError(t,
		"need to run merge check 'spr check' before merging",
		func() { s.MergePullRequests(context.Background(), nil) })
}

func TestMergePullRequests_MergeCheckGate_StaleHash(t *testing.T) {
	t.Setenv("SPR_DEBUG", "1")
	s, _, githubmock := makeMergeCheckTest(t, "currentTopHash", "oldHashFromBefore", true)
	githubmock.ExpectGetInfo()

	require.PanicsWithError(t,
		"need to run merge check 'spr check' before merging",
		func() { s.MergePullRequests(context.Background(), nil) })
}

func TestMergePullRequests_MergeCheckGate_ExplicitSkip(t *testing.T) {
	t.Setenv("SPR_DEBUG", "1")
	s, _, githubmock := makeMergeCheckTest(t, "topHash", "SKIP", true)
	githubmock.ExpectGetInfo()

	// With "SKIP" recorded, MergeCheck is bypassed. The flow continues
	// past the gate and (because Info has no PRs) eventually hits the
	// "no mergeable pull requests" path. The contract pinned here is
	// that the FAILURE is the no-PR one, NOT the merge-check one.
	require.PanicsWithError(t,
		"no mergeable pull requests found in the stack",
		func() { s.MergePullRequests(context.Background(), nil) })
}

// --- fetchAndGetGitHubInfo: refuse on spr-branch ---

// If the user is checked out on a spr-pushed branch (e.g. spr/master/abcd1234),
// spr.UpdatePullRequests must abort with a helpful message instead of
// duplicating PRs against the remote PR branch.
func TestUpdatePullRequests_OnSprBranch_RefusesAndBails(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	githubmock.Info = &github.GitHubInfo{
		UserName:     "test",
		RepositoryID: "repo",
		LocalBranch:  "spr/master/deadbeef", // matches BranchNameRegex
	}
	stub := &stubVcsOps{commandName: "git spr"}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	// Only GetInfo is expected — once spr sees we're on a spr branch, it
	// returns nil from fetchAndGetGitHubInfo and aborts before any
	// CreatePullRequest / UpdatePullRequest call.
	githubmock.ExpectGetInfo()

	s.UpdatePullRequests(context.Background(), nil, nil)

	// The refusal message is printed via fmt.Printf (not sd.output), so
	// we can't assert on it directly from the test — but we can confirm
	// no further mock calls happened.
	githubmock.ExpectationsMet()
	_ = output
}

// --- checkStackUsable ---
//
// checkStackUsable replaces confirmIfIncompleteStack. It no longer prompts
// the user — the only remaining failure case (multi-head ambiguity) has no
// "continue anyway" answer that isn't disastrous, so we just print the
// warning as an error and return false.

func TestCheckStackUsable_NoWarning_ReturnsTrue(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	stub := &stubVcsOps{stackWarning: ""}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	ok := s.checkStackUsable()

	require.True(t, ok)
	require.Empty(t, output.String(), "no output when no warning")
}

func TestAmendCommit_StackUnusable_AbortsWithError(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	stub := &stubVcsOps{
		stackWarning: "your stack is non-linear (2 heads above trunk): A, B",
		commits: []git.Commit{
			{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test"},
		},
	}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.AmendCommit(context.Background())

	require.Contains(t, output.String(), "non-linear")
	require.NotContains(t, output.String(), "Commit to amend",
		"guard must fire before the commit-pick prompt")
}

func TestRunMergeCheck_StackUnusable_AbortsWithError(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.MergeCheck = "echo ok" // must be non-empty to reach the guard
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	stub := &stubVcsOps{
		stackWarning: "your stack is non-linear (2 heads above trunk): A, B",
		commits: []git.Commit{
			{CommitID: "00000001", CommitHash: "c100000000000000000000000000000000000000", Subject: "test"},
		},
	}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output

	s.RunMergeCheck(context.Background())

	require.Contains(t, output.String(), "non-linear")
	require.NotContains(t, output.String(), "MergeCheck PASSED")
	require.NotContains(t, output.String(), "MergeCheck FAILED")
}

func TestCheckStackUsable_Warning_PrintsErrorAndReturnsFalse(t *testing.T) {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubBranch = "master"
	gitmock := mockgit.NewMockGit(t)
	githubmock := mockclient.NewMockClient(t)
	stub := &stubVcsOps{stackWarning: "your stack is non-linear (2 heads above trunk): A, B"}
	s := NewStackedPR(cfg, githubmock, gitmock, stub)
	output := &bytes.Buffer{}
	s.output = output
	// No input set — if the function reads stdin, the test will hang or panic.
	// The new behavior must NOT read stdin.

	ok := s.checkStackUsable()

	require.False(t, ok)
	require.Contains(t, output.String(), "non-linear")
	require.NotContains(t, output.String(), "Continue anyway", "no Y/N prompt anymore")
}
