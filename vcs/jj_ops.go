package vcs

import (
	"fmt"
	"strings"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/google/uuid"
)

// JjOps implements VCSOperations using jj (Jujutsu) commands.
// Git commands are still used for push operations (via gitcmd).
type JjOps struct {
	cfg         *config.Config
	jjcmd       JjInterface
	gitcmd      git.GitInterface
	genCommitID func() string
}

// NewJjOps creates a jj-based VCSOperations implementation.
func NewJjOps(cfg *config.Config, jjcmd JjInterface, gitcmd git.GitInterface) *JjOps {
	return &JjOps{
		cfg:         cfg,
		jjcmd:       jjcmd,
		gitcmd:      gitcmd,
		genCommitID: func() string { return uuid.New().String()[:8] },
	}
}

// Fetch runs `jj git fetch` without rebasing.
func (j *JjOps) Fetch() error {
	return j.jjcmd.Jj("git fetch", nil)
}

// FetchAndRebase fetches from remote and rebases using jj commands.
// Preserves jj change IDs (unlike git rebase which destroys them).
func (j *JjOps) FetchAndRebase(cfg *config.Config) error {
	if cfg.User.NoRebase {
		// Only fetch, skip rebase (same semantics as git NoRebase)
		return j.jjcmd.Jj("git fetch", nil)
	}

	err := j.jjcmd.Jj("git fetch", nil)
	if err != nil {
		return err
	}

	// Rebase current stack onto updated trunk.
	//
	// --skip-emptied drops local commits whose patches were absorbed into
	// the new trunk (e.g. siblings of a mid-stack squash-merge whose
	// content is now in trunk via the squash). Without this, jj rebase
	// would leave them around as `[EMPTY]` stubs — the clean form of the
	// cascade-orphan bug. This matches the patch-id-based behavior of
	// `git rebase` in git-mode FetchAndRebase. It does NOT help when an
	// orphan's local diff has structurally drifted from the squashed
	// version (overlapping but-different edits) — that case still
	// produces conflicts and needs orphan-PR detection + `jj abandon`
	// before fetch+rebase. See vcs/jj_cascade_integration_test.go for
	// the full behavior matrix.
	remote := cfg.Repo.GitHubRemote
	branch := cfg.Repo.GitHubBranch
	rebaseCmd := fmt.Sprintf("rebase -b @ -d %s@%s --skip-emptied", branch, remote)
	return j.jjcmd.Jj(rebaseCmd, nil)
}

// GetLocalCommitStack returns unmerged commits using jj log.
// If any commits lack commit-id trailers, adds them via jj describe.
func (j *JjOps) GetLocalCommitStack(cfg *config.Config, gitcmd git.GitInterface) []git.Commit {
	template := `commit_id ++ "\x1f" ++ change_id ++ "\x1f" ++ empty ++ "\x1f" ++ description ++ "\x1e"`

	var output string
	err := j.jjcmd.JjArgs([]string{"log", "--no-graph", "--reversed", "--color=never", "-r", "trunk()..(@:: | ::@)", "-T", template}, &output)
	if err != nil {
		panic(err)
	}

	parsed, valid := parseJjLogOutput(output)

	if !valid {
		// Add commit-id trailers to commits that lack them
		for i, p := range parsed {
			if p.sprCommitID == "" && !p.empty {
				newID := j.genCommitID()
				newDesc := strings.TrimRight(p.description, "\n")
				newDesc += "\n\ncommit-id:" + newID
				err := j.jjcmd.JjArgs([]string{"describe", "-r", p.changeID, "-m", newDesc}, nil)
				if err != nil {
					panic(fmt.Sprintf(
						"error: cannot add commit-id trailer to %s (%s) — commit is likely immutable\n\n"+
							"This happens when an untracked remote bookmark points at this commit\n"+
							"(or one of its descendants), making the entire chain immutable.\n\n"+
							"To fix, track the remote bookmarks that cover this commit:\n"+
							"  jj bookmark track <bookmark> --remote <remote>\n\n"+
							"To find which bookmarks are involved:\n"+
							"  jj log -r '%s::' -T 'change_id.short() ++ \" \" ++ remote_bookmarks ++ \"\\n\"'\n\n"+
							"Underlying error: %v",
						p.changeID, p.subject, p.changeID, err))
				}
				parsed[i].sprCommitID = newID
			}
		}

		// Re-read commit hashes since jj describe changes them
		err = j.jjcmd.JjArgs([]string{"log", "--no-graph", "--reversed", "--color=never", "-r", "trunk()..(@:: | ::@)", "-T", template}, &output)
		if err != nil {
			panic(err)
		}
		reparsed, revalid := parseJjLogOutput(output)
		if !revalid {
			panic("unable to add commit-id trailers via jj describe")
		}
		parsed = reparsed
	}

	// Convert to []git.Commit
	var commits []git.Commit
	for _, p := range parsed {
		if p.wip {
			// Include WIP commits but mark them (spr stops at first WIP)
		}
		commits = append(commits, git.Commit{
			CommitID:   p.sprCommitID,
			CommitHash: p.commitHash,
			ChangeID:   p.changeID,
			Subject:    p.subject,
			Body:       p.body,
			WIP:        p.wip,
		})
	}
	return commits
}

