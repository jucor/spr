package vcs

import (
	"context"
	"fmt"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/vcs/mockjj"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeJjTestConfig() *config.Config {
	cfg := config.EmptyConfig()
	cfg.Repo.GitHubRemote = "origin"
	cfg.Repo.GitHubBranch = "main"
	cfg.Repo.MergeMethod = "squash"
	cfg.User.BranchPrefix = "spr"
	return cfg
}

// --- FetchAndRebase ---

func TestJjOpsFetchAndRebase(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectFetch()
	jjmock.ExpectRebase("origin", "main")

	err := ops.FetchAndRebase(cfg, nil)
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

func TestJjOpsFetchAndRebase_NoRebase(t *testing.T) {
	cfg := makeJjTestConfig()
	cfg.User.NoRebase = true
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	// Only fetch, no rebase
	jjmock.ExpectFetch()

	err := ops.FetchAndRebase(cfg, nil)
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

// --- GetLocalCommitStack ---

func TestJjOpsGetLocalCommitStack_AllHaveIDs(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	c1 := &git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
		Subject:    "test commit 1",
	}
	c2 := &git.Commit{
		CommitID:   "00000002",
		CommitHash: "c200000000000000000000000000000000000000",
		ChangeID:   "jjchange2",
		Subject:    "test commit 2",
	}

	jjmock.ExpectLogAndRespond([]*git.Commit{c1, c2})

	commits := ops.GetLocalCommitStack(cfg, nil)
	require.Len(t, commits, 2)
	assert.Equal(t, "00000001", commits[0].CommitID)
	assert.Equal(t, "jjchange1", commits[0].ChangeID)
	assert.Equal(t, "00000002", commits[1].CommitID)
	assert.Equal(t, "jjchange2", commits[1].ChangeID)
	jjmock.ExpectationsMet()
}

// TestJjOpsGetLocalCommitStack_AutoRevset pins that the implementation queries
// the connected-component revset `trunk()..(@:: | ::@)` rather than `trunk()..@`,
// so the whole stack is returned regardless of where @ is. This is the core
// safety fix for jj mode: spr update from a mid-stack @ no longer truncates
// the stack and closes PRs for the commits above @.
func TestJjOpsGetLocalCommitStack_AutoRevset(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	// Simulate: stack of 3 commits, @ is at the middle one. The revset must
	// return all 3 regardless of @'s position.
	c1 := &git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
		Subject:    "bottom commit",
	}
	c2 := &git.Commit{
		CommitID:   "00000002",
		CommitHash: "c200000000000000000000000000000000000000",
		ChangeID:   "jjchange2",
		Subject:    "middle commit (@)",
	}
	c3 := &git.Commit{
		CommitID:   "00000003",
		CommitHash: "c300000000000000000000000000000000000000",
		ChangeID:   "jjchange3",
		Subject:    "top commit",
	}

	jjmock.ExpectLogAndRespond([]*git.Commit{c1, c2, c3})

	commits := ops.GetLocalCommitStack(cfg, nil)
	require.Len(t, commits, 3, "must return the whole stack regardless of @'s position")
	assert.Equal(t, "00000001", commits[0].CommitID)
	assert.Equal(t, "00000002", commits[1].CommitID)
	assert.Equal(t, "00000003", commits[2].CommitID)
	jjmock.ExpectationsMet()
}

func TestJjOpsGetLocalCommitStack_WIPCommit(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	c1 := &git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
		Subject:    "WIP not ready yet",
	}

	jjmock.ExpectLogAndRespond([]*git.Commit{c1})

	commits := ops.GetLocalCommitStack(cfg, nil)
	require.Len(t, commits, 1)
	assert.True(t, commits[0].WIP)
	jjmock.ExpectationsMet()
}

func TestJjOpsGetLocalCommitStack_ImmutableCommitError(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)
	ops.genCommitID = func() string { return "deadbeef" }

	// Commit without commit-id trailer — triggers jj describe attempt
	c1 := &git.Commit{
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchangeimmutable",
		Subject:    "feat: some old commit",
		// No CommitID
	}

	jjmock.ExpectLogAndRespond([]*git.Commit{c1})
	jjmock.ExpectDescribeAndFail("jjchangeimmutable",
		"feat: some old commit\n\ncommit-id:deadbeef",
		fmt.Errorf("Error: Commit abc123 is immutable"))

	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic for immutable commit")
		msg := fmt.Sprintf("%v", r)
		assert.Contains(t, msg, "jjchangeimmutable")
		assert.Contains(t, msg, "immutable")
		assert.Contains(t, msg, "jj bookmark track")
	}()
	ops.GetLocalCommitStack(cfg, nil)
}

