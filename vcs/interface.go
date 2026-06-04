package vcs

import (
	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
)

// VCSOperations abstracts the version control operations that differ between
// git and jj (Jujutsu). Operations like push, fetch, and branch management
// stay on git.GitInterface; only history-rewriting operations are abstracted here.
type VCSOperations interface {
	// FetchAndRebase fetches from remote and rebases local stack onto updated trunk.
	// Git: git fetch + git rebase origin/main --autostash
	// jj:  jj git fetch + jj rebase -b @ -d main@origin
	FetchAndRebase(cfg *config.Config) error

	// Fetch pulls remote refs without rebasing. Used by `spr sync` in jj mode
	// where jj's change-id alignment makes a cherry-pick unnecessary; the user
	// can use `spr update` for the rebase.
	// Git: git fetch
	// jj:  jj git fetch
	Fetch() error

	// GetLocalCommitStack returns unmerged commits (bottom-first), adding
	// commit-id trailers if missing. Returns an error if the operation
	// can't complete (e.g. jj-mode immutable commit when trying to add a
	// trailer) — git mode never errors.
	// Git: git log origin/main..HEAD, then git rebase -i with spr_reword_helper if needed
	// jj:  jj log -r 'trunk()..(@:: | ::@)' --reversed, then jj describe for missing trailers
	//      (the connected-component revset is position-independent — see vcs/jj_parse.go)
	GetLocalCommitStack() ([]git.Commit, error)

	// AmendInto squashes working copy changes into a specific commit in the stack.
	// Git: git commit --fixup <hash> + git rebase -i --autosquash --autostash
	// jj:  jj squash --into <change-id>
	AmendInto(commit git.Commit) error

	// EditStart prepares a commit for editing.
	// Git: git rebase -i with 'edit' stop (the rebase is paused for the user).
	// jj:  jj edit <change-id> — moves @ to the target commit. jj is non-blocking,
	//      so no session state is needed; the user modifies files and `jj undo`
	//      reverts. Returns when @ has moved.
	EditStart(commit git.Commit) error

	// EditFinish completes an edit session in git mode.
	// Git: git add -u + (commit --amend --no-edit if not in conflict-resolution)
	//      + git rebase --continue. Uses .git/REBASE_HEAD to distinguish initial
	//      edit stop from conflict resolution.
	// jj:  no-op. The spr layer branches on CommandName() before calling this and
	//      prints echo-only guidance pointing users at `jj new <change-id>` /
	//      `jj undo` for the navigate / revert workflow.
	EditFinish() error

	// EditAbort cancels an edit session in git mode.
	// Git: git rebase --abort
	// jj:  no-op (same reason as EditFinish — spr layer short-circuits with
	//      echo-only guidance pointing at `jj undo`).
	EditAbort() error

	// PrepareForPush saves working state before push and returns a cleanup func.
	// Git: git stash / git stash pop
	// jj:  no-op (working copy is always a commit)
	PrepareForPush() (cleanup func(), err error)

	// PushBranches force-pushes spr branches for updated commits.
	// Git: git push --force --atomic origin <hash>:refs/heads/<branch> ...
	//      (or one push per branch if `individually` is true)
	// jj:  jj bookmark set <branch> -r <hash> for each commit; then either
	//      one push per bookmark (if `individually`) or one glob push for
	//      cfg.User.BranchPrefix + "/" + cfg.Repo.GitHubBranch + "/*"
	PushBranches(cfg *config.Config, commits []git.Commit, individually bool) error

	// IsEditing returns true if an edit session is in progress.
	// Always false in jj mode (no session model).
	IsEditing() bool

	// EditStatePath returns the path to the edit state file.
	// Empty in jj mode (no state file).
	EditStatePath() string

	// CheckStackCompleteness checks whether the current stack is usable.
	// Returns a non-empty error string if not; "" if everything looks fine.
	// Git: no-op (returns "") — git's HEAD-on-branch model + rebase-in-progress
	//      lock cover the relevant safety cases natively.
	// jj:  detects multi-head ambiguity via `heads(trunk()..(@:: | ::@))`. If
	//      the connected component has more than one head, the stack is non-
	//      linear and spr refuses to operate.
	CheckStackCompleteness() string

	// CommandName returns the CLI command prefix for user-facing messages.
	// Returns "git spr" for git mode or "jj spr" for jj mode.
	CommandName() string
}

// NewVCSOperations creates a VCSOperations implementation appropriate for the
// current repository. If the repo is jj-colocated (both .jj/ and .git/ exist
// — see IsJJColocated) and the user has not set noJJ, returns a jj
// implementation. Otherwise returns a git implementation.
func NewVCSOperations(cfg *config.Config, gitcmd git.GitInterface) VCSOperations {
	if !cfg.User.NoJJ && IsJJColocated(gitcmd.RootDir()) {
		return NewJjOps(cfg, NewJjCmd(gitcmd.RootDir()), gitcmd)
	}
	return NewGitOps(cfg, gitcmd)
}
