package spr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

	if mergeMethod == genclient.PullRequestMergeMethod_SQUASH && sd.config.SquashPerPR() {
		sd.bottomUpMerge(ctx, count)
		return
	}

	githubInfo := sd.github.GetInfo(ctx, sd.gitcmd)
	sd.runMergeCheckGate(githubInfo)
	prIndex := topMergeableIndex(sd.config, githubInfo.PullRequests, count)
	if prIndex == -1 {
		if len(githubInfo.PullRequests) == 0 {
			// Nothing mergeable because there's nothing open: if the
			// persisted flag is set, it must be stale (left over from an
			// earlier crashed run — this call never set it itself, since
			// marking happens further below). Repair it even on this
			// fatal-exit path, so the guard's own "re-run 'git spr merge'"
			// recovery hint is actually true on the next attempt.
			if sd.config.State.MergeAllInProgress[githubInfo.Key()] {
				fmt.Fprintf(sd.output, "note: cleared stale mergeAll-in-progress flag for this stack\n")
				sd.clearMergeAllInProgress(githubInfo.Key())
			}
			check(errors.New("no mergeable pull requests found in the stack"))
			return
		}
		if sd.config.State.MergeAllInProgress[githubInfo.Key()] {
			// PRs are still open, but none is mergeable yet, and the
			// persisted flag is set for this stack — this is the primary
			// resume scenario the guard protects: a previous mergeAll
			// cascade already landed its internal merges and the final
			// wait timed out (the flag was deliberately left set), and the
			// user re-ran while the bottom PR's checks are still pending
			// or failing. Keep the flag set (clearing it here would let a
			// subsequent 'spr update' force-push and duplicate PRs) and
			// point at the actual blocker instead of a misleading
			// "nothing mergeable" fatal.
			check(fmt.Errorf(
				"a mergeAll stack merge in progress: PR #%d is not yet mergeable (checks pending or failing) — re-run 'git spr merge' once checks are green",
				githubInfo.PullRequests[0].Number))
			return
		}
		check(errors.New("no mergeable pull requests found in the stack"))
		return
	}
	// Only mark in-progress once the gate and mergeability check have
	// passed — nothing below this point returns without either landing at
	// least one PR (cleared at cascadeMerge's end) or panicking, which is
	// the actual "resume via 'git spr merge'" scenario the guard protects.
	sd.markMergeAllInProgress(githubInfo.Key())
	sd.cascadeMerge(ctx, githubInfo, prIndex, mergeMethod)
}

