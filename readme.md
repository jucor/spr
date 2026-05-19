![logo](docs/git_spr_logo.png)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Build](https://github.com/ejoffe/spr/actions/workflows/ci.yml/badge.svg)](https://github.com/ejoffe/spr/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/release/ejoffe/spr.svg)](https://GitHub.com/ejoffe/spr/releases/)

**Each commit becomes a pull request. Stop juggling branches.**

`git spr` manages stacked pull requests on GitHub so you don't have to. Write commits on a single branch, and spr turns each one into its own pull request -- kept in sync, correctly ordered, and ready to merge.

![terminal cast](docs/git_spr_cast.gif)

## Why stacked PRs?

- **Small PRs get reviewed faster.** A 50-line change gets meaningful feedback; a 500-line change gets "looks good."
- **No more branch gymnastics.** Stop creating `feature-part-1`, `feature-part-2`, rebasing one onto the other, and resolving conflicts between them.
- **Ship incrementally.** Land the database migration today, the API tomorrow, the UI the day after -- each reviewed and merged independently.
- **Works with native GitHub.** No extra services, no custom merge bots. Just pull requests and branches, managed for you.

## Quick Start

Install via brew, nix, or [download a binary](https://github.com/ejoffe/spr/releases):

```shell
brew install ejoffe/tap/spr        # macOS/Linux
nix profile install github:ejoffe/spr   # Nix
```

Then use it like normal git -- just replace `git push` + manual PR creation with `git spr update`:

```shell
git commit -m "Add user authentication"
git commit -m "Add login page"
git commit -m "Add session management"

git spr update   # creates 3 pull requests, stacked in order
git spr status   # show status of your stack
git spr merge    # merge everything that's ready
```

That's it. Each commit is a PR. Amend a commit and run `git spr update` again to sync changes.

## Commands

| Command | Aliases | Description |
|---------|---------|-------------|
| `git spr update`  | `u`, `up` | Create and update pull requests for commits in the stack |
| `git spr status`  | `s`, `st` | Show status of open pull requests |
| `git spr merge`   |           | Merge all mergeable pull requests |
| `git spr amend`   | `a`       | Amend a commit in the stack |
| `git spr edit`    | `e`       | Edit a commit in the stack (interactive rebase) |
| `git spr sync`    |           | Synchronize local stack with remote |
| `git spr check`   |           | Run pre-merge checks (configured by `mergeCheck`) |
| `git spr version` |           | Show version info |

**Global flags:** `--detail` (show status bit headers), `--verbose` (log git commands and GitHub API calls), `--no-jj` (disable jj mode), `--debug`, `--profile`

## Installation

### Brew
```shell
brew tap ejoffe/homebrew-tap
brew install ejoffe/tap/spr
```

### Nix
```shell
nix profile install github:ejoffe/spr
```
Or run without installing:
```shell
nix run github:ejoffe/spr
```

### Apt
```shell
echo "deb [trusted=yes] https://apt.fury.io/inigolabs/ /" | sudo tee /etc/apt/sources.list.d/inigolabs.list
sudo apt update
sudo apt install spr
```

### Binary
Download pre-compiled binaries from the [releases page](https://github.com/ejoffe/spr/releases).

### From source
```shell
make bin   # requires goreleaser; binaries output to dist/
```

## Usage Guide

### Workflow

Commit your changes to a branch as you normally do. Every commit becomes a pull request.

```shell
git add feature_1.go
git commit -m "Feature 1"
git add feature_2.go
git commit -m "Feature 2"
git add feature_3.go
git commit -m "Feature 3"

git spr update
```

The commit subject becomes the PR title; the commit body becomes the PR description. There's no need to create branches or call `git push` -- `git spr update` handles everything.

**Work in progress:** Prefix a commit message with **WIP** to skip PR creation for that commit. Remove the prefix when you're ready.

### Updating pull requests

Run `git spr update` to sync your entire stack. New commits get new PRs; amended commits update existing PRs automatically.

```shell
> git spr update
[⌛❌✅❌] 60: Feature 3
[✅✅✅✅] 59: Feature 2
[✅✅✅✅] 58: Feature 1
```

| Flag | Alias | Description |
|------|-------|-------------|
| `--count`     | `-c` | Update a specific number of PRs from the bottom of the stack |
| `--reviewer`  | `-r` | Add reviewers to newly created pull requests |
| `--no-rebase` | `--nr` | Disable rebasing (also supports `SPR_NOREBASE` env var) |

### Amending commits

Stage your changes, then use `git spr amend` to pick which commit to amend:

```shell
> git add feature_2.go
> git spr amend
 3 : 5cba235d : Feature 3
 2 : 4dc2c5b2 : Feature 2
 1 : 9d1b8193 : Feature 1
Commit to amend [1-3]: 2
```

Use `--update` (`-u`) to automatically run `git spr update` after amending.

### Editing commits

Use `git spr edit` to start an interactive rebase session on a specific commit:

```shell
> git spr edit
 3 : 5cba235d : Feature 3
 2 : 4dc2c5b2 : Feature 2
 1 : 9d1b8193 : Feature 1
Commit to edit [1-3]: 2
```

Finish with `git spr edit --done` (add `-u` to also update). Cancel with `git spr edit --abort`. In `jj` mode this works differently — see [Jujutsu (jj) Support](#jujutsu-jj-support) below.

### Syncing

Use `git spr sync` to pull remote changes into your local stack. Useful after PRs have been merged or updated on GitHub.

### Merging

Use `git spr merge` instead of the GitHub UI to merge in the correct order:

```shell
> git spr merge
MERGED #58 Feature 1
MERGED #59 Feature 2
MERGED #60 Feature 3
[✅❌✅✅] 61: Feature 4
```

spr finds the top mergeable PR in the stack, combines all commits up to it into a single PR, merges it, and closes the intermediate PRs. This avoids triggering redundant CI runs.

Use `--count N` to merge only the bottom N pull requests.

### Merge status bits

Each PR shows four status bits:

```
[✅❌✅✅] 61: Feature 4
 │  │  │  └─ stack: all PRs below are ready
 │  │  └──── conflicts: no merge conflicts
 │  └─────── approval: PR is approved
 └────────── checks: CI checks pass
```

| Bit | ⌛ | ❌ | ✅ | ➖ |
|-----|---|---|---|---|
| Checks   | pending | failed       | pass       | not required |
| Approval | --      | not approved | approved   | not required |
| Conflicts| --      | has conflicts| no conflicts| --          |
| Stack    | --      | blocked below| all clear  | --           |

Configure check and approval requirements with `requireChecks`, `requiredChecks`, and `requireApproval` in `.spr.yml`. When `requiredChecks` lists specific check names, only those checks are evaluated -- all others are ignored. This is useful when optional checks (e.g. linters, deploy previews) would otherwise cause the status to show as failed.

### Starting a new stack

Create a new branch from the latest pushed state:

```shell
git checkout -b new_stack @{push}
```

## Configuration

Configuration is created automatically on first run. Repository config lives in `.spr.yml` at the repo root; user config lives in `~/.spr.yml`.

<details>
<summary><strong>Repository configuration</strong> (.spr.yml)</summary>

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `requireChecks` | bool | `true` | Require checks to pass in order to merge |
| `requiredChecks` | list | | List of check names that must pass. When set, only these checks are evaluated; all others are ignored |
| `requireApproval` | bool | `true` | Require PR approval in order to merge |
| `githubRepoOwner` | str | | GitHub owner (auto-detected from git remote) |
| `githubRepoName` | str | | GitHub repository name (auto-detected from git remote) |
| `githubRemote` | str | `origin` | Git remote name to use |
| `githubBranch` | str | `main` | Target branch for pull requests |
| `githubHost` | str | `github.com` | GitHub host (update for GitHub Enterprise) |
| `mergeMethod` | str | `rebase` | Merge method: `rebase`, `squash`, or `merge` |
| `mergeQueue` | bool | `false` | Use GitHub merge queue |
| `prTemplateType` | str | `stack` | PR template: `stack`, `basic`, `why_what`, or `custom` |
| `prTemplatePath` | str | | Path to custom PR template file (auto-sets type to `custom`) |
| `prTemplateInsertStart` | str | | Marker in custom template for commit body insertion start |
| `prTemplateInsertEnd` | str | | Marker in custom template for commit body insertion end |
| `mergeCheck` | str | | Command to run with `git spr check` before merging |
| `forceFetchTags` | bool | `false` | Fetch tags during `git spr update` |
| `showPrTitlesInStack` | bool | `false` | Show PR titles in stack description within PR body |
| `branchPushIndividually` | bool | `false` | Push branches one at a time instead of atomically |
| `defaultReviewers` | list | | Reviewers to add to every new pull request |

Example `.spr.yml`:

```yaml
requireChecks: true
requiredChecks:
  - "ci/test"
  - "ci/build"
requireApproval: true
mergeMethod: squash
defaultReviewers:
  - teammate
```

</details>

<details>
<summary><strong>User configuration</strong> (~/.spr.yml)</summary>

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `showPRLink` | bool | `true` | Show full pull request URL |
| `shortPRLink` | bool | `false` | Show clickable `PR-<number>` instead of full URL |
| `showCommitID` | bool | `false` | Show first 8 characters of commit hash |
| `logGitCommands` | bool | `false` | Log git commands to stdout |
| `logGitHubCalls` | bool | `false` | Log GitHub API calls to stdout |
| `statusBitsHeader` | bool | `true` | Show status bit type headers |
| `statusBitsEmojis` | bool | `true` | Use emoji status bits |
| `createDraftPRs` | bool | `false` | Create new PRs as drafts |
| `preserveTitleAndBody` | bool | `false` | Don't overwrite PR title and body on update |
| `noRebase` | bool | `false` | Skip rebasing on `git spr update` |
| `deleteMergedBranches` | bool | `false` | Delete branches after PRs are merged |
| `branchPrefix` | str | `spr` | Prefix for spr-managed branch names |
| `noJJ` | bool | `false` | Disable jj (Jujutsu) mode in jj-colocated repos (also `--no-jj` flag or `SPR_NOJJ` env var) |

</details>

## Jujutsu (jj) Support

spr supports [Jujutsu](https://jj-vcs.github.io/jj/) colocated repositories. When spr detects a `.jj/` directory in your repo root, it automatically uses jj-native commands for history-rewriting operations (`jj describe`, `jj rebase`, `jj squash`, `jj edit`) instead of `git rebase`. Change IDs are preserved across all spr operations. Everything else (push, branch management, GitHub API calls) continues to use git, which works identically in colocated repos.

**Setup:**
```shell
jj git init --colocate              # initialize jj on top of your existing git repo
git spr jj-setup                    # register the `jj spr` alias
```

Or set up the alias manually:
```shell
jj config set --user aliases.spr '["util", "exec", "--", "git-spr"]'
```

### Working with `jj spr`

Most commands work identically to their `git spr` counterparts -- just substitute `jj` for `git`:
```shell
jj spr update          # create/update PRs
jj spr status          # show PR status
jj spr merge           # merge PRs
jj spr amend           # amend a commit in the stack (uses `jj squash --into`)
jj spr check           # run pre-merge checks
```

A consequence of jj's stable change IDs: spr finds your stack by *connected component*, not by working-copy position. You can leave `@` anywhere -- at the top of the stack, in the middle (e.g. after `jj edit`), or off to the side -- and `jj spr update` operates on the whole stack. No need to navigate back to the tip before running spr commands.

### Editing commits in jj

`jj spr edit` works differently from `git spr edit`. In `git` mode, editing starts an interactive rebase session that pauses on the chosen commit, requiring `--done` or `--abort` to finish. In `jj` mode, none of that scaffolding is needed: `jj edit` is instant and non-blocking, descendants are auto-rebased, and `jj undo` walks back any change.

```shell
> jj spr edit
 3 : 5cba235d : Feature 3
 2 : 4dc2c5b2 : Feature 2
 1 : 9d1b8193 : Feature 1
Commit to edit [1-3]: 2

Editing commit 2 (Feature 2): running `jj edit jjchange<id>`
To revert any changes: jj undo
```

After `jj spr edit` runs, `@` is on the chosen commit. Modify files and they're auto-snapshotted into that commit; descendants are auto-rebased. To navigate back to the top of the stack, use `jj new <change-id>`. To undo the edit entirely, use `jj undo`. `--done` and `--abort` are accepted for compatibility but print a reminder that they're git-mode only.

### Syncing in jj

`jj spr sync` runs `jj git fetch` and exits. In jj's model, change IDs naturally align local and remote commits, so the cherry-pick logic that `git spr sync` uses is unnecessary.

```shell
> jj spr sync
Running: jj git fetch
done. To also rebase onto the latest trunk, use `jj spr update`.
```

### Non-linear stacks

spr's data model is a linear stack. If your stack has multiple heads above trunk (e.g. you started a sibling chain with `jj new -r <mid> -m '...'`), spr refuses to operate and tells you to consolidate:

```shell
> jj spr update
error: your stack is non-linear (2 heads above trunk): jjchange_a, jjchange_b. spr requires a linear stack -- use `jj rebase` or `jj edit` to consolidate.
```

Resolve with `jj rebase` to merge the chains, or `jj edit` into the one you want to operate on (the other is excluded automatically by the connected-component rule).

**Opt-out:** If you have a `.jj/` directory but want spr to use git mode anyway: pass `--no-jj`, set `SPR_NOJJ=true`, or add `noJJ: true` to `~/.spr.yml`.

## How it compares

spr is similar to [Graphite](https://graphite.dev), [ghstack](https://github.com/ezyang/ghstack), and [Gerrit](https://www.gerritcodereview.com/)'s stacked review model -- but works purely with GitHub's native pull requests. No extra service, no custom merge bot, no lock-in.

## Contributing

Found a bug? [Open an issue.](https://github.com/ejoffe/spr/issues) Pull requests are welcome.

If you find spr useful, a star helps others discover it.

## License

[MIT License](LICENSE)
