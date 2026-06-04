package vcs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git/mockgit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeGitTestConfig() *config.Config {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubRemote = "origin"
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.MergeMethod = "rebase"
	return cfg
}

func TestGitOpsFetchAndRebase(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	gitmock.ExpectFetch() // expects git fetch + git rebase origin/master --autostash

	err := ops.FetchAndRebase(cfg)
	require.NoError(t, err)
	gitmock.ExpectationsMet()
}

func TestGitOpsFetchAndRebase_ForceTags(t *testing.T) {
	cfg := makeGitTestConfig()
	cfg.Repo.ForceFetchTags = true
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	// ExpectFetch expects "git fetch" but with force tags it's "git fetch --tags --force"
	// We need a custom expectation
	gitmock.ExpectFetchTags()

	err := ops.FetchAndRebase(cfg)
	require.NoError(t, err)
	gitmock.ExpectationsMet()
}

func TestGitOpsPrepareForPush_Clean(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	gitmock.ExpectStatus() // returns empty (clean)

	cleanup, err := ops.PrepareForPush()
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	cleanup() // should not panic, no stash pop expected
	gitmock.ExpectationsMet()
}

func TestGitOpsIsEditing_NoStateFile(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	assert.False(t, ops.IsEditing())
}

func TestGitOpsEditStatePath(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	// mockgit.RootDir() returns ""
	assert.Contains(t, ops.EditStatePath(), "spr_edit_state")
}

// --- CheckStackCompleteness ---

func TestGitOpsCheckStackCompleteness_Noop(t *testing.T) {
	cfg := makeGitTestConfig()
	gitmock := mockgit.NewMockGit(t)
	ops := NewGitOps(cfg, gitmock)

	// Git mode is a no-op — detached HEAD is caught by fetchAndGetGitHubInfo
	warning := ops.CheckStackCompleteness()
	assert.Equal(t, "", warning)
	gitmock.ExpectationsMet()
}

// --- EditFinish: REBASE_HEAD-based conflict-vs-edit-stop detection ---
//
// EditFinish branches on whether .git/REBASE_HEAD exists:
//   - missing: initial edit stop → amend then `rebase --continue`
//   - present: conflict resolution → `rebase --continue` only (NO amend)
//
// The distinction matters because amending on the conflict path would
// squash the resolved commit's changes into the previous commit (the
// bug fixed upstream in 606df435). These tests pin the behavior so a
// future refactor doesn't accidentally re-introduce that bug.

// editFinishHarness wires a GitOps to a real temp .git dir and a mockgit.
// Returns (ops, gitmock, gitDir). The test controls whether REBASE_HEAD
// exists by touching/removing it inside gitDir before invoking
// EditFinish.
func editFinishHarness(t *testing.T) (*GitOps, *mockgit.Mock, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0755))
	gitmock := mockgit.NewMockGit(t)
	gitmock.SetRootDir(root)
	ops := NewGitOps(makeGitTestConfig(), gitmock)
	return ops, gitmock, filepath.Join(root, ".git")
}

func TestGitOpsEditFinish_InitialEditStop_AmendsThenContinues(t *testing.T) {
	ops, gitmock, _ := editFinishHarness(t)

	// No REBASE_HEAD file → initial edit stop path.
	gitmock.ExpectEditDoneAmend()

	require.NoError(t, ops.EditFinish())
	gitmock.ExpectationsMet()
}

func TestGitOpsEditFinish_ConflictResolution_SkipsAmend(t *testing.T) {
	ops, gitmock, gitDir := editFinishHarness(t)

	// Create REBASE_HEAD → conflict-resolution path. EditFinish must NOT
	// call `commit --amend --no-edit` (doing so would squash the
	// resolved commit into the previous one — the bug we're guarding).
	require.NoError(t, os.WriteFile(filepath.Join(gitDir, "REBASE_HEAD"), []byte("dead\n"), 0644))
	gitmock.ExpectEditDoneConflictResolved()

	require.NoError(t, ops.EditFinish())
	gitmock.ExpectationsMet()
}

func TestGitOpsEditFinish_RebaseContinueConflict_ReturnsErrRebaseConflict(t *testing.T) {
	ops, gitmock, _ := editFinishHarness(t)

	// Initial edit stop (no REBASE_HEAD), amend succeeds, but
	// `rebase --continue` fails — must return vcs.ErrRebaseConflict
	// (wrapped) so the spr layer prints the specific recovery hint.
	gitmock.ExpectEditDoneAmendWithConflict()

	err := ops.EditFinish()
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRebaseConflict)
}