// bottomUpMerge lands one squash commit per PR, bottom-up. The next PR is
// retargeted to the trunk BEFORE each merge — lab-verified: waiting for
// GitHub's deletion-triggered retargeting loses a race and closes the PR.
// When a landing leaves the next PR conflicted (squash erased ancestry),
// a restack (spr update flow) rebases the remaining branches.
func (sd *stackediff) bottomUpMerge(ctx context.Context, count *uint) {
	landed := 0
	gateChecked := false
	marked := false
	for {
		info := sd.github.GetInfo(ctx, sd.gitcmd)
		if !gateChecked {
			sd.runMergeCheckGate(info)
			gateChecked = true
		}
		open := info.PullRequests
		if len(open) == 0 || (count != nil && landed >= int(*count)) {
			// Clear based on the PERSISTED flag, not the local `marked` —
			// a resumed run can find nothing left to do on its very first
			// iteration (marked still false) while the flag is stuck true
			// from an earlier crashed run. Checking the persisted state
			// repairs that case too.
			if sd.config.State.MergeAllInProgress[info.Key()] {
				sd.clearMergeAllInProgress(info.Key())
			}
			return
		}
		if !marked {
			// Only mark in-progress once we know there's at least one PR to
			// land — a no-op call (nothing open) never partially merged
			// anything, so it should never leave a dangling flag behind.
			sd.markMergeAllInProgress(info.Key())
			marked = true
		}
		bottom := open[0]
		if !bottom.MergeStatus.NoConflicts {
			// squash landed below erased ancestry; restack drops the landed
			// commit (patch-id) and force-pushes the remaining branches.
			// The reset is deferred so a panic inside UpdatePullRequests
			// (e.g. check() under SPR_DEBUG) can't leave internalUpdate
			// stuck true and silently disable the guard from then on.
			func() {
				sd.internalUpdate = true
				defer func() { sd.internalUpdate = false }()
				sd.UpdatePullRequests(ctx, nil, nil)
			}()
			continue
		}
		if !bottom.Mergeable(sd.config) {
			// waitUntilMergeable can take minutes; the stack may have
			// changed underneath us (a landing above, a new commit
			// pushed, a restack). `info`/`open` are now stale, so don't
			// keep using them below — loop back and let the next
			// iteration's GetInfo re-fetch a fresh view before retargeting
			// or merging anything.
			sd.waitUntilMergeable(ctx, bottom.Commit.CommitID)
			continue
		}
		if len(open) > 1 {
			next := open[1]
			sd.github.UpdatePullRequest(ctx, sd.gitcmd, info, open, next, next.Commit, nil)
		}
		sd.github.MergePullRequest(ctx, bottom, genclient.PullRequestMergeMethod_SQUASH,
			&github.MergeCommitMessage{Headline: bottom.Commit.Subject, Body: bottom.Commit.Body})
		if sd.config.Repo.MergeQueue {
			// Every landing here targets trunk, so under mergeQueue the call
			// above only enqueued it - MergePullRequest's config-driven
			// routing called enablePullRequestAutoMerge, not an actual
			// merge. Wait for the queue to actually land it before treating
			// it as merged (branch deletion, the MERGED print, and the next
			// iteration's retarget all assume it's really gone).
			sd.waitUntilMerged(ctx, bottom.Commit.CommitID)
		}
		if sd.config.User.DeleteMergedBranches {
			sd.deleteRemoteBranchBestEffort(ctx, bottom.FromBranch)
		}
		bottom.Merged = true
		fmt.Fprintf(sd.output, "%s\n", bottom.String(sd.config))
		landed++
	}
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
		// Internal merges always use the direct mutation, never the queue:
		// they target unprotected spr-managed branches, and
		// enablePullRequestAutoMerge errors out on a base without branch
		// protection. Queue protection applies per target branch, and only
		// trunk is queue-protected, so this is legitimate in every mode
		// (identical to MergePullRequest's non-queue behavior when there's
		// no queue at all).
		sd.github.MergePullRequestDirect(ctx, pr, mergeMethod, cascadeInternalMessage(mergeMethod, pr.Commit))
		if sd.config.User.DeleteMergedBranches {
			sd.deleteRemoteBranchBestEffort(ctx, pr.FromBranch)
		}
	}

	if sd.config.Repo.MergeQueue {
		// The queue runs its own CI on the temporary merge branch, so there's
		// no need to wait for br_0's checks to re-run here (unlike the
		// non-queue path below). This call only enqueues PR_0 - it is not
		// yet actually merged, so its branch is left alone and it is not
		// marked Merged. Every other PR in the range is already genuinely
		// merged above; a queue rejection leaves exactly one open PR
		// carrying the whole stack, recoverable by re-running 'git spr merge'.
		sd.github.MergePullRequest(ctx, prs[0], mergeMethod, cascadeFinalMessage(mergeMethod, prs[:prIndex+1]))
		for i := 1; i <= prIndex; i++ {
			prs[i].Merged = true
			fmt.Fprintf(sd.output, "%s\n", prs[i].String(sd.config))
		}
		fmt.Fprintf(sd.output, "added to merge queue: %s\n", prs[0].String(sd.config))
		sd.clearMergeAllInProgress(githubInfo.Key())
		return
	}

	finalPR := prs[0]
	if prIndex >= 1 {
		// The internal merges above just changed br_0's head SHA; under
		// branch protection its checks must re-run before this final,
		// protected landing.
		finalPR = sd.waitUntilMergeable(ctx, prs[0].Commit.CommitID)
	}
	sd.github.MergePullRequest(ctx, finalPR, mergeMethod, cascadeFinalMessage(mergeMethod, prs[:prIndex+1]))
	if sd.config.User.DeleteMergedBranches {
		sd.deleteRemoteBranchBestEffort(ctx, prs[0].FromBranch)
	}

	for i := 0; i <= prIndex; i++ {
		prs[i].Merged = true
		fmt.Fprintf(sd.output, "%s\n", prs[i].String(sd.config))
	}

	sd.clearMergeAllInProgress(githubInfo.Key())
}

