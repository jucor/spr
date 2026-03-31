package template_stack

import (
	"github.com/ejoffe/spr/config"
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/template"
)

type StackTemplatizer struct {
	showPrTitlesInStack bool
	concatCommitMessages bool
}

func NewStackTemplatizer(showPrTitlesInStack bool, concatCommitMessages ...bool) *StackTemplatizer {
	concat := true // default
	if len(concatCommitMessages) > 0 {
		concat = concatCommitMessages[0]
	}
	return &StackTemplatizer{
		showPrTitlesInStack:  showPrTitlesInStack,
		concatCommitMessages: concat,
	}
}

func (t *StackTemplatizer) Title(info *github.GitHubInfo, commit git.Commit) string {
	// In multi-commit mode, check for spr-pr-title: marker in tip commit
	if info.GroupMap != nil {
		meta := template.ParsePRMetadata(commit.Body)
		if meta.Title != "" {
			return meta.Title
		}
	}
	return commit.Subject
}

func (t *StackTemplatizer) Body(info *github.GitHubInfo, commit git.Commit, pr *github.PullRequest) string {
	var body string

	if info.GroupMap != nil {
		// Multi-commit mode: use PR metadata and/or concatenated commit messages
		groupCommits := info.GroupMap[commit.CommitID]
		body = t.multiCommitBody(commit, groupCommits)
	} else {
		body = commit.Body
	}

	// Always show stack section and notice
	body += "\n\n"
	body += "---\n"
	body += "**Stack**:\n"
	body += template.FormatStackMarkdown(commit, info.PullRequests, t.showPrTitlesInStack)
	body += "---\n"
	body += template.ManualMergeNotice()
	return body
}

// multiCommitBody builds the PR body for a multi-commit PR.
// Priority: spr-pr-body marker > tip commit body > empty.
// Then optionally appends concatenated commit messages.
func (t *StackTemplatizer) multiCommitBody(tipCommit git.Commit, groupCommits []git.Commit) string {
	meta := template.ParsePRMetadata(tipCommit.Body)

	var body string
	if meta.Description != "" {
		body = meta.Description
	} else {
		// Fall back to tip commit body (stripped of spr metadata)
		body = template.StripSPRMetadata(tipCommit.Body)
	}

	if t.concatCommitMessages && len(groupCommits) > 1 {
		if body != "" {
			body += "\n\n"
		}
		body += "---\n"
		body += "**Commits**:\n\n"
		body += template.FormatGroupCommits(groupCommits)
	}

	return body
}

// NewStackTemplatizerFromConfig creates a StackTemplatizer from full config.
func NewStackTemplatizerFromConfig(cfg *config.RepoConfig) *StackTemplatizer {
	return &StackTemplatizer{
		showPrTitlesInStack:  cfg.ShowPrTitlesInStack,
		concatCommitMessages: cfg.ConcatCommitMessages,
	}
}
