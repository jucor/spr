# Handoff: `jj spr merge` of a stack PR cascades downstream into "closed-but-not-merged" orphans

**Author:** Julien (via Claude, 2026-06-02)
**Audience:** the future Claude Code session investigating `jj spr` semantics in
`~/github/others/spr` and `~/github/others/spr-edit-fix`.
**Scope:** describes a real-world failure mode observed on the polis stack, with a
live read-only example, and proposes how we want to investigate + document + fix it.

---

## TL;DR

When `jj spr merge` squash-merges a PR that is **not at the very bottom** of the
stack, the squash on `edge` ends up containing **the cumulative content of every
PR below it that hasn't been merged yet** (because the squash takes the PR
branch's tip, which already has all the parent stack content). GitHub then
auto-closes those intermediate PRs (since their patches are empty against the
new `edge`) with `state: closed` and `merged_at: null`.

The user's local `jj spr` stack still carries the un-squashed versions of those
intermediate commits. The next `jj spr update` errors out with conflicts when
trying to rebase the duplicate-content commits onto the new `edge`.

We need to:
1. Reproduce this end-to-end on a synthetic test repo (TDD).
2. Decide whether `jj spr` should handle it automatically, prompt the user, or
   leave it to the user with documentation.
3. Document the recovery path so users hitting this can self-rescue.
4. Land the implementation (and/or docs) as a dedicated commit in the spr fork
   (the "DG commit" — the doc-generation / dev-guide commit).

---

## Live example to use as read-only reference

**Polis repo (read-only):** `/Users/julien/polis/github/polis`

> Do NOT push, rebase, or otherwise mutate this checkout. It's an in-progress
> stack the user is actively reviewing. Treat it as a museum exhibit for
> debugging.

### The stack that triggered the bug

17 published PRs (#2508–#2524) on the `spr-stack` jj bookmark. As of
2026-06-02 ~10:40 BST, GitHub state was:

| PR | GitHub state | `merged_at` | Notes |
|---|---|---|---|
| #2508 | closed | 2026-06-01 16:29 ✅ | regular squash-merge |
| #2509 | closed | **null** | orphan: content absorbed by #2515's squash |
| #2510 | closed | **null** | orphan |
| #2511 | closed | 2026-06-01 16:32 ✅ | regular squash-merge |
| #2512 | closed | **null** | orphan |
| #2513 | closed | **null** | orphan |
| #2514 | closed | **null** | orphan |
| #2515 | closed | 2026-06-02 09:39 ✅ | **the triggering squash-merge** |
| #2516 | open  | — | `mergeable: false`, `mergeable_state: dirty` |
| #2517–#2524 | open | — | clean locally, base = previous PR's spr branch |

### Edge tip commits (read-only reference)

```
$ git -C ~/polis/github/polis log --oneline -3 origin/edge
99ed694fb Speed up regression tests (#2515)   ← triggering squash
a0f25dc43 Deep analysis of Python-Clojure discrepancies and fix plan (#2511)
1ec8506d9 CI: add concurrency group to E2E workflow; run CI on spr/ branches (#2508)
```

### Local jj stack at handoff time

```
$ jj log -r 'edge..spr-stack' --no-graph -T 'change_id.short() ++ " " ++ commit_id.short() ++ " " ++ if(conflict, "[CONFLICT] ", "") ++ description.first_line() ++ "\n"'

ymxxnkypxvkr d822f318d983 WIP Update plan and journal
nyrqpkymwosx ea9012b64b67 [CONFLICT] WIP Fix load_votes() timestamp ordering bug
nmmxropzorno 9484788881e9 [CONFLICT] WIP Fix D1: PCA sign flip prevention
lknvpxyrlonq 965f6d826d67 [CONFLICT] WIP Fix D12: comment priorities
tloqslpsmszp 50dcc9b9108b [CONFLICT] WIP Fix D3: k-smoother buffer
qnwmxxwxmmmp 52ab8bdbea4e [CONFLICT] WIP Fix D11: consensus selection
szpwyyqksyvp 046986258ff0 [CONFLICT] WIP Fix D10: rep comment selection
rklpntyowqxt 4361847925bf Fix K-means k divergence                     ← #2524
sntsvnrkullr dff35098fee6 Fix D15: moderation handling                  ← #2523
wzsqprkvpyru 6449b6de5f57 Fix D8                                        ← #2522
lvsxprttkuoo 4788e804bdfb Fix D7                                        ← #2521
nulnlpzsputy b098e7405268 Fix D6                                        ← #2520
pktpomqmwult 241791f16992 Fix D5                                        ← #2519
rzsxyxvutnnt 81051a272a6c Fix D9                                        ← #2518
zlqrulknzqqs 878bafd98d50 Fix test DB connection                        ← #2517
pqnzuzpzxsro 9cce5fdda2e6 Vectorize participant info                    ← #2516 (still open)
lzyzprmoznnz 7154cd8c4cc4 Speed up regression tests                     ← #2515 (orphan)
olvqzruoxmtm 2a5820f37607 Fix D4                                        ← #2514 (orphan)
pynmwztnvpsk 71fa1d96efef [CONFLICT] Fix D2                             ← #2513 (orphan)
koprnonoztnr 2573cb2ffe78 [CONFLICT] Per-discrepancy test infrastructure ← #2512 (orphan)
tlvwokpwzwmm 548d7caf037f Deep analysis                                 ← #2511 (merged)
vlllqszxwxwy 659c19b6a984 Speed up CI: uv pip                           ← #2510 (orphan)
wsmqmvnlzmku d95fc879a3c8 Add SKIP_GOLDEN env var                       ← #2509 (orphan)
mwrnsyqxtwzp 3d5891724afa CI: add concurrency group                     ← #2508 (merged)
```

The local stack still has commits for **every PR**, including the orphans and
the regularly-merged ones. The conflicts visible on `koprnonoztnr` and
`pynmwztnvpsk` are downstream of jj's auto-rebase trying to re-apply orphan
content on top of an `edge` that already has it.

---

## How the bug triggered, step by step

### Background

- Repo uses `jucor/spr` (fork of `ejoffe/spr`) with jj-colocated mode.
- `.spr.yml` config: `mergeMethod: squash`, `githubBranch: edge`.
- The stack is on a single jj bookmark `spr-stack`. One commit = one PR.

### Sequence

1. **Day 1.** User runs `jj spr merge --count 1` on PR #2508 (bottom of stack).
   - spr squashes #2508's branch tip into one commit; merges to `edge`.
   - At this point #2508's branch tip only had #2508's content (it was the
     bottom), so the squash commit contains exactly that. No orphans.
   - `edge` advances to `1ec8506d9`.
   - Remaining stack rebases automatically on next `jj spr update`. All clean.

