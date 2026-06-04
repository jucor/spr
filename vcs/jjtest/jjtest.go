package jjtest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/vcs"
)

// Repo is a configured jj-colocated test repository plus the spr VCS
// abstractions wired to operate on it.
type Repo struct {
	// Path is the working directory of the colocated repo (also contains .jj/).
	Path string
	// RemotePath is the bare git repo used as "origin".
	RemotePath string
	// Cfg is a minimal spr config pointing at this repo's origin/master.
	Cfg *config.Config
	// JjCmd is a real JjInterface that shells out to `jj` in Path.
	JjCmd vcs.JjInterface
	// GitCmd is a real GitInterface that shells out to `git` in Path.
	GitCmd git.GitInterface
	// JjOps is a fully-wired VCSOperations using the above.
	JjOps vcs.VCSOperations
}

// NewRepo creates a fresh colocated git+jj repo in t.TempDir() and a bare
// "origin" remote in a sibling temp dir, with one initial commit on the
// master branch (so `trunk()` resolves to something meaningful in jj).
func NewRepo(t *testing.T) Repo {
	t.Helper()

	repoPath := t.TempDir()
	remotePath := t.TempDir()

	// Bare remote so `jj git fetch` / `jj git push` work without GitHub.
	mustRun(t, remotePath, "git", "init", "--bare", "-b", "master")

	// Local working repo.
	mustRun(t, repoPath, "git", "init", "-b", "master")
	mustRun(t, repoPath, "git", "config", "user.name", "spr-test")
	mustRun(t, repoPath, "git", "config", "user.email", "spr-test@example.com")
	// Disable any inherited GPG/SSH signing so tests don't trip on missing
	// keys in CI / clean dev environments.
	mustRun(t, repoPath, "git", "config", "commit.gpgsign", "false")
	mustRun(t, repoPath, "git", "config", "tag.gpgsign", "false")
	mustRun(t, repoPath, "git", "remote", "add", "origin", remotePath)

	// Initial commit on master so jj's trunk() has a definite anchor.
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun(t, repoPath, "git", "add", "README.md")
	mustRun(t, repoPath, "git", "commit", "-m", "initial")
	mustRun(t, repoPath, "git", "push", "origin", "master")

	// Colocate jj on top. After this, jj treats master@origin as the trunk.
	mustRun(t, repoPath, "jj", "git", "init", "--colocate")

	// Belt-and-braces: also set jj's own identity at repo scope so
	// describe/squash don't pick up a stale machine identity.
	mustRun(t, repoPath, "jj", "config", "set", "--repo", "user.name", "spr-test")
	mustRun(t, repoPath, "jj", "config", "set", "--repo", "user.email", "spr-test@example.com")

	// Disable color globally for this repo so command output parsing
	// (e.g. op log IDs) isn't corrupted by ANSI escape sequences.
	mustRun(t, repoPath, "jj", "config", "set", "--repo", "ui.color", "never")

	// Disable any signing inherited from user-level jj config — tests run
	// in ephemeral repos with no GPG/SSH keys configured.
	mustRun(t, repoPath, "jj", "config", "set", "--repo", "signing.behavior", "drop")

	// Use a no-op editor so commands like `jj squash --into` don't hang
	// on an interactive description-merge prompt. The value must be a TOML
	// string (quoted) — bare `true` is parsed as a boolean and rejected.
	mustRun(t, repoPath, "jj", "config", "set", "--repo", "ui.editor", `"true"`)

	cfg := config.EmptyConfig()
	cfg.Repo.GitHubRemote = "origin"
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.MergeMethod = "rebase"
	cfg.User.BranchPrefix = "spr"

	gitCmd := &testGitCmd{rootDir: repoPath}
	jjCmd := vcs.NewJjCmd(repoPath)
	jjOps := vcs.NewJjOps(cfg, jjCmd, gitCmd)

	return Repo{
		Path:       repoPath,
		RemotePath: remotePath,
		Cfg:        cfg,
		JjCmd:      jjCmd,
		GitCmd:     gitCmd,
		JjOps:      jjOps,
	}
}

// AddCommit creates a new commit on top of @ with the given subject. If
// withTrailer is true, the description includes a `commit-id:` line so spr
// considers it already-tracked (no `jj describe` retrofit needed). Returns
// the new change ID.
func (r Repo) AddCommit(t *testing.T, subject string, withTrailer bool) string {
	t.Helper()
	return r.AddCommitWithBody(t, subject, "", withTrailer)
}

