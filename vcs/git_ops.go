package vcs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/rs/zerolog/log"
)

// GitOps implements VCSOperations using standard git commands.
// This is a pure extraction of the existing logic from spr.go and helpers.go.
type GitOps struct {
	cfg    *config.Config
	gitcmd git.GitInterface
}

// NewGitOps creates a git-based VCSOperations implementation.
func NewGitOps(cfg *config.Config, gitcmd git.GitInterface) *GitOps {
	return &GitOps{cfg: cfg, gitcmd: gitcmd}
}

// FetchAndRebase fetches from remote and rebases the local stack.
// When MultiCommitPRs is enabled, saves and restores local branch positions
// across the rebase using commit-id trailers for matching.
func (g *GitOps) FetchAndRebase(cfg *config.Config) error {
	if cfg.Repo.ForceFetchTags {
		g.gitcmd.MustGit("fetch --tags --force", nil)
	} else {
		g.gitcmd.MustGit("fetch", nil)
	}

	// Save branch→commitID mapping before rebase (multi-commit mode only)
	var branchCommitIDs map[string]string
	if cfg.Repo.MultiCommitPRs {
		branchCommitIDs = g.getBranchCommitIDMap(cfg)
	}

	rebaseCommand := fmt.Sprintf("rebase %s/%s --autostash",
		cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)
	err := g.gitcmd.Git(rebaseCommand, nil)

	// After rebase, update branch pointers (multi-commit mode only)
	if cfg.Repo.MultiCommitPRs && len(branchCommitIDs) > 0 {
		g.updateBranchesAfterRebase(cfg, branchCommitIDs)
	}

	return err
}

// getBranchCommitIDMap returns a map of branch-name → commit-id for all
// local branches in the trunk..HEAD range. It reads each branch's commit
// message to extract the commit-id trailer.
func (g *GitOps) getBranchCommitIDMap(cfg *config.Config) map[string]string {
	commitIDRegex := regexp.MustCompile(`commit-id:\s*([a-f0-9]{8})`)

	branchMap := git.GetLocalBranchMap(g.gitcmd)
	if len(branchMap) == 0 {
		return nil
	}

	// Get the set of commit hashes in our stack range
	stackRange := fmt.Sprintf("%s/%s..HEAD", cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)
	var logOutput string
	err := g.gitcmd.Git(fmt.Sprintf("log --format=%%H %s", stackRange), &logOutput)
	if err != nil {
		return nil
	}
	stackHashes := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(logOutput), "\n") {
		if line != "" {
			stackHashes[line] = true
		}
	}

	result := make(map[string]string)
	for hash, branches := range branchMap {
		if !stackHashes[hash] {
			continue
		}
		// Read commit message to get commit-id
		var msg string
		err := g.gitcmd.Git(fmt.Sprintf("log -1 --format=%%B %s", hash), &msg)
		if err != nil {
			continue
		}
		matches := commitIDRegex.FindStringSubmatch(msg)
		if matches != nil {
			for _, branch := range branches {
				if branch != cfg.Repo.GitHubBranch && !git.IsSPRBranch(branch) {
					result[branch] = matches[1]
				}
			}
		}
	}
	return result
}

// updateBranchesAfterRebase moves local branches to their new positions
// after a rebase, matching by commit-id trailers.
func (g *GitOps) updateBranchesAfterRebase(cfg *config.Config, oldBranches map[string]string) {
	commitIDRegex := regexp.MustCompile(`commit-id:\s*([a-f0-9]{8})`)

	// Build commitID → newHash map from the rebased stack
	stackRange := fmt.Sprintf("%s/%s..HEAD", cfg.Repo.GitHubRemote, cfg.Repo.GitHubBranch)
	var logOutput string
	err := g.gitcmd.Git(fmt.Sprintf("log --format=%%H%%n%%B%s %s", "%x00", stackRange), &logOutput)
	if err != nil {
		return
	}

	commitIDToHash := make(map[string]string)
	for _, entry := range strings.Split(logOutput, "\x00") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		lines := strings.SplitN(entry, "\n", 2)
		if len(lines) < 1 {
			continue
		}
		hash := strings.TrimSpace(lines[0])
		body := ""
		if len(lines) > 1 {
			body = lines[1]
		}
		matches := commitIDRegex.FindStringSubmatch(body)
		if matches != nil {
			commitIDToHash[matches[1]] = hash
		}
	}

	// Move each branch to its new position
	for branch, commitID := range oldBranches {
		newHash, found := commitIDToHash[commitID]
		if !found {
			log.Debug().Str("branch", branch).Str("commitID", commitID).
				Msg("skipping branch update: commit-id not found after rebase (commit may have been dropped)")
			continue
		}
		err := g.gitcmd.Git(fmt.Sprintf("branch -f %s %s", branch, newHash), nil)
		if err != nil {
			log.Warn().Str("branch", branch).Err(err).Msg("failed to update branch after rebase")
		} else {
			log.Debug().Str("branch", branch).Str("newHash", newHash[:8]).
				Msg("updated branch position after rebase")
		}
	}
}