2. **Day 1 (later).** User runs `jj spr merge --count 1` on PR #2511.
   - **PR #2511 is NOT at the bottom of the un-merged stack — PR #2509 and
     #2510 are still un-merged below it.**
   - spr squashes PR #2511's branch tip. That tip has the cumulative content of
     #2509 + #2510 + #2511 (since each stack PR's branch contains the previous
     PRs' content).
   - The squash commit on `edge` therefore contains #2509+#2510+#2511's full
     content rolled together.
   - `edge` advances to `a0f25dc43`.
   - **GitHub auto-closes #2509 and #2510** because their patches are now empty
     against `edge`. They get `state: closed` and `merged_at: null`.
   - User probably didn't notice — they just saw their stack getting shorter.

3. **Day 2 morning.** Same pattern repeats: user runs `jj spr merge --count 1`
   on **PR #2515** (which is several steps up the stack from the new bottom,
   PR #2512).
   - PR #2515's branch tip has the cumulative content of #2512+#2513+#2514+#2515.
   - Squash commit on `edge` (`99ed694fb`) absorbs all of it.
   - GitHub auto-closes #2512, #2513, #2514 (orphans).
   - PR #2516 becomes the new open bottom of the stack. spr re-points #2516's
     base to `edge` (was previously `spr/edge/<2515>`).

4. **Day 2.** Claude attempts unrelated work on PR #2514 (which is now an
   orphan — `state: closed, merged_at: null` — but Claude doesn't know that
   yet because the local stack still has the commit and acts like it's normal).
   - Tries to edit a comment block on the `olvqzruoxmtm` commit.
   - jj happily edits and rebases descendants. No error yet — the conflicts
     don't surface until a fetch brings in the new `edge`.