// AddCommitWithBody is like AddCommit but with a description body.
//
// Lifecycle: write a unique file into the current working copy, describe it
// with the given subject/body, then advance @ to a fresh empty WC for the
// next call. Returns the change ID of the commit just described.
//
// We describe @ in place (rather than `jj new` + describe new) so that
// PushBranches doesn't trip over an ancestor empty/undescribed commit when
// pushing — jj refuses to push commits with no description. The fresh WC
// advance at the end is itself empty/undescribed but it's a *descendant* of
// the named commits, not an ancestor, so push paths don't include it.
func (r Repo) AddCommitWithBody(t *testing.T, subject, body string, withTrailer bool) string {
	t.Helper()

	filename := fmt.Sprintf("file_%s.txt", randomToken(t))
	if err := os.WriteFile(filepath.Join(r.Path, filename), []byte(subject), 0644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}

	desc := subject
	if body != "" {
		desc += "\n\n" + body
	}
	if withTrailer {
		desc += "\n\ncommit-id:" + randomToken(t)
	}
	mustRun(t, r.Path, "jj", "describe", "-m", desc)

	changeID := r.At(t)

	// Advance @ to a fresh empty WC for the next call. The empty WC is a
	// descendant of the just-described commit and gets filtered out by
	// parseJjLogOutput; it's never an ancestor of the pushed commits.
	mustRun(t, r.Path, "jj", "new")

	return changeID
}

// Edit moves @ to the given change ID (wraps `jj edit`).
func (r Repo) Edit(t *testing.T, changeID string) {
	t.Helper()
	mustRun(t, r.Path, "jj", "edit", changeID)
}

// AddCommitFile is like AddCommit but lets the caller pick a stable filename
// and content. Useful for scenarios where the test needs the same bytes to
// appear in multiple places (e.g. simulating a GitHub squash that absorbs
// the same content as a local commit). Returns the change ID.
func (r Repo) AddCommitFile(t *testing.T, subject, filename, content string, withTrailer bool) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Path, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	desc := subject
	if withTrailer {
		desc += "\n\ncommit-id:" + randomToken(t)
	}
	mustRun(t, r.Path, "jj", "describe", "-m", desc)
	changeID := r.At(t)
	mustRun(t, r.Path, "jj", "new")
	return changeID
}

// PushSquashToOrigin simulates a GitHub squash-merge landing on origin/master:
// from a side worktree, builds a single commit whose tree contains the given
// files (overlaid on current origin/master) and pushes it as the new
// origin/master tip. The local colocated repo is unaffected until the caller
// fetches. Returns the new origin/master tip SHA (short).
func (r Repo) PushSquashToOrigin(t *testing.T, subject string, files map[string]string) string {
	t.Helper()
	side := t.TempDir()
	mustRun(t, side, "git", "clone", r.RemotePath, ".")
	mustRun(t, side, "git", "config", "user.name", "spr-test")
	mustRun(t, side, "git", "config", "user.email", "spr-test@example.com")
	mustRun(t, side, "git", "config", "commit.gpgsign", "false")

	for name, content := range files {
		full := filepath.Join(side, name)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		mustRun(t, side, "git", "add", name)
	}
	mustRun(t, side, "git", "commit", "-m", subject)
	mustRun(t, side, "git", "push", "origin", "master")
	sha := strings.TrimSpace(mustRun(t, side, "git", "rev-parse", "--short", "HEAD"))
	return sha
}

