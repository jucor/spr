// Package gittest provides a minimal git-only integration test harness:
// a bare "origin" remote plus a local clone, both backed by real `git`.
//
// Mirrors vcs/jjtest in shape, but contains no jj at all — used to observe
// real git behavior (e.g. how `git rebase` handles cascade-orphan commits
// whose content was absorbed into an upstream squash).
package gittest

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/vcs"
)

// Repo is a configured git test repository plus the spr VCS abstractions
// wired to operate on it. The remote at RemotePath is a bare repo serving
// as "origin"; Path is a normal working clone.
type Repo struct {
	Path       string
	RemotePath string
	Cfg        *config.Config
	GitCmd     git.GitInterface
	GitOps     vcs.VCSOperations
}

// NewRepo creates a bare "origin" remote and a fresh working clone, with
// one initial commit on master (so subsequent test commits have an anchor).
func NewRepo(t *testing.T) Repo {
	t.Helper()

	repoPath := t.TempDir()
	remotePath := t.TempDir()

	mustRun(t, remotePath, "git", "init", "--bare", "-b", "master")

	mustRun(t, repoPath, "git", "init", "-b", "master")
	mustRun(t, repoPath, "git", "config", "user.name", "spr-test")
	mustRun(t, repoPath, "git", "config", "user.email", "spr-test@example.com")
	mustRun(t, repoPath, "git", "config", "commit.gpgsign", "false")
	mustRun(t, repoPath, "git", "config", "tag.gpgsign", "false")
	mustRun(t, repoPath, "git", "remote", "add", "origin", remotePath)

	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	mustRun(t, repoPath, "git", "add", "README.md")
	mustRun(t, repoPath, "git", "commit", "-m", "initial")
	mustRun(t, repoPath, "git", "push", "origin", "master")

	cfg := config.EmptyConfig()
	cfg.Repo.GitHubRemote = "origin"
	cfg.Repo.GitHubBranch = "master"
	cfg.Repo.MergeMethod = "squash"
	cfg.User.BranchPrefix = "spr"

	gitCmd := &testGitCmd{rootDir: repoPath}
	gitOps := vcs.NewGitOps(cfg, gitCmd)

	return Repo{
		Path:       repoPath,
		RemotePath: remotePath,
		Cfg:        cfg,
		GitCmd:     gitCmd,
		GitOps:     gitOps,
	}
}

// AddCommit creates a new commit on top of HEAD by writing the given
// content to filename, then committing with subject as the message.
// Returns the resulting commit SHA (short).
func (r Repo) AddCommit(t *testing.T, subject, filename, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.Path, filename), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	mustRun(t, r.Path, "git", "add", filename)
	mustRun(t, r.Path, "git", "commit", "-m", subject)
	sha := strings.TrimSpace(mustRun(t, r.Path, "git", "rev-parse", "--short", "HEAD"))
	return sha
}

// PushSquashToOrigin simulates a GitHub squash-merge landing on origin/master:
// from a side worktree, builds a single commit whose tree contains the given
// files (overlaid on current origin/master), then pushes it as the new
// origin/master tip. The local repo is unaffected until the caller fetches.
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

// Git runs an arbitrary `git` command in the local repo and returns trimmed
// stdout. Mostly for inspection / assertions in tests.
func (r Repo) Git(t *testing.T, args ...string) string {
	t.Helper()
	return strings.TrimRight(mustRun(t, r.Path, "git", args...), "\n")
}

// run executes name+args in dir, returning stdout on success and a wrapped
// error including stdout+stderr on failure.
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

func mustRun(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	out, err := run(dir, name, args...)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return out
}

var tokenCounter int64

func randomToken() string {
	return fmt.Sprintf("%08x", atomic.AddInt64(&tokenCounter, 1))
}

// testGitCmd is a real git.GitInterface that shells out without the realgit
// wrapper (which os.Exit()s on error and is unsuitable for tests).
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
	return g.Git(args, output)
}

func (g *testGitCmd) RootDir() string { return g.rootDir }

func (g *testGitCmd) DeleteRemoteBranch(_ context.Context, branch string) error {
	return g.Git(fmt.Sprintf("push origin :%s", branch), nil)
}
