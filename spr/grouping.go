package spr

import (
	"github.com/ejoffe/spr/git"
	"github.com/rs/zerolog/log"
)

// PRGroup represents a group of consecutive commits that form a single PR.
// The group is delimited by a local branch (git) or bookmark (jj) at the tip commit.
type PRGroup struct {
	// LocalBranch is the local branch/bookmark name marking the tip of this PR group.
	LocalBranch string

	// Commits in this group, ordered bottom-first (oldest first).
	Commits []git.Commit
}

// TipCommit returns the top (most recent) commit in the group — the one
// the branch/bookmark points to.
func (g PRGroup) TipCommit() git.Commit {
	return g.Commits[len(g.Commits)-1]
}

// IsWIP returns true if the tip commit is marked as work in progress.
func (g PRGroup) IsWIP() bool {
	return g.TipCommit().WIP
}

// GroupCommitsIntoPRs groups a linear commit stack by branch/bookmark boundaries.
//
// Every commit that has a qualifying local branch (not spr/*, not targetBranch)
// marks the end of a PR group. Commits above the last branch are treated as WIP
// and returned separately.
//
// Returns:
//   - groups: PR groups ordered bottom-first, each with its branch name and commits
//   - wipCommits: commits above the last branch (no PR will be created for these)
func GroupCommitsIntoPRs(commits []git.Commit, targetBranch string) (groups []PRGroup, wipCommits []git.Commit) {
	var currentCommits []git.Commit

	for _, c := range commits {
		currentCommits = append(currentCommits, c)

		branch := qualifyingBranch(c, targetBranch)
		if branch != "" {
			groups = append(groups, PRGroup{
				LocalBranch: branch,
				Commits:     currentCommits,
			})
			currentCommits = nil
		}
	}

	// Any remaining commits after the last branch are WIP
	wipCommits = currentCommits
	return groups, wipCommits
}

// qualifyingBranch returns the first qualifying branch name on a commit,
// or "" if none. Qualifying means: not an spr/* branch and not the target branch.
func qualifyingBranch(c git.Commit, targetBranch string) string {
	if len(c.Branches) == 0 {
		return ""
	}
	var selected string
	count := 0
	for _, b := range c.Branches {
		if b == targetBranch || git.IsSPRBranch(b) {
			continue
		}
		count++
		if selected == "" {
			selected = b
		}
	}
	if count > 1 {
		log.Warn().Str("selected", selected).Int("total", count).
			Msg("multiple qualifying branches on commit; using first one")
	}
	return selected
}

// TipCommits extracts the tip commit from each group, suitable for use
// in place of the flat localCommits list in the existing spr flow.
func TipCommits(groups []PRGroup) []git.Commit {
	tips := make([]git.Commit, len(groups))
	for i, g := range groups {
		tips[i] = g.TipCommit()
	}
	return tips
}

// BuildGroupMap creates a map from tip commit-id to the full list of commits
// in that group. Used by templates to generate multi-commit PR bodies.
func BuildGroupMap(groups []PRGroup) map[string][]git.Commit {
	m := make(map[string][]git.Commit, len(groups))
	for _, g := range groups {
		m[g.TipCommit().CommitID] = g.Commits
	}
	return m
}