// AmendInto squashes working copy changes into a specific commit.
// Uses jj squash which preserves change IDs.
func (j *JjOps) AmendInto(commit git.Commit) error {
	if commit.ChangeID == "" {
		return fmt.Errorf("cannot amend: commit %s has no jj change ID", commit.CommitID)
	}
	return j.jjcmd.Jj(fmt.Sprintf("squash --into %s", commit.ChangeID), nil)
}

// EditStart runs `jj edit <change-id>` to move @ to the target commit so the
// user can start modifying files immediately. No state file, no op-id snapshot,
// no rebase session — jj is non-blocking and the user navigates back via native
// jj (jj new <change-id>) or reverts via jj undo.
func (j *JjOps) EditStart(commit git.Commit) error {
	if commit.ChangeID == "" {
		return fmt.Errorf("cannot edit: commit %s has no jj change ID", commit.CommitID)
	}
	return j.jjcmd.Jj("edit "+commit.ChangeID, nil)
}

// EditFinish and EditAbort are no-ops in jj mode. The spr layer branches on
// CommandName() before reaching the VCS interface and prints native-jj
// instructions instead of calling these. They exist to satisfy VCSOperations.
func (j *JjOps) EditFinish() error { return nil }
func (j *JjOps) EditAbort() error  { return nil }

// PushBranches sets jj bookmarks for each updated commit and pushes via jj git push.
// This ensures jj tracks the branches, keeping the commits mutable.
//
// Honors cfg.User.BranchPushIndividually (mirrors GitOps): when true, pushes
// one bookmark per `jj git push` call instead of one glob push. The
// individual mode is intended for cases where atomic multi-branch push
// times out on slow remotes.
func (j *JjOps) PushBranches(cfg *config.Config, commits []git.Commit, individually bool) error {
	branchNames := make([]string, 0, len(commits))
	for _, commit := range commits {
		branchName := git.BranchNameFromCommit(cfg, commit)
		err := j.jjcmd.JjArgs([]string{"bookmark", "set", branchName, "-r", commit.CommitHash, "--allow-backwards"}, nil)
		if err != nil {
			return fmt.Errorf("failed to set bookmark %s: %w", branchName, err)
		}
		branchNames = append(branchNames, branchName)
	}
	remote := cfg.Repo.GitHubRemote
	if individually {
		for _, branchName := range branchNames {
			if err := j.jjcmd.JjArgs([]string{"git", "push", "--remote", remote, "--bookmark", branchName}, nil); err != nil {
				return fmt.Errorf("failed to push bookmark %s: %w", branchName, err)
			}
		}
		return nil
	}
	branchGlob := "glob:" + cfg.User.BranchPrefix + "/" + cfg.Repo.GitHubBranch + "/*"
	return j.jjcmd.JjArgs([]string{"git", "push", "--remote", remote, "--bookmark", branchGlob}, nil)
}

// PrepareForPush is a no-op for jj — the working copy is always a commit.
func (j *JjOps) PrepareForPush() (func(), error) {
	return func() {}, nil
}

// IsEditing always returns false in jj mode — there's no session concept.
func (j *JjOps) IsEditing() bool { return false }

// EditStatePath returns an empty string in jj mode — no state file is written.
func (j *JjOps) EditStatePath() string { return "" }

// CheckStackCompleteness detects multi-head ambiguity in the user's stack.
// spr requires a linear stack; if the connected component of @ contains more
// than one head, spr can't decide which is "the" tip and we refuse to operate.
// Mid-stack @ is not a problem here — GetLocalCommitStack uses the
// connected-component revset and returns the whole stack regardless.
func (j *JjOps) CheckStackCompleteness() string {
	var output string
	err := j.jjcmd.JjArgs([]string{"log", "--no-graph", "--color=never", "-r", "heads(trunk()..(@:: | ::@))", "-T", `change_id ++ "\n"`}, &output)
	if err != nil {
		return ""
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	heads := strings.Split(output, "\n")
	if len(heads) <= 1 {
		return ""
	}
	return fmt.Sprintf("your stack is non-linear (%d heads above trunk): %s. spr requires a linear stack — use `jj rebase` or `jj edit` to consolidate.", len(heads), strings.Join(heads, ", "))
}

func (j *JjOps) CommandName() string {
	return "jj spr"
}