// --- Fetch ---

func TestJjOpsFetch(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectFetch()

	err := ops.Fetch()
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

// --- FetchAndRebase error paths ---

func TestJjOpsFetchAndRebase_FetchFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectFetchAndFail(fmt.Errorf("network is down"))
	// Note: no ExpectRebase — rebase must not run if fetch failed.

	err := ops.FetchAndRebase(cfg, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network is down")
	jjmock.ExpectationsMet()
}

func TestJjOpsFetchAndRebase_RebaseFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectFetch()
	jjmock.ExpectRebaseAndFail("origin", "main", fmt.Errorf("rebase conflict"))

	err := ops.FetchAndRebase(cfg, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rebase conflict")
	jjmock.ExpectationsMet()
}

// --- AbandonChangeIDs + FetchAndRebase orphan-prune path ---

func TestJjOpsAbandonChangeIDs_Empty(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	// Empty / nil input must issue zero jj commands.
	require.NoError(t, ops.AbandonChangeIDs(nil))
	require.NoError(t, ops.AbandonChangeIDs([]string{}))
	require.NoError(t, ops.AbandonChangeIDs([]string{""}))
	jjmock.ExpectationsMet()
}

func TestJjOpsAbandonChangeIDs_MultipleInOrder(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectAbandon("abc")
	jjmock.ExpectAbandon("def")
	jjmock.ExpectAbandon("ghi")

	require.NoError(t, ops.AbandonChangeIDs([]string{"abc", "def", "ghi"}))
	jjmock.ExpectationsMet()
}

func TestJjOpsAbandonChangeIDs_ShortCircuitOnError(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectAbandon("abc")
	jjmock.ExpectAbandonAndFail("def", fmt.Errorf("immutable"))
	// "ghi" must not be attempted after the failure.

	err := ops.AbandonChangeIDs([]string{"abc", "def", "ghi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "jj abandon def")
	assert.Contains(t, err.Error(), "immutable")
	jjmock.ExpectationsMet()
}

func TestJjOpsFetchAndRebase_WithOrphans_AbandonsBeforeRebase(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	// Expected sequence: fetch first, then abandon each orphan in order,
	// then rebase. Verifies that abandon happens BEFORE rebase so the
	// rebase never sees the orphan patches.
	jjmock.ExpectFetch()
	jjmock.ExpectAbandon("orphan1")
	jjmock.ExpectAbandon("orphan2")
	jjmock.ExpectRebase("origin", "main")

	err := ops.FetchAndRebase(cfg, []string{"orphan1", "orphan2"})
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

func TestJjOpsFetchAndRebase_WithOrphans_AbandonFailsSkipsRebase(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	// Fetch succeeds, abandon fails — rebase must NOT run, since rebasing
	// with the orphan still present would reproduce the bug we're trying
	// to prevent.
	jjmock.ExpectFetch()
	jjmock.ExpectAbandonAndFail("orphan1", fmt.Errorf("immutable"))

	err := ops.FetchAndRebase(cfg, []string{"orphan1", "orphan2"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable")
	jjmock.ExpectationsMet()
}

func TestJjOpsFetchAndRebase_NoRebase_SkipsAbandon(t *testing.T) {
	cfg := makeJjTestConfig()
	cfg.User.NoRebase = true
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	// NoRebase => fetch only. No abandon either: without rebase there's
	// no failure to prevent, and abandoning blindly could surprise users.
	jjmock.ExpectFetch()

	err := ops.FetchAndRebase(cfg, []string{"would-be-orphan"})
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

// --- GetLocalCommitStack: log call failures (defensive panics) ---
//
// jj log is expected to always succeed in well-formed repos; if it
// fails, JjOps panics with the underlying error. These tests pin the
// panic behavior so a future change doesn't silently swallow the error.

func TestJjOpsGetLocalCommitStack_FirstLogFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectLogAndFail(fmt.Errorf("revset parse error"))

	defer func() {
		r := recover()
		require.NotNil(t, r, "must panic on first jj log failure")
		assert.Contains(t, fmt.Sprintf("%v", r), "revset parse error")
	}()
	ops.GetLocalCommitStack(cfg, nil)
}

func TestJjOpsGetLocalCommitStack_SecondLogFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)
	ops.genCommitID = func() string { return "deadbeef" }

	// First log returns a commit without trailer → triggers describe path.
	c1 := &git.Commit{
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
		Subject:    "needs trailer",
	}
	jjmock.ExpectLogAndRespond([]*git.Commit{c1})
	jjmock.ExpectDescribe("jjchange1", "needs trailer\n\ncommit-id:deadbeef")
	// Second log (re-read after describe) fails.
	jjmock.ExpectLogAndFail(fmt.Errorf("repo went sideways"))

	defer func() {
		r := recover()
		require.NotNil(t, r, "must panic on second jj log failure")
		assert.Contains(t, fmt.Sprintf("%v", r), "repo went sideways")
	}()
	ops.GetLocalCommitStack(cfg, nil)
}

// --- GetLocalCommitStack: post-describe re-parse failure ---
//
// If parsing the log fails initially, JjOps retrofits commit-id trailers via
// `jj describe`, then re-parses. If the re-parse STILL produces an invalid
// result, the documented behavior is to panic — we'd never expect this in
// practice but pin it so an accidental silent-return regression is caught.

func TestJjOpsGetLocalCommitStack_PostDescribeReparseFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)
	ops.genCommitID = func() string { return "deadbeef" }

	// Commit without trailer → initial parse invalid.
	c1 := &git.Commit{
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
		Subject:    "needs trailer",
	}
	jjmock.ExpectLogAndRespond([]*git.Commit{c1})
	jjmock.ExpectDescribe("jjchange1", "needs trailer\n\ncommit-id:deadbeef")
	// After describe, re-parse — but we still return a commit with no
	// trailer to simulate jj describe somehow not taking effect.
	jjmock.ExpectLogAndRespond([]*git.Commit{c1})

	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic on re-parse failure")
		assert.Contains(t, fmt.Sprintf("%v", r), "unable to add commit-id trailers")
	}()
	ops.GetLocalCommitStack(cfg, nil)
}

// --- PushBranches error paths ---

func TestJjOpsPushBranches_BookmarkSetFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	commit := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
	}

	jjmock.ExpectBookmarkSetAndFail("spr/main/00000001",
		commit.CommitHash, fmt.Errorf("bookmark exists with conflict"))
	// Note: no push expected — must abort on bookmark-set failure.

	err := ops.PushBranches(cfg, []git.Commit{commit}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to set bookmark")
	jjmock.ExpectationsMet()
}

