package spr

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ejoffe/spr/git"
	"github.com/ejoffe/spr/github"
	"github.com/ejoffe/spr/github/githubclient/gen/genclient"
)

// mergeAll lands the stack so that every pull request ends MERGED.
// Direction depends on merge method + squashGranularity; see
// spr-notes/DESIGN_CASCADE_STACK_MERGE.md for the full rationale.
func (sd *stackediff) mergeAll(ctx context.Context, count *uint) {
	mergeMethod, err := sd.config.MergeMethod()
	check(err)

	githubInfo := sd.github.GetInfo(ctx, sd.gitcmd)
	sd.runMergeCheckGate(githubInfo)
	prIndex := topMergeableIndex(sd.config, githubInfo.PullRequests, count)
	if prIndex == -1 {
		check(errors.New("no mergeable pull requests found in the stack"))
		return
	}
	sd.cascadeMerge(ctx, githubInfo, prIndex, mergeMethod)
}

// cascadeMerge merges top-down: each PR into the base branch it already
// has. No PR ever changes base, and no open PR's base branch is deleted,
// so GitHub's deletion-triggered automation never fires.
func (sd *stackediff) cascadeMerge(ctx context.Context, githubInfo *github.GitHubInfo,
	prIndex int, mergeMethod genclient.PullRequestMergeMethod) {
	prs := githubInfo.PullRequests

	if prIndex+1 < len(prs) {
		// A surviving PR sits above the merged range, based on the branch of
		// the PR directly below it, which auto-delete (or our own deletion)
		// will remove on the FIRST merge below. Retarget it explicitly NOW —
		// lab-verified: relying on GitHub's automatic retargeting loses a
		// race and closes the PR.
		survivor := prs[prIndex+1]
		sd.github.UpdatePullRequest(ctx, sd.gitcmd, githubInfo, prs, survivor, survivor.Commit, nil)
	}

	for i := prIndex; i >= 1; i-- {
		pr := prs[i]
		comment := fmt.Sprintf(
			"Merging as part of a stack cascade; the stack lands on %s via pull request #%d",
			sd.config.Repo.GitHubBranch, prs[0].Number)
		sd.github.CommentPullRequest(ctx, pr, comment)
		sd.github.MergePullRequest(ctx, pr, mergeMethod, cascadeInternalMessage(mergeMethod, pr.Commit))
		if sd.config.User.DeleteMergedBranches {
			sd.deleteRemoteBranchBestEffort(ctx, pr.FromBranch)
		}
	}

	sd.github.MergePullRequest(ctx, prs[0], mergeMethod, cascadeFinalMessage(mergeMethod, prs[:prIndex+1]))
	if sd.config.User.DeleteMergedBranches {
		sd.deleteRemoteBranchBestEffort(ctx, prs[0].FromBranch)
	}

	for i := 0; i <= prIndex; i++ {
		prs[i].Merged = true
		fmt.Fprintf(sd.output, "%s\n", prs[i].String(sd.config))
	}
}

// deleteRemoteBranchBestEffort deletes a branch whose one dependent PR is
// already merged or retargeted. Failure is fine — repos with
// "Automatically delete head branches" have already deleted it.
func (sd *stackediff) deleteRemoteBranchBestEffort(ctx context.Context, branch string) {
	if err := sd.gitcmd.DeleteRemoteBranch(ctx, branch); err != nil {
		fmt.Fprintf(sd.output, "note: could not delete remote branch %q (already deleted?): %v\n", branch, err)
	}
}

// cascadeInternalMessage keeps a cascaded PR's squash commit message
// faithful to the commit (GitHub would otherwise use the PR title once
// the branch holds more than one commit). Nil for merge/rebase.
func cascadeInternalMessage(mergeMethod genclient.PullRequestMergeMethod, c git.Commit) *github.MergeCommitMessage {
	if mergeMethod != genclient.PullRequestMergeMethod_SQUASH {
		return nil
	}
	return &github.MergeCommitMessage{Headline: c.Subject, Body: c.Body}
}

// cascadeFinalMessage names every commit in the stack in the final
// squash commit. Nil for merge/rebase.
func cascadeFinalMessage(mergeMethod genclient.PullRequestMergeMethod, prs []*github.PullRequest) *github.MergeCommitMessage {
	if mergeMethod != genclient.PullRequestMergeMethod_SQUASH {
		return nil
	}
	msg := &github.MergeCommitMessage{Headline: prs[0].Commit.Subject, Body: prs[0].Commit.Body}
	if len(prs) > 1 {
		var b strings.Builder
		b.WriteString(msg.Body)
		if msg.Body != "" {
			b.WriteString("\n\n")
		}
		b.WriteString("Stack:\n")
		for _, pr := range prs {
			fmt.Fprintf(&b, "- %s (#%d)\n", pr.Commit.Subject, pr.Number)
		}
		msg.Body = b.String()
	}
	return msg
}