5. **Day 2 user runs `jj spr update`.**
   - spr does `jj git fetch` internally → pulls the new `edge` (`99ed694fb`).
   - jj's auto-rebase tries to reapply the local commits for #2512, #2513,
     #2514 on top of the new `edge`. Each of those commits modifies files
     that `99ed694fb` has already modified with the same content. jj's
     three-way merge produces "duplicate change" conflicts that look
     content-identical but jj treats as 2-sided / 3-sided conflicts.
   - The bottom-most conflicted commits (`koprnonoztnr` for #2512,
     `pynmwztnvpsk` for #2513) are stuck.
   - spr refuses to push: `Error: Won't push commit 71fa1d96efef since it has conflicts`.

### What the error looks like

```
$ jj spr update
> git rev-parse --show-toplevel
> github fetch pull requests
> git branch --no-color
error: jj [git push --remote origin --bookmark glob:spr/edge/*]: exit status 1
stdout:
stderr: Error: Won't push commit 71fa1d96efef since it has conflicts
Hint: Rejected commit: pynmwztn 71fa1d96 spr/edge/c0a682ec* | (conflict) Fix D2: in-conv participant threshold + D2c vote count source
```

The user is left with a stack where:
- 6 commits are conflict-marker time bombs (orphan PRs + their cascades).
- `jj spr update` can't make forward progress until they're resolved.
- The user has no clear UX cue that the underlying issue is "GitHub already has
  this content via a higher-up squash".

### Secondary symptom: PR #2516 `mergeable_state: dirty`

Even before the conflict cascade, PR #2516 itself was flagged dirty by GitHub:
- Its base was re-pointed to `edge` (after #2515's squash-merge).
- Its branch still carries the un-squashed multi-commit history of
  #2509/#2510/#2512/#2513/#2514/#2515's content.
- GitHub's mergeability check sees overlapping line edits between the
  un-squashed commits on the branch and the single squash commit on `edge`,
  reports `dirty`.
- It's not really a conflict — it's the same content twice — but GitHub doesn't
  know that.

---

## Reproduction guidance (read-only inspection on the polis repo)

Useful commands the spr dev session can run **without mutating** the polis
checkout:

```bash
# See the stack with conflict markers
jj -R ~/polis/github/polis log -r 'edge..spr-stack' --no-graph \
   -T 'change_id.short() ++ " " ++ commit_id.short() ++ " " ++ if(conflict, "[CONFLICT] ", "") ++ description.first_line() ++ "\n"' \
   --ignore-working-copy

# Check which PRs are orphans (merged_at is null but state is closed)
for pr in 2508 2509 2510 2511 2512 2513 2514 2515 2516; do
  gh api repos/compdemocracy/polis/pulls/$pr \
     --jq '{pr: .number, state, merged_at, merge_commit_sha: (.merge_commit_sha[0:9] // "—")}'
done

# Inspect a specific orphan PR's content was actually absorbed by the
# triggering squash:
git -C ~/polis/github/polis show 99ed694fb --stat | head -20
```

---

## Proposed plan of investigation (the work itself)

### 1. Synthetic E2E reproduction (TDD)

Build a single integration test in `~/github/others/spr/spr_test.go` (or a new
`squash_cascade_test.go`) that:

- Spins up a tmp repo (use existing test helpers).
- Creates a fake `edge` branch and a stack of N=4 PRs commits A → B → C → D.
- Calls the spr merge code path for commit C (mid-stack).
- Asserts:
  - `edge` now contains a squash commit with content from A+B+C.
  - The fake GitHub API correctly closes PRs for A and B as orphans
    (state=closed, merged_at=null), and merges PR C normally.
  - On a subsequent `spr.Update()` call from the same client, the local
    state for commits A and B should be… (this is the open design question
    — see "Discussion" below).

The test should fail today (or pass trivially because we currently do nothing),
which makes it a working RED test for the work.

### 2. Discussion: who handles the orphans?

Three options, not mutually exclusive:

#### Option A — Manual user recovery (no code change in spr; docs only)

- spr does nothing differently.
- Document the recovery: `jj abandon <change-id>` for each orphan, then
  `jj spr update`. Or `jj rebase -b spr-stack -d edge --skip-emptied` if
  jj's patch-id detection works.
- **Pros:** zero code complexity in spr.
- **Cons:** user must understand the failure mode, which is non-obvious
  ("GitHub closed my PR but didn't merge it???"). Recovery surface area
  is wide.

#### Option B — Proactive prompt at merge time

- When `jj spr merge` is about to squash a PR that is NOT the bottom of the
  un-merged stack, detect this and warn the user:
  `"This will cascade-close PRs #X, #Y, #Z since their content is included in
  the squash. Proceed? [y/N]"`
- After confirmation, do the merge AND automatically clean up the local
  stack (abandon the orphan commits, rebase remainder onto new `edge`).
- **Pros:** UX is great. User is informed before the mess happens.
- **Cons:** requires querying GitHub state + computing which PRs will become
  orphans before the squash. Logic non-trivial.

#### Option C — Reactive cleanup in `jj spr update`

- When `jj spr update` detects local commits whose change-ids correspond to
  GitHub PRs in state `closed, merged_at: null` (i.e. orphans), automatically
  `jj abandon` them (after confirming with the user, or with a flag).
- **Pros:** lighter-touch than B; works retroactively for users who already
  hit the issue.
- **Cons:** harder to detect cleanly (need to map jj change-ids to PR numbers
  via the `commit-id:` trailer); risk of abandoning user work if mapping is
  wrong.

The user (Julien) wants to discuss which option to take. Likely outcome:
**B + C combined**, with **A** as the docs-only fallback for older spr versions.

### 3. User-facing documentation

Wherever spr's docs live (`docs/index.md`, `readme.md`, or a new `docs/stacked-squash-merges.md`):

- Document the "cascade-closure" gotcha.
- Explain that GitHub auto-closes PRs whose content was absorbed by a
  higher-up squash.
- Provide the recovery recipe (Option A above).
- If we implement B/C, document the new behavior + how to opt out.

### 4. The "DG commit" (doc-generation / dev-guide commit)

The user explicitly said:
> we need to help that in a specific DG commit, whatever we end up doing

Interpretation: a single dedicated commit on the spr fork that ships the
documentation entry for this issue. Whether or not we end up implementing
Option B or C, the docs commit should land. Title suggestion:
`docs: stacked squash-merges cascade-close intermediate PRs`.

---

## Suggested workflow for the next session

1. **Open this handoff, read in full.**
2. **Read-only inspection** of `~/polis/github/polis` to internalize the bug.
   (Do NOT push/rebase/edit anything there.)
3. **Sketch the synthetic test repo setup.** Existing spr test helpers in
   `~/github/others/spr/spr_test.go` and `jj_integration_test.go` should give
   the scaffolding for a tmp-repo + fake-GitHub harness.
4. **Write the RED test first.** Reproduce the cascade-orphan behavior end-to-end.
   Make sure the test fails by default (or trivially asserts current behavior
   we want to change).
5. **Bring the discussion back to the user (Julien)** with concrete options A/B/C
   sized and the test in place. Decide together.
6. **Implement the chosen path** (could be docs-only, could be Option B/C).
7. **Ship the DG commit** with the documentation entry regardless of
   implementation choice.

---

## Cross-references

- **Live failing stack:** `/Users/julien/polis/github/polis` (do not mutate)
- **spr fork:** `/Users/julien/github/others/spr` (jj-spr development)
- **spr edit-fix worktree:** `/Users/julien/github/others/spr-edit-fix` (also
  spr development; user-confirm before touching either)
- **GitHub PR base/head reference:**
  - PR #2516 (currently dirty): https://github.com/compdemocracy/polis/pull/2516
  - PR #2515 (triggering squash): https://github.com/compdemocracy/polis/pull/2515
  - PR #2511 (earlier cascade-trigger): https://github.com/compdemocracy/polis/pull/2511
- **Polis CLAUDE.local.md** lives in `~/polis/claude-config-deploy/polis/` and
  has the polis-side spr usage notes.

---

## Open questions for Julien

1. Is the "user merging mid-stack" pattern (skipping over un-merged PRs) common
   in your workflow, or did it happen here by accident? If it's accidental, a
   simple warning in Option B might be the whole fix.
2. Do you want the spr fork to ship this fix or send it upstream to
   `ejoffe/spr`?
3. For Option C's "abandon orphan locally" auto-action: opt-in flag, opt-out
   flag, or interactive prompt?
4. Should the documentation live in `docs/` (rendered on the spr site) or just
   in `readme.md`?

---

*End of handoff.*