// GetLocalCommitStack returns the local commit stack using git log.
// Delegates to the existing git.GetLocalCommitStack function.
// When MultiCommitPRs is enabled, annotates commits with local branch names.
func (g *GitOps) GetLocalCommitStack(cfg *config.Config, gitcmd git.GitInterface) []git.Commit {
	commits := git.GetLocalCommitStack(cfg, gitcmd)
	if cfg.Repo.MultiCommitPRs {
		branchMap := git.GetLocalBranchMap(gitcmd)
		git.AnnotateCommitsWithBranches(commits, branchMap, cfg.Repo.GitHubBranch)
	}
	return commits
}

// AmendInto creates a fixup commit and autosquashes it into the target.
// Extracted from spr.go AmendCommit().
func (g *GitOps) AmendInto(commit git.Commit) error {
	g.gitcmd.MustGit("commit --fixup "+commit.CommitHash, nil)
	rebaseCmd := fmt.Sprintf("rebase -i --autosquash --autostash %s/%s",
		g.cfg.Repo.GitHubRemote, g.cfg.Repo.GitHubBranch)
	g.gitcmd.MustGit(rebaseCmd, nil)
	return nil
}

// EditStart begins an interactive edit session on a commit.
// Extracted from spr.go EditCommit().
func (g *GitOps) EditStart(commit git.Commit) error {
	// Write state file
	stateContent := fmt.Sprintf("commit_id=%s\ncommit_subject=%s\n", commit.CommitID, commit.Subject)
	err := os.WriteFile(g.EditStatePath(), []byte(stateContent), 0644)
	if err != nil {
		return err
	}

	// Use the spr binary as the sequence editor to rewrite 'pick' to 'edit'
	exe, err := os.Executable()
	if err != nil {
		os.Remove(g.EditStatePath())
		return err
	}
	editorCmd := fmt.Sprintf("%s _edit-sequence %s", exe, commit.CommitHash[:7])

	rebaseCmd := fmt.Sprintf("rebase -i --autostash %s/%s",
		g.cfg.Repo.GitHubRemote, g.cfg.Repo.GitHubBranch)
	err = g.gitcmd.GitWithEditor(rebaseCmd, nil, editorCmd)
	if err != nil {
		os.Remove(g.EditStatePath())
		return err
	}
	return nil
}

// EditFinish completes an edit session by amending and continuing the rebase.
// Extracted from spr.go EditCommitDone().
func (g *GitOps) EditFinish() error {
	g.gitcmd.MustGit("add -A", nil)
	err := g.gitcmd.Git("commit --amend --no-edit", nil)
	if err != nil {
		return fmt.Errorf("failed to amend commit: %w", err)
	}
	err = g.gitcmd.Git("rebase --continue", nil)
	if err != nil {
		return fmt.Errorf("rebase conflict detected: %w", err)
	}
	os.Remove(g.EditStatePath())
	return nil
}

// EditAbort cancels the current edit session.
// Extracted from spr.go EditCommitAbort().
func (g *GitOps) EditAbort() error {
	err := g.gitcmd.Git("rebase --abort", nil)
	if err != nil {
		return fmt.Errorf("failed to abort rebase: %w", err)
	}
	os.Remove(g.EditStatePath())
	return nil
}

// PrepareForPush stashes uncommitted changes and returns a cleanup function.
// Extracted from spr.go syncCommitStackToGitHub().
func (g *GitOps) PrepareForPush() (func(), error) {
	var output string
	g.gitcmd.MustGit("status --porcelain --untracked-files=no", &output)
	if output != "" {
		err := g.gitcmd.Git("stash", nil)
		if err != nil {
			return nil, err
		}
		return func() { g.gitcmd.MustGit("stash pop", nil) }, nil
	}
	return func() {}, nil
}

// IsEditing returns true if an edit session is in progress.
func (g *GitOps) IsEditing() bool {
	_, err := os.Stat(g.EditStatePath())
	return err == nil
}

// EditStatePath returns the path to the edit state file.
func (g *GitOps) EditStatePath() string {
	return filepath.Join(g.gitcmd.RootDir(), ".git", "spr_edit_state")
}

// PushBranches force-pushes spr branches for updated commits via git.
func (g *GitOps) PushBranches(cfg *config.Config, commits []git.Commit, individually bool) error {
	var refNames []string
	for _, commit := range commits {
		branchName := git.BranchNameFromCommit(cfg, commit)
		refNames = append(refNames, commit.CommitHash+":refs/heads/"+branchName)
	}
	if individually {
		for _, refName := range refNames {
			pushCommand := fmt.Sprintf("push --force %s %s", cfg.Repo.GitHubRemote, refName)
			g.gitcmd.MustGit(pushCommand, nil)
		}
	} else {
		pushCommand := fmt.Sprintf("push --force --atomic %s %s",
			cfg.Repo.GitHubRemote, strings.Join(refNames, " "))
		g.gitcmd.MustGit(pushCommand, nil)
	}
	return nil
}

// CheckStackCompleteness is a no-op for git. The detached-HEAD case is already
// caught by the branch name check in fetchAndGetGitHubInfo.
func (g *GitOps) CheckStackCompleteness() string {
	return ""
}