// waitUntilMergeable polls GetInfo until the PR carrying commitID is
// mergeable, sleeping with backoff (5s..30s) up to mergeWaitTimeout.
// The internal cascade merges changed the branch's head SHA, so checks
// re-run once on the final branch before a protected landing.
func (sd *stackediff) waitUntilMergeable(ctx context.Context, commitID string) *github.PullRequest {
	timeout := sd.config.MergeWaitTimeoutDuration()
	interval := 5 * time.Second
	var waited time.Duration
	for {
		info := sd.github.GetInfo(ctx, sd.gitcmd)
		var pr *github.PullRequest
		for _, p := range info.PullRequests {
			if p.Commit.CommitID == commitID {
				pr = p
				break
			}
		}
		if pr == nil {
			check(errors.New("pull request disappeared while waiting for checks; run 'git spr status' and re-run 'git spr merge' to resume"))
			return nil
		}
		if pr.Mergeable(sd.config) {
			return pr
		}
		if waited >= timeout {
			check(fmt.Errorf("timed out after %s waiting for PR #%d to become mergeable; re-run 'git spr merge' to resume", timeout, pr.Number))
			return nil
		}
		fmt.Fprintf(sd.output, "waiting for PR #%d checks (%s elapsed)\n", pr.Number, waited)
		sd.sleep(interval)
		waited += interval
		if interval < 30*time.Second {
			interval += 5 * time.Second
		}
	}
}

// waitUntilMerged polls GetInfo until no open pull request carries
// commitID, sleeping with backoff (5s..30s) up to mergeWaitTimeout. Clone
// of waitUntilMergeable with the inverse success test: under mergeQueue,
// MergePullRequest only enqueues a PR (it doesn't merge it), so bottom-up
// landings must wait for the queue to actually merge the commit before
// retargeting/reporting the next PR as landed.
func (sd *stackediff) waitUntilMerged(ctx context.Context, commitID string) {
	timeout := sd.config.MergeWaitTimeoutDuration()
	interval := 5 * time.Second
	var waited time.Duration
	for {
		info := sd.github.GetInfo(ctx, sd.gitcmd)
		var pr *github.PullRequest
		for _, p := range info.PullRequests {
			if p.Commit.CommitID == commitID {
				pr = p
				break
			}
		}
		if pr == nil {
			return
		}
		if waited >= timeout {
			check(fmt.Errorf("timed out waiting for the merge queue to merge PR #%d; re-run 'git spr merge' to resume", pr.Number))
			return
		}
		fmt.Fprintf(sd.output, "waiting for the merge queue to merge PR #%d (%s elapsed)\n", pr.Number, waited)
		sd.sleep(interval)
		waited += interval
		if interval < 30*time.Second {
			interval += 5 * time.Second
		}
	}
}

// markMergeAllInProgress persists that a mergeAll stack merge has started
// for this stack, so a `spr update` run mid-cascade (crash, network blip,
// GitHub 500) refuses instead of opening duplicate PRs for commits whose
// PRs were internally merged. Cleared by clearMergeAllInProgress once the
// merge completes; if it's still set on the next run, that's the signal a
// previous mergeAll didn't finish.
func (sd *stackediff) markMergeAllInProgress(key string) {
	if sd.config.State.MergeAllInProgress == nil {
		sd.config.State.MergeAllInProgress = map[string]bool{}
	}
	sd.config.State.MergeAllInProgress[key] = true
	sd.saveState()
}

// clearMergeAllInProgress marks a mergeAll stack merge as complete.
func (sd *stackediff) clearMergeAllInProgress(key string) {
	if sd.config.State.MergeAllInProgress == nil {
		sd.config.State.MergeAllInProgress = map[string]bool{}
	}
	delete(sd.config.State.MergeAllInProgress, key)
	sd.saveState()
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