func TestJjOpsPushBranches_GitPushFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	commit := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
	}

	jjmock.ExpectBookmarkSet("spr/main/00000001", commit.CommitHash)
	jjmock.ExpectGitPushAndFail("origin", "spr/main/*",
		fmt.Errorf("Won't push commit ... since it has conflicts"))

	err := ops.PushBranches(cfg, []git.Commit{commit}, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts")
	jjmock.ExpectationsMet()
}

// --- CheckStackCompleteness: heads query failure ---
//
// Today, if the heads query errors, CheckStackCompleteness returns "" (no
// warning) — a deliberate soft-fail so a transient jj error doesn't block
// every spr command. Pin this so a future change doesn't quietly start
// returning an error string that breaks unrelated callers.

func TestJjOpsCheckStackCompleteness_HeadsQueryFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectCheckHeadsAndFail(fmt.Errorf("jj internal error"))

	warning := ops.CheckStackCompleteness()
	assert.Equal(t, "", warning, "soft-fail: errors return empty warning")
	jjmock.ExpectationsMet()
}

// --- AmendInto ---

func TestJjOpsAmendInto(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	commit := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		ChangeID:   "jjchange1",
	}

	jjmock.ExpectSquash("jjchange1")

	err := ops.AmendInto(commit)
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

func TestJjOpsAmendInto_NoChangeID(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	commit := git.Commit{
		CommitID:   "00000001",
		CommitHash: "c100000000000000000000000000000000000000",
		// No ChangeID
	}

	err := ops.AmendInto(commit)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no jj change ID")
	jjmock.ExpectationsMet()
}

