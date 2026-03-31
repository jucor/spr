package template_basic

import (
	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/template"
)

type BasicTemplatizer struct{}

func NewBasicTemplatizer() *BasicTemplatizer {
	return &BasicTemplatizer{}
}

func (t *BasicTemplatizer) Title(info *github.GitHubInfo, commit git.Commit) string {
	if info.GroupMap != nil {
		meta := template.ParsePRMetadata(commit.Body)
		if meta.Title != "" {
			return meta.Title
		}
	}
	return commit.Subject
}

func (t *BasicTemplatizer) Body(info *github.GitHubInfo, commit git.Commit, pr *github.PullRequest) string {
	var body string
	if info.GroupMap != nil {
		groupCommits := info.GroupMap[commit.CommitID]
		meta := template.ParsePRMetadata(commit.Body)
		if meta.Description != "" {
			body = meta.Description
		} else {
			body = template.StripSPRMetadata(commit.Body)
		}
		if len(groupCommits) > 1 {
			if body != "" {
				body += "\n\n"
			}
			body += "---\n**Commits**:\n\n"
			body += template.FormatGroupCommits(groupCommits)
		}
	} else {
		body = commit.Body
	}
	body += "\n\n"
	body += template.ManualMergeNotice()
	return body
}
