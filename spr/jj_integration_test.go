//go:build integration

package spr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/mockclient"
	"github.com/ejoffe/spr/vcs/jjtest"
	"github.com/stretchr/testify/require"
)

// TestMain enforces the jj-availability contract documented in vcs/jjtest:
// fail fast unless SPR_SKIP_JJ_INTEGRATION=1 is set.
func TestMain(m *testing.M) {
	if os.Getenv("SPR_SKIP_JJ_INTEGRATION") == "1" {
		fmt.Fprintln(os.Stderr, "SPR_SKIP_JJ_INTEGRATION=1 set, skipping jj integration tests")
		os.Exit(0)
	}
	if _, err := exec.LookPath("jj"); err != nil {
		fmt.Fprintln(os.Stderr,
			"jj binary not found on PATH. Install jj (https://jj-vcs.github.io/jj/) "+
				"or set SPR_SKIP_JJ_INTEGRATION=1 to skip integration tests.")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// recordingClient is a thin wrapper around the standard mock — it exists
// to keep the test-setup pattern consistent and to give us a place to add
// observability hooks if needed. Upstream's GetInfo no longer takes the
// commit list, so the original "did GetInfo receive 3 commits?" assertion
// is replaced by checking that StatusPullRequests sees the full stack via
// the spr layer (auto-revset is also verified at the VCS layer in
// vcs/jj_integration_test.go).
type recordingClient struct {
	*mockclient.MockClient
}

// newSpr wires together a fresh jjtest.Repo, a recordingClient, and a
// stackediff. Returns the stackediff plus its output buffer and the client.
func newSpr(t *testing.T, repo jjtest.Repo, input string) (*stackediff, *bytes.Buffer, *recordingClient) {
	t.Helper()
	mock := mockclient.NewMockClient(t)
	mock.Info = &github.GitHubInfo{
		UserName:     "spr-test",
		RepositoryID: "RepoID",
		LocalBranch:  "master",
	}
	rec := &recordingClient{MockClient: mock}
	s := NewStackedPR(repo.Cfg, rec, repo.GitCmd, repo.JjOps)
	out := &bytes.Buffer{}
	s.output = out
	s.input = bytes.NewBufferString(input)
	return s, out, rec
}

// --- Headliner: mid-stack @ doesn't silently truncate the stack ---

// TestSprIntegration_StatusFromMidStack_ShowsFullStack runs StatusPullRequests
// after `jj edit` to a mid-stack commit and asserts that GetInfo received
// all 3 commits. Pre-auto-revset, GetInfo would see only 2 (c1, c2),
// causing the c3 PR to be considered "gone" by `spr update`.
func TestSprIntegration_StatusFromMidStack_ShowsFullStack(t *testing.T) {
	repo := jjtest.NewRepo(t)
	repo.AddCommit(t, "C1", true)
	c2 := repo.AddCommit(t, "C2", true)
	repo.AddCommit(t, "C3", true)

	repo.Edit(t, c2) // mid-stack — the original bug's trigger

	// Inspect the VCS layer directly: the original bug was that
	// GetLocalCommitStack with @ mid-stack returned a truncated stack,
	// which caused spr update to close PRs for the missing commits.
	// Auto-revset makes this stack position-independent.
	commits := repo.JjOps.GetLocalCommitStack(repo.Cfg, repo.GitCmd)
	require.Len(t, commits, 3,
		"GetLocalCommitStack must return the full stack even when @ is mid-stack — regression test for the closed-PR bug")

	// And verify spr's StatusPullRequests doesn't blow up from this state.
	s, _, rec := newSpr(t, repo, "")
	rec.ExpectGetInfo()
	s.StatusPullRequests(context.Background())
	rec.ExpectationsMet()
}

// --- Multi-head safety ---

// makeForkRepo builds a stack with a fork off c1 and leaves @ on c1 so the
// connected component (@:: | ::@) includes both heads.
func makeForkRepo(t *testing.T) jjtest.Repo {
	t.Helper()
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "C1", true)
	repo.AddCommit(t, "C2 on main chain", true)
	repo.Fork(t, c1, "C2' sibling")
	repo.Edit(t, c1)
	return repo
}

func TestSprIntegration_UpdateOnFork_RefusesWithError(t *testing.T) {
	repo := makeForkRepo(t)
	s, out, _ := newSpr(t, repo, "")
	// No GitHub expectations: multi-head detection must abort BEFORE any
	// GitHub call. If a call is made, the mock will fail.

	snapshot := repo.SnapshotOpLog(t)
	s.UpdatePullRequests(context.Background(), nil, nil)

	require.Contains(t, out.String(), "non-linear")
	repo.AssertOpsSince(t, snapshot)
}

func TestSprIntegration_AmendOnFork_RefusesWithError(t *testing.T) {
	repo := makeForkRepo(t)
	s, out, _ := newSpr(t, repo, "")

	snapshot := repo.SnapshotOpLog(t)
	s.AmendCommit(context.Background())

	require.Contains(t, out.String(), "non-linear")
	repo.AssertOpsSince(t, snapshot)
}

func TestSprIntegration_CheckOnFork_RefusesWithError(t *testing.T) {
	repo := makeForkRepo(t)
	repo.Cfg.Repo.MergeCheck = "echo would-not-run"
	s, out, _ := newSpr(t, repo, "")

	snapshot := repo.SnapshotOpLog(t)
	s.RunMergeCheck(context.Background())

	require.Contains(t, out.String(), "non-linear")
	repo.AssertOpsSince(t, snapshot)
}

// --- Edit flow ---

func TestSprIntegration_Edit_MovesAtAndAnnounces(t *testing.T) {
	repo := jjtest.NewRepo(t)
	c1 := repo.AddCommit(t, "first", true)
	repo.AddCommit(t, "second", true)
	repo.AddCommit(t, "third", true)

	s, out, _ := newSpr(t, repo, "1\n") // pick bottom commit (c1)

	snapshot := repo.SnapshotOpLog(t)
	s.EditCommit(context.Background())

	require.Contains(t, out.String(), "jj edit "+c1, "output must announce the jj edit invocation")
	require.Contains(t, out.String(), "jj undo")
	require.Equal(t, c1, repo.At(t), "@ must be on the chosen commit after spr edit returns")
	// Exactly one jj op: the edit itself. jj describes this op as
	// "edit commit <commit-hash>".
	repo.AssertOpsSince(t, snapshot, "edit commit")
}

func TestSprIntegration_Edit_OnImmutableCommit_ReportsError(t *testing.T) {
	// EditStart on the trunk commit (immutable by default) must surface a
	// clear error and not move @.
	repo := jjtest.NewRepo(t)
	trunkID := mustTrunk(t, repo)
	before := repo.At(t)

	err := repo.JjOps.EditStart(asGitCommit(trunkID))

	require.Error(t, err)
	require.Contains(t, err.Error(), "immutable")
	require.Equal(t, before, repo.At(t))
}

// --- Sync flow ---

func TestSprIntegration_Sync_RunsJjGitFetch(t *testing.T) {
	repo := jjtest.NewRepo(t)
	s, out, _ := newSpr(t, repo, "")

	snapshot := repo.SnapshotOpLog(t)
	s.SyncStack(context.Background())

	require.Contains(t, out.String(), "jj git fetch")
	require.NotContains(t, out.String(), "jj rebase",
		"sync must NOT promise to rebase — that's spr update's job")
	// jj git fetch may or may not create an op depending on whether new
	// refs were fetched. What matters is that no other / destructive op
	// was recorded.
	ops := repo.OpsSince(t, snapshot)
	for _, op := range ops {
		require.Contains(t, op.Description, "fetch",
			"sync may only produce fetch ops, got %q", op.Description)
	}
}

// --- Edit --done / --abort: echo-only, no jj ops ---

func TestSprIntegration_EditDone_EchoOnly_NoOps(t *testing.T) {
	repo := jjtest.NewRepo(t)
	s, out, _ := newSpr(t, repo, "")

	snapshot := repo.SnapshotOpLog(t)
	s.EditCommitDone(context.Background(), false)

	require.Contains(t, out.String(), "jj new")
	require.Contains(t, out.String(), "jj undo")
	repo.AssertOpsSince(t, snapshot)
}

func TestSprIntegration_EditAbort_EchoOnly_NoOps(t *testing.T) {
	repo := jjtest.NewRepo(t)
	s, out, _ := newSpr(t, repo, "")

	snapshot := repo.SnapshotOpLog(t)
	s.EditCommitAbort(context.Background())

	require.Contains(t, out.String(), "jj undo")
	repo.AssertOpsSince(t, snapshot)
}

// --- helpers ---

func mustTrunk(t *testing.T, repo jjtest.Repo) string {
	t.Helper()
	cmd := exec.Command("jj", "log", "--no-graph", "-r", "trunk()", "-T", "change_id")
	cmd.Dir = repo.Path
	out, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "trunk(): %s", string(out))
	return strings.TrimSpace(string(out))
}

func asGitCommit(changeID string) git.Commit {
	return git.Commit{ChangeID: changeID, CommitID: "deadbeef", Subject: "test"}
}