// InsertAbove creates a new commit on top of @ with the given subject and
// optional trailer. Unlike AddCommit, it does NOT touch @ itself — useful
// for inserting mid-stack commits onto an existing real commit. Returns
// the new commit's change ID. After this call, @ is on the new commit.
func (r Repo) InsertAbove(t *testing.T, subject string, withTrailer bool) string {
	t.Helper()
	mustRun(t, r.Path, "jj", "new")
	filename := fmt.Sprintf("file_%s.txt", randomToken(t))
	if err := os.WriteFile(filepath.Join(r.Path, filename), []byte(subject), 0644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	desc := subject
	if withTrailer {
		desc += "\n\ncommit-id:" + randomToken(t)
	}
	mustRun(t, r.Path, "jj", "describe", "-m", desc)
	return r.At(t)
}

// Fork creates a new sibling change with the given subject on top of parent.
// Used to build multi-head scenarios.
func (r Repo) Fork(t *testing.T, parent, subject string) string {
	t.Helper()
	mustRun(t, r.Path, "jj", "new", parent)
	mustRun(t, r.Path, "jj", "describe", "-m", subject)
	return r.At(t)
}

// At returns the change ID of @ (the current working copy).
func (r Repo) At(t *testing.T) string {
	t.Helper()
	out, err := run(r.Path, "jj", "log", "--no-graph", "-r", "@", "-T", "change_id")
	if err != nil {
		t.Fatalf("read @ change id: %v", err)
	}
	return strings.TrimSpace(out)
}

// OpID identifies a jj operation by its short ID.
type OpID string

// Op describes a single jj operation as recorded in the op log.
type Op struct {
	ID          OpID
	Description string
}

// SnapshotOpLog records the current jj op ID. Pair with [OpsSince] /
// [AssertOpsSince] to verify exactly which operations were performed.
func (r Repo) SnapshotOpLog(t *testing.T) OpID {
	t.Helper()
	out, err := run(r.Path, "jj", "op", "log", "--no-graph", "-n", "1", "-T", "id.short(16)")
	if err != nil {
		t.Fatalf("snapshot op log: %v", err)
	}
	return OpID(strings.TrimSpace(out))
}

// OpsSince returns operations recorded after the snapshot, oldest-first.
// The snapshot itself is excluded. The description matches what `jj op log
// -T description` would print (the first line of the operation description).
func (r Repo) OpsSince(t *testing.T, snapshot OpID) []Op {
	t.Helper()
	// `jj op log` is newest-first by default; we want everything between
	// HEAD and the snapshot (exclusive). The revset `<snapshot>::@op-` is
	// not how jj op log works — we list a few and filter in Go.
	out, err := run(r.Path,
		"jj", "op", "log", "--no-graph", "--color=never",
		"-T", "id.short(16) ++ \"\\x1f\" ++ description.first_line() ++ \"\\n\"")
	if err != nil {
		t.Fatalf("op log: %v", err)
	}
	var ops []Op
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 2)
		if len(parts) != 2 {
			continue
		}
		id := OpID(parts[0])
		if id == snapshot {
			break // reached the snapshot; everything after this is older
		}
		// Insert at front so the final slice is oldest-first.
		ops = append([]Op{{ID: id, Description: parts[1]}}, ops...)
	}
	return ops
}

// AssertOpsSince fails the test if the ops recorded since snapshot do not
// match expected (substring-matched against each op's description, in order).
// Pass no expected to assert no ops were recorded.
func (r Repo) AssertOpsSince(t *testing.T, snapshot OpID, expected ...string) {
	t.Helper()
	ops := r.OpsSince(t, snapshot)
	if len(ops) != len(expected) {
		t.Fatalf("op log mismatch: got %d ops, want %d\ngot:  %s\nwant: %s",
			len(ops), len(expected), formatOps(ops), strings.Join(expected, "; "))
	}
	for i, want := range expected {
		if !strings.Contains(ops[i].Description, want) {
			t.Fatalf("op log[%d] mismatch: got %q, want substring %q\nfull log: %s",
				i, ops[i].Description, want, formatOps(ops))
		}
	}
}

func formatOps(ops []Op) string {
	parts := make([]string, len(ops))
	for i, op := range ops {
		parts[i] = fmt.Sprintf("[%s %s]", op.ID, op.Description)
	}
	return strings.Join(parts, " ")
}

// run executes a command in dir, returning stdout on success.
//
// stdout and stderr are captured separately so jj's informational messages
// (e.g. "Rebased N descendant commits onto updated working copy" written
// to stderr) don't corrupt parsed stdout — same fix as JjCmd.Jj. Without
// this, helpers like At() and SnapshotOpLog() that parse the returned
// string would silently produce garbage when jj has just touched the
// graph.
func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("%s %s: %w\nstdout: %s\nstderr: %s",
			name, strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), nil
}

// mustRun fails the test on any command error.
func mustRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := run(dir, name, args...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return out
}

// randomToken returns a short hex string unique to this test invocation;
// used for unique filenames and synthetic commit-id trailers.
var tokenCounter = 0

func randomToken(t *testing.T) string {
	t.Helper()
	tokenCounter++
	return fmt.Sprintf("%08x", tokenCounter)
}

// testGitCmd is a real git.GitInterface that shells out without the
// realgit go-git wrapper (which calls os.Exit on errors — unsuitable for
// tests). Same shape as JjCmd.
type testGitCmd struct {
	rootDir string
}

func (g *testGitCmd) Git(args string, output *string) error {
	cmdArgs := strings.Fields(args)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Dir = g.rootDir
	out, err := cmd.CombinedOutput()
	if output != nil {
		*output = strings.TrimRight(string(out), "\n")
	}
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", args, err, string(out))
	}
	return nil
}

func (g *testGitCmd) MustGit(args string, output *string) {
	if err := g.Git(args, output); err != nil {
		panic(err)
	}
}

func (g *testGitCmd) GitWithEditor(args string, output *string, editorCmd string) error {
	// Editor irrelevant in jj-mode integration tests (jj doesn't drive interactive git).
	return g.Git(args, output)
}

func (g *testGitCmd) RootDir() string { return g.rootDir }

func (g *testGitCmd) DeleteRemoteBranch(_ context.Context, branch string) error {
	return g.Git(fmt.Sprintf("push origin :%s", branch), nil)
}