// --- EditStart: runs jj edit; EditFinish / EditAbort: no-ops ---
//
// jj is non-blocking, so EditFinish and EditAbort have no work to do —
// returning to the stack tip or reverting changes is the user's job via
// native jj (`jj new`, `jj undo`). EditStart, however, actually runs
// `jj edit <change-id>` to move @ to the target commit, so the user can
// just start modifying files when spr exits.

func TestJjOpsIsEditing_AlwaysFalse(t *testing.T) {
	cfg := makeJjTestConfig()
	ops := NewJjOps(cfg, nil, nil)
	assert.False(t, ops.IsEditing(), "jj mode has no edit session concept")
}

func TestJjOpsEditStatePath_Empty(t *testing.T) {
	cfg := makeJjTestConfig()
	ops := NewJjOps(cfg, nil, nil)
	assert.Equal(t, "", ops.EditStatePath(), "jj mode has no state file")
}

func TestJjOpsEditStart_RunsJjEdit(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectEdit("jjchange1")

	err := ops.EditStart(git.Commit{ChangeID: "jjchange1", CommitID: "00000001"})
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

func TestJjOpsEditStart_NoChangeID(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t) // no expectations — no jj edit must run
	ops := NewJjOps(cfg, jjmock, nil)

	err := ops.EditStart(git.Commit{CommitID: "00000001"}) // ChangeID empty
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no jj change ID")
	jjmock.ExpectationsMet()
}

func TestJjOpsEditStart_JjEditFails(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectEditAndFail("jjchange1", fmt.Errorf("commit is immutable"))

	err := ops.EditStart(git.Commit{ChangeID: "jjchange1", CommitID: "00000001"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "immutable")
	jjmock.ExpectationsMet()
}

func TestJjOpsEditFinish_NoOp(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)
	err := ops.EditFinish()
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

func TestJjOpsEditAbort_NoOp(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)
	err := ops.EditAbort()
	require.NoError(t, err)
	jjmock.ExpectationsMet()
}

// --- PrepareForPush ---

func TestJjOpsPrepareForPush_IsNoop(t *testing.T) {
	cfg := makeJjTestConfig()
	ops := NewJjOps(cfg, nil, nil)

	cleanup, err := ops.PrepareForPush()
	require.NoError(t, err)
	require.NotNil(t, cleanup)
	cleanup() // should not panic
}

// --- CheckStackCompleteness ---
//
// In jj mode this detects multi-head ambiguity (the stack is non-linear).
// Mid-stack @ is no longer a problem because GetLocalCommitStack uses the
// connected-component revset and returns the whole stack regardless.

func TestJjOpsCheckStackCompleteness_SingleHead(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectCheckHeads("jjchange_tip")

	warning := ops.CheckStackCompleteness()
	assert.Equal(t, "", warning)
	jjmock.ExpectationsMet()
}

func TestJjOpsCheckStackCompleteness_NoStack(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectCheckHeads("")

	warning := ops.CheckStackCompleteness()
	assert.Equal(t, "", warning)
	jjmock.ExpectationsMet()
}

func TestJjOpsCheckStackCompleteness_MultipleHeads(t *testing.T) {
	cfg := makeJjTestConfig()
	jjmock := mockjj.NewMockJj(t)
	ops := NewJjOps(cfg, jjmock, nil)

	jjmock.ExpectCheckHeads("jjchange_head1\njjchange_head2")

	warning := ops.CheckStackCompleteness()
	assert.Contains(t, warning, "non-linear")
	assert.Contains(t, warning, "jjchange_head1")
	assert.Contains(t, warning, "jjchange_head2")
	jjmock.ExpectationsMet()
}

// --- mockRootDir implements git.GitInterface just for RootDir ---

type mockRootDir struct {
	rootDir string
}

func (m *mockRootDir) GitWithEditor(args string, output *string, editorCmd string) error { return nil }
func (m *mockRootDir) Git(args string, output *string) error                             { return nil }
func (m *mockRootDir) MustGit(args string, output *string)                               {}
func (m *mockRootDir) RootDir() string                                                   { return m.rootDir }
func (m *mockRootDir) DeleteRemoteBranch(ctx context.Context, branch string) error       { return nil }
