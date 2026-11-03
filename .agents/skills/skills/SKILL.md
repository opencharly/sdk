---
name: skills
description: |-
  Skill maintenance: when and how to update skills, root rulebooks, and README.md. Use when updating documentation, feeding back operational insights, or auditing skill coverage.
---

# Skill Maintenance Guidelines

## Overview

Skills are living documents at `marketplace/<family>/skills/<name>/SKILL.md`. They
are the primary procedural knowledge source for every supported agent harness
and are always loaded before codebase exploration. This skill covers when and
how to update them.

## Skill Dispatchers

The umbrella `AGENTS.md` is the single, harness-neutral rulebook: every
harness reads it directly, and the trigger → skill dispatcher lives in its
Part I ("R0. Skills first"). A subrepo's `AGENTS.md` carries only that repo's
extra rows (e.g. `charly/AGENTS.md`'s "R0 for the core"), never a copy.
When multiple triggers apply, load all matching skills before acting.
Full index: `marketplace/README.md`.

**How the umbrella table is maintained.** It is a HAND-CURATED, compact subset
of the generated dispatcher (`marketplace/DISPATCHER.md`, emitted by
`charly marketplace generate` from each skill entity's `triggers:` — one row per
trigger; that file is the authority for the full set). Compact means ONE row per
trigger with every skill it selects in one cell, never one row per skill. Being
prose, the table lives OUTSIDE the `BEGIN/END GENERATED SKILL DISPATCHER`
markers, so `charly task skills` (`scripts/sync-dispatcher.sh`) is a no-op until a
consumer adds the markers; then it splices the full generated fragment in place.
To add, rename, or remove a skill, update the row in the same PR and keep every
ref resolving to `marketplace/<family>/skills/<skill>/SKILL.md`.

## Adding a `triggers:` is a CONFIG change — and its proof is the emitted row

A skill entity's `triggers:` is not prose: `emit_dispatcher.go` consumes it to emit
exactly one `DISPATCHER.md` row per triggers-bearing skill (`if s.Type == "agent" ||
len(s.Triggers) == 0 { continue }`). A change that only adds `triggers:` to a skill
entity therefore alters a GENERATED artifact — it is a **config** change, and the
`documentation reviewed` attribution tier does not apply to it.

The gate is the generated artifact, not a structure check. `charly box validate`
proves the YAML parses; it does NOT prove the routing the change exists to add, and
A1/R10 forbid deferring the effect to the nightly refresh bot. Run the corpus's own
generator and paste the emitted rows:

```bash
charly marketplace generate --root <root> --out <out>
grep -n '<family>:<skill>' <out>/DISPATCHER.md
```

When the owning repo's marketplace pin is OLDER than the branch (the common case for
an unmerged skill change), the live corpus cannot see the change at all — and
`CHARLY_REPO_OVERRIDE` cannot substitute a directly-pinned repo (the separate defect
opencharly/opencharly#387). Generate from a MINIMAL root carrying the exact changed
skill entities plus the real family registry, and paste the rows.

## The corpus must actually LOAD — audit delivery, not just content

A skill that never reaches the agent is indistinguishable from one that was
never written. Enablement and installation are SEPARATE halves of the harness
contract, and satisfying only the first fails SILENTLY:

- **Claude Code** — `enabledPlugins` in `.claude/settings.json` flips
  enablement only. Skills load when the harness ALSO holds an install record
  for that plugin scoped to the CURRENT project. A `settings.json` listing a
  plugin whose install record belongs to a different project, or whose
  recorded `installPath` no longer exists, loads NONE of its skills — the
  marketplace, manifest, and `SKILL.md` frontmatter can all be valid while
  the corpus is entirely absent.
- `claude plugin list` reads enablement from `settings.json` and prints
  `Status: enabled` regardless of whether the install record resolves, so it
  CONFIRMS a corpus that is not loading. It is not evidence; never close an
  audit on it.

**The only reliable probe is a fresh session's own answer:**

```bash
claude -p "List the skills available to you whose name contains ':'"
```

**The fix writes both halves** — never hand-edit the settings JSON, which
would reproduce the same half-configured state:

```bash
claude plugin install <plugin>@<marketplace> --scope project
```

Run the probe after any change to a marketplace, to a plugin's enablement, or
to a harness config — and BEFORE concluding that a dispatcher row is wrong. A
dispatcher pointing at a skill the session cannot load presents exactly like a
badly written dispatcher, and the wrong fix (rewriting the row) leaves the
real defect in place.

Scope the enabled set to what the repo actually does. A repo that never builds
images or runs pods should not enable the image/pod plugins: besides the token
cost, each one contributes its pod-provided MCP servers, which then log
connection failures for boxes that were never meant to be running there.

**Updating the marketplace is NOT updating the plugin.** The corpus is cached
per marketplace COMMIT
(`~/.claude/plugins/cache/<marketplace>/<plugin>/<sha>/`).
`claude plugin marketplace update <marketplace>` refreshes the marketplace
clone only — an installed plugin keeps resolving its OLD commit, so a fix that
has already merged and regenerated stays invisible: a removed skill still
loads, a removed MCP server still fails on every session. Move the plugin
itself, at the scope it is installed at:

```bash
claude plugin update <plugin>@<marketplace> --scope project
```

It names the commits it moved between (`updated from <old> to <new>`), and
that line is the confirmation to look for — a refreshed marketplace clone is
not evidence that any session sees the change. `--scope` defaults to `user`
and fails outright on a project-scoped plugin.

## When to Update Skills

| Trigger | Action |
|---------|--------|
| Deployment step fails or needs undocumented workaround | Update the relevant `/charly-core:*`, `/charly-build:*`, `/charly-check:*`, `/charly-automation:*`, kind plugin (`/charly-image:*`, `/charly-vm:*`, `/charly-kubernetes:*`, `/charly-local:*`, `/charly-pod:*`), per-pod plugin (`/charly-jupyter:*`, `/charly-coder:*`, …), or split-foundation plugin (`/charly-distros:*`, `/charly-languages:*`, `/charly-infrastructure:*`, `/charly-tools:*`) |
| Verification check missing from image skill | Add to the image skill's Verification section |
| Skill's recommended defaults are wrong | Fix in the skill, not the project rulebook (`AGENTS.md`) |
| New feature added to charly CLI | Update `/charly-core:<cmd>` or `/charly-build:<cmd>` skill + `/charly-internals:go` source map |
| New candy or box added | Create skill via `charly box new candy` scaffold or manual SKILL.md |
| Bug fix changes behavior | Document the fix in affected skills |
| Cross-skill behavior discovered | Update Cross-References in all affected skills |
| A doc / skill / comment diverges from observed reality (discovered by ANY means — not only a bed or a deleted identifier) | Treat as an incident (R1): RCA it, then sweep EVERY sibling doc/skill/comment carrying the same false/outdated/misleading claim and fix them all in the current cutover (blocking, R1). The rows below are special cases of this |
| A live bed contradicts a skill's claim (Risk Driven Development found it stale) | Fix the stale skill in the same change — RDD keeps the living docs honest; for a high-risk claim the running system is ground truth, not the doc |
| Removed identifier still referenced in skill paragraph (R5 self-test failed) | Update / delete the paragraph in the same commit as the removal (R5) |
| Project-rulebook heading / R-number / clause name changes (they are a public API) | Sweep every mirroring surface in the same commit — see "Mirroring surfaces" below (R5) |
| Rule DETAIL accretes inside the project rulebook (matrix, catalog, worked example growing in place) | Move the detail to its owning skill (see the Authoritative-copy registry); the project rulebook keeps the mandate + a `*Detail:*` pointer |

## Skills are PUBLISHED — the corpus is a public surface

Every skill in this repo is rendered as a page on **opencharly.ai** by `charly docs generate`
(see `/charly-build:docs`). Three consequences bind every skill edit:

- **The frontmatter `description:` is public.** It becomes the page's subtitle and meta
  description, so it is read by people who have never seen the repo.
- **A dangling `/charly-<plugin>:<skill>` cross-reference BREAKS THE DOCS BUILD.** References are
  rewritten into site links at generation time and an unresolvable one is a hard error, not a
  silent dead link. Renaming or deleting a skill therefore means sweeping every reference to it
  in the same change — which the build now enforces rather than trusting.
- **`references/*.md` split files become child pages**, linked from their entry card. They carry
  no frontmatter by contract; only the entry `SKILL.md` does.

The upside is that the corpus's internal consistency is now machine-checked: the docs
repo's drift gate (its deploy workflow regenerates and fails on any diff) and the docs
build together catch a stale cross-reference that a `git grep` sweep missed.

## When NOT to Update Skills

- **Ephemeral issues** — use conversation context or memory
- **User-specific config** — use the active harness's private memory or user configuration
- **Bug fixes in charly code** — the fix is in git; document behavioral changes in skills only
- **Anything derivable from code** — skills document *usage*, not implementation details
- **Historical / version-history content** — dated change notes, renames, completed cutovers, retired identifiers → the repo's `CHANGELOG/`, never a skill or the project rulebook (the project rulebook "Where things are documented" owns the full doc-split). Skills describe current behavior in present tense only; write a landing cutover's narrative to its release `CHANGELOG/<YYYY.DDD.HHMM>.md` entry and state the new standing rule forward-looking in the skill, with no history.

## How to Update

The rule this section implements — authored content lives in the repo that owns it, and any binding
that crosses a repo line is DERIVED rather than hand-maintained — is the umbrella `AGENTS.md`
Part II #5. What follows is only the how.

1. **Edit the skill's own FILE, in the repo that owns it.** A skill's prose lives at
   `<owning candy dir>/.agents/skills/<name>/SKILL.md`: the `<name>` DIRECTORY is the skill id, the
   frontmatter `name` must agree with it, and an optional `references/<stem>.md` sits beside it (no
   frontmatter, by contract). A sub-agent DEFINITION is ONE file at
   `<owning candy dir>/.agents/agents/<name>.md` — the SIBLING tree, never a child of
   `.agents/skills`, and a candy's `skills:` list does not govern it. `.agents/skills` is what a
   candy's `skills:` list means when it declares none (a candy that declares another tree is honoured
   by the generators; the OCI-label collector reads only the default one — a stated limit at
   `sdk/deploykit/skills_collect.go`). Both shapes go through ONE reader
   (`sdk/candywalk/skillfile.go` — `AgentsSkillsDir` / `AgentsAgentsDir`), so the file you author and
   the file the corpus publishes parse identically. **You write the body; nothing writes it for
   you.**

2. **Leave the `skill:` node carrying ROUTING only.** Its sibling node in the owning repo's
   `charly.yml` keeps `family`/`owner`/`triggers` (plus `category`, and `type: agent` with
   `model`/`tools` for a sub-agent definition) — that is what the R0 dispatcher, the corpus family
   and the OCI-label association consume. The prose fields `description:`/`content:`/`references:` are
   the TRANSITIONAL in-line form: `spec/schema/skill.cue` still declares them and a node that carries
   them instead of a file is still read, but they are being removed — author in the FILE and delete
   the node's copy as part of the move.

3. **An inline body that DISAGREES with the file is a HARD ERROR** — the one failure mode that makes
   the in-line advice actively break a migrated repo. The marketplace generator refuses the node:

   ```
   skill %q: the node's inline `content:` and its file disagree — delete the inline body and keep the file
   skill %q: the node's inline `description:` and its file's frontmatter disagree
   ```

   (`plugin-marketplace/candy/plugin-marketplace/skillfile.go`, and the same pair for an `agent`-type
   node); `references:` declared on BOTH sides is refused as well. A repo cannot land a corpus while
   one of its nodes is in that state. **`charly box validate` is not the gate that catches this one**
   — it resolves the file and takes it, so a stale inline copy passes it. Moving a body means
   DELETING the node's `content:`/`description:` in the same change, never copying the file back over
   them.

4. **An IMAGE cannot own a resolvable skill.** A `candy:` node carrying `base:` or `from:` folds into
   the BOX set and never the layer set, so it has no source directory at all: its skill's body
   resolves nowhere, and the node is silently absent from the `ai.opencharly.skill` OCI label too — it
   is invisible, not merely unreadable. `charly box validate` rejects the shape
   (`… owner %q is an image, not a candy …`, `plugin-box/candy/plugin-box/validate_skills.go`,
   `opencharly/sdk#434`). `owner:` must name a PACKAGE candy the image composes; for a repo whose
   skills belong to images the sanctioned shape is a `<repo>-skills` carrier candy — a real candy,
   with install content that `copy:`s `.agents/skills` to the account's `~/.agents/skills` and an ADE
   `check:` proving it landed, composed by every image that carries one of its skills.
   `opencharly/distro-arch`'s `candy/arch-skills` is the worked example. The carrier is deliberately
   COARSE: one carrier composed into N images puts that repo's whole skill set on all N labels.

5. **Regenerate the corpus** in the marketplace repo — the ownership table, the command, the deploy
   drift gate and the daily refresh all live in `/charly-internals:marketplace`. The file you edited
   is the generation SOURCE; `marketplace/**/SKILL.md` is a PROJECTION carrying a DO-NOT-EDIT banner,
   and an edit there is reverted by the next regeneration, silently and without conflict. `git grep
   -l 'DO[- ]NOT[- ]EDIT'` tells you which files are generated — but triage the hits rather than
   trusting them, since a file may MENTION the banner without carrying one (agent definitions under
   `marketplace/<family>/agents/` are hand-authored).
6. If the insight affects cross-skill behavior, update the project rulebook (`AGENTS.md`) too
7. After any non-trivial deployment session, ask: "Did we learn anything that future sessions should know?"

### Regenerate ONLY the projections of the sources you edited

**A projection merges before the superproject source that pins it**, so prose can be
correct and landed in `plugins`/`docs` while the owning repo's skill file is still in flight
on another branch. Regenerating from a superproject tree that lacks that pending source
does not bring the projection up to date — it **drags it backwards**, and the diff is
indistinguishable from a legitimate update.

Measured: a wholesale `charly marketplace generate` touched **21** files in `plugins`
and **41** in `docs` when three skill files had changed. The extra ones were reverts,
about 500 lines of landed prose including 317 from one `git-workflow` reference and 107 from
the `pr-validator` agent spec. Nothing in the output flagged it.

So: after regenerating, **restore every file whose source skill file you did not touch**, and
audit the deletions that remain — each one should be a line you deliberately replaced.

```bash
git -C plugins diff --numstat        # +added -deleted per file
# Every deleted line, read in full. Do NOT use `grep '^-[^-]'` to skip the
# `--- a/file` header: a deleted markdown BULLET renders as `-- text`, so that
# pattern silently drops it — and in a prose repo bullets are the majority shape,
# not an edge case. Measured on one real regeneration: 90 reported vs 96 actual,
# every missed line a bullet. Exclude the header explicitly instead, and
# cross-check against --numstat, which counts independently of any regex: if the
# two disagree, the regex is wrong, and that disagreement IS the alarm.
git -C plugins diff | grep '^-' | grep -v '^--- '
git -C plugins diff --numstat | awk '{s+=$2} END{print s}'   # must equal the count above
```


**After ANY merge, `update-branch`, or conflict resolution, RE-RUN this audit against the NEW
base.** A revert introduced by catching up is invisible to the old one, and the old number keeps
looking right. Measured on one branch: against the original merge-base 580 deletions; against
`origin/main` after the catch-up, **1170** — the second is what the squash actually lands. Both
numbers were correct; only one was the merge. A grep-vs-`--numstat` cross-check agreed perfectly
throughout and was measuring the wrong baseline, so **agreement between two methods does not
rescue a wrong base**. The one-line form that surfaces every offender:

```bash
git diff --numstat origin/main...HEAD | awk '$2>$1'   # every file losing net lines
```

It caught eleven files at once, including a whole skill file deleted outright — after five
consecutive audits had reported the branch clean.

**And a conflict resolution is not separate from the exclusion list — it is the same concern.**
Resolving conflicts by regenerating is right for projections, but a regeneration run WITHOUT the
protected-file list re-introduces exactly what the list exists to prevent. This recurred on two
hops within an hour, the second at ~19x the scale of the first.
A file you never edited appearing in that list is the signal, and "it was probably just
stale" is the reading that ships the revert.

## Skill File Structure

This is the shape of the file you AUTHOR — and it is the same shape the corpus publishes, because one
reader parses both:

```markdown
---
name: <skill-name>
description: |
  One-sentence description of when to invoke this skill.
  For layers: "Use when working with <component>."
  For images: "MUST be invoked before building, deploying, or troubleshooting the <image> image."
---

# <Title>

## Overview / Properties
## Key Sections (varies by type)
## Cross-References (related skills)
## When to Use This Skill
```

Two frontmatter keys, and no marker of your own: the generator adds its DO-NOT-EDIT banner to the copy
it publishes, and the shared reader (`sdk/candywalk`) strips that marker on the way back in — which is
what makes the authored file and the published file round-trip to the same body. The EOF convention is
one trailing newline (`TrimSuffix(content, "\n") + "\n"`, `plugin-marketplace#33`): a body authored
without one differs from its own projection by that byte on every skill of the repo.

## Progressive disclosure: multi-file skills

Both halves are files in the skill's own directory, authored together: the entry `SKILL.md` plus one
`references/<stem>.md` per topic. Nothing emits the split for you — the corpus projection copies both,
and both are read back through the same reader.

A skill is either a single `SKILL.md`, or an entry `SKILL.md` plus sibling `references/*.md` files in the same skill directory, loaded on demand by path. Split once a single file grows past ~400 lines or spans more than one cleanly-separable topic.

- The entry `SKILL.md` keeps its frontmatter unchanged, an overview, a "when to use" section, and an index table mapping each topic to its `references/<file>.md` with a one-line description — entries stay ~100–150 lines total.
- Each `references/<file>.md` carries the full detail for its topic in plain markdown with NO frontmatter (only the entry file is dispatched by the `Skill` tool).
- Cross-file links are relative (`references/<name>.md`); a heading another file quotes keeps its text stable across the split.
- An Authoritative-copy registry owner (below) may keep its canonical full text inside a `references/<file>.md` split file rather than the top-level `SKILL.md` body — the registry still names the SKILL as sole owner; the reference file is where the detail actually lives.

## Rulebooks vs Skills

| Content type | Where it belongs |
|-------------|-----------------|
| Project philosophy, architecture, key rules | harness root rulebook (`AGENTS.md`) |
| Command usage, flags, examples | `/charly-core:<cmd>` or `/charly-build:<cmd>` skill |
| Layer properties, packages, ports | per-pod plugin (`/charly-jupyter:<name>`, `/charly-coder:<name>`, …) or split-foundation plugin (`/charly-distros:*`, `/charly-languages:*`, `/charly-infrastructure:*`, `/charly-tools:*`) for base layers |
| Image composition, deployment, verification | per-pod plugin or `/charly-distros:<name>` / `/charly-infrastructure:<name>` for base images |
| Skill disambiguation (which skill to use) | the active harness root rulebook's R0 Skill Dispatcher |
| Detailed operational patterns | Relevant `/charly-core:*` / `/charly-build:*` / `/charly-check:*` / `/charly-automation:*` / kind-plugin skill |
| Hard rule / gate / mandate (the WHAT and the MUST) | the umbrella `AGENTS.md` when org-wide, a subrepo's `AGENTS.md` when specific to that repo — stated ONCE, with an owning-skill pointer |
| Operationalization / matrix / catalog / worked example (the HOW) | The ONE owning skill (see the Authoritative-copy registry below) |
| Version history / past changes / renames / cutover narration | the repo's `CHANGELOG/` (per-CalVer `<YYYY.DDD.HHMM>.md`) — never the project rulebook or a skill |
| Long-term thesis / vision / aspiration ("why & where it's going") | `VISION.md` (repo root) — never restating command usage, architecture, or history |

## Where guidance belongs

Every new fact belongs on exactly ONE surface, and every surface has a reader.
Choosing the surface wrong either duplicates the fact (two copies drift
apart) or hides it where its reader never looks:

| Surface | Carries | Put a new fact there when |
|---|---|---|
| `README.md` | the user-facing overview — what the repo is, how to use it | the reader has never opened the source: what the component does, how to install/run/use it |
| `AGENTS.md` | the harness-neutral RULEBOOK — short, decisive policy (the WHAT and the MUST) | the fact is a hard rule, gate, or mandate every session must obey, or a pointer (trigger → owning skill) |
| the owning skill | the detailed HOW — procedure, matrices, catalogs, worked examples, command sequences | the fact is operationalization detail one rule needs, or how-to that would bloat the rulebook |
| `CHANGELOG/` | history — dated change notes, renames, completed cutovers, retired identifiers | the content is about the PAST (what changed, why, when). Never advice; nobody reads history for guidance |

"Mandate in the harness rulebook, detail in the skill" (below) is this same
split seen from the rule side: the rulebook states the mandate plus a pointer;
the owning skill carries the detail. README and `CHANGELOG/` are the other two
poles — the user overview and the history.

**Anti-patterns — each is a placement mistake, not a wording one:**

- **Policy prose drifting into a skill.** A MUST / gate / mandate written only
  as skill prose: a session that loads the rulebook alone never sees it. The
  rulebook states the mandate; the skill carries the how.
- **How-to bloating `AGENTS.md`.** Matrices, catalogs, worked examples, and
  command sequences are detail — move them to the owning skill and leave the
  rulebook the mandate plus a pointer; the rulebook stays short.
- **History kept in a rulebook.** Dated notes, rename narratives, and cutover
  stories go to `CHANGELOG/<YYYY.DDD.HHMM>.md`; the rulebook and the skill state
  only the standing rule, present tense, forward-looking.
- **Harness / file names leaking into harness-neutral policy prose.** `AGENTS.md`
  is harness-neutral: name the ROLE (the agent, the watcher, the gate), never the
  harness or a harness-specific file. Harness mechanics live in the per-harness
  config or skill.

The umbrella rulebook's "Ledger and interruption safety" rule is a
worked example of the split: the rulebook states the MUST and names the owning
skill, and the sibling `/charly-internals:agents` owns the ledger procedure
(see `/charly-internals:agents`, "Todo ledger & interruption safety").

## Mandate in the harness rulebook, detail in the skill

The canonical split for every rule: **the harness rulebook (`AGENTS.md`) states
the equivalent mandate and points at exactly one owning skill; the owning skill
carries the operationalization** —
forbidden-pattern catalogs, decision matrices, worked examples, command
sequences. Every other document links to the owner and NEVER restates it
(restated copies drift; the linked original cannot).

The harness rulebook (`AGENTS.md`) owns the **Skill Dispatcher**, **RDD**,
**ADE**, and **SDD** mandates, the acceptance checklist, and AI-attribution
tiers. Keep its policy equivalent while preserving harness-specific tool
language.

### Authoritative-copy registry

| Matrix / catalog / definition | Sole owner |
|---|---|
| "R10 gate by change class" matrix (incl. the class → gate → tier cross-walk) + "Flag discipline" catalog | `/charly-check:check` |
| R1–R5, RDD (incl. "The spike"), ADE operationalization (forbidden patterns, risk table, worked examples) | `/charly-internals:strict-policy` |
| SDD operationalization (the schema→generated-code pipeline map, the schema-change recipe, the generation-coverage current state) | `/charly-internals:go` |
| Hard-cutover workflow, forbidden patterns, deliverables | `/charly-internals:cutover-policy` |
| `disposable:` / `preemptible:` flag semantics, "What counts as an R10 run" | `/charly-internals:disposable` |
| Landing mechanics (branch loop, the two-step PR + `pr-validator` merge/tag, CalVer-generated-at-merge, branch protection, multi-repo order) | `/charly-internals:git-workflow` |
| Agent/workflow/team primitives, hooks doctrine | `/charly-internals:agents` |
| Kernel/plugin doctrine (core = kernel; every capability a plugin candy), the two authoring shapes, placement, the three-lane transport doctrine, the seams catalog, **the kernel/plugin boundary law** (E/M/B/D/R) + the incomplete-seam mandate | `/charly-internals:plugin` |
| Skill Dispatcher, RDD/ADE/SDD mandates, R1–R10, acceptance checklist, attribution tiers, Documentation-only change class anchor, "What the validator checks" table, "Commit, push, land" | the umbrella `AGENTS.md`, Part I (`charly/AGENTS.md` carries only the charly-core rules: core dispatcher rows, the kernel/plugin boundary law, the core SDD pipeline, R9 + the core Go gate) |

A registry owner may hold its canonical text in a `references/<file>.md` split file rather than the top-level `SKILL.md` body (see "Progressive disclosure" above) — the row still names the SKILL as sole owner regardless of which file inside it carries the detail.

## Mirroring surfaces — sweep when rulebook wording changes

**The project rulebook's section headings, R-numbers, and named clauses ("flag-override
clause", "gate by change class", "Documentation-only change class",
"documentation reviewed" tier, "Acceptance checklist", "Commit, push,
land", "What the validator checks", …) are a public API.** These surfaces reference them and MUST be
swept in the same commit as any rename or removal (R5):

- the 2 hooks in `.claude/hooks/` (`pre-commit-gate.sh`, `pre-push-gate.sh` — deterministic
  command-mechanics gates only; there is no reminder-hook layer),
- the agents in `marketplace/internals/agents/*.md`,
- `charly/AGENTS.md` (the charly-core rules; it points at the umbrella `AGENTS.md`
  for every org-wide section and must never restate one),
- the per-directory signpost `AGENTS.md` files (`candy/`, `marketplace/`, `docs/`,
  each `box/<distro>`),
- the workflows in `.claude/workflows/*.js`,
- every SKILL.md that quotes a section name (grep before assuming).

The sweep test: `grep -rn '<old phrase>'` across the superproject + submodules
returns only `CHANGELOG/` context afterwards. Prefer keeping headings and
clause names STABLE when rewording content — a stable name keeps every
mirroring surface valid for free.

## Command skills vs topic skills

Most skills under `marketplace/core/skills/` and `marketplace/build/skills/`
map 1:1 to a top-level charly command (e.g. `/charly-build:build` ↔ `charly box build`,
`/charly-core:charly-status` ↔ `charly status`).
**Topic skills** are the exception: they don't correspond to a
top-level command but cover a cross-cutting concept surfaced by flags
or layer composition. Today's topic skills:

| Skill | Surfaced via | What it covers |
|---|---|---|
| `/charly-automation:enc` | `charly config --encrypt`, `charly config mount`, `charly config unmount`, `charly config passwd` | Encrypted-volume (gocryptfs) semantics, keyring resolution, `charly-enc-<image>-<volume>.scope` lifecycle |
| `/charly-automation:openclaw-deploy` | Composing `openclaw-*` layers | OpenClaw AI gateway deployment story |
| `/charly-automation:sidecar` | `charly config --sidecar tailscale` | Sidecar-container model, pod networking, env-var routing |

When adding a new command, always create a matching command skill. Consider a topic skill when a concept spans multiple commands or layers and the natural home isn't any single command's skill. Keep the frontmatter `description:` explicit about the topic nature (the blocking `Skill:` tool dispatcher matches on description keywords).

## Plugin Structure

Plugins are sorted into four use-case buckets. Directory names live at
`marketplace/<name>/` (no `charly-` prefix); plugin.json `name:` fields keep the
`charly-` prefix; every skill is invoked as `/charly-<plugin>:<skill>`.

The authoritative per-plugin skill counts and purposes are the bucket tables
in `marketplace/README.md` — point there, never copy them (counts drift).

## Agent & signpost conventions

### Agents (`marketplace/<family>/agents/<name>.md`)

Sub-agents are markdown + YAML frontmatter (`name`, `description`, `tools`,
`model`, …), discovered from a plugin's `agents/` directory
(`charly-internals/agents/`, `charly-automation/agents/`). **Plugin-loaded agents IGNORE the `hooks`,
`mcpServers`, and `permissionMode` frontmatter fields** — keep those out of
plugin agents (use `.claude/agents/` or `settings.json` if you genuinely
need them). The charly roster splits into **enforcers** (root-cause-analyzer,
layer-validator, testing-validator — gate claims) and **executors**
(check-bed-runner, deploy-verifier — drive `charly check` and return verbatim
proof). Full story: `/charly-internals:agents`. Dynamic workflows are NOT plugin
content — they live in the superproject's `.claude/workflows/*.js`.

### Per-directory AGENTS.md signposts

The repo-root `AGENTS.md` is the single, complete, harness-neutral rulebook.
Per-directory `AGENTS.md` files (`charly/`, `candy/`, `marketplace/`, `docs/`, and each
`box/<distro>` submodule) are THIN signposts only: they name the skills to
load for that area and point back to root. They MUST NOT restate any rule body —
duplication drifts (an earlier layer-validator and the reminder hooks both drifted
exactly this way; the reminder hooks now name rules as pointers/triggers, never
restating their bodies). Subagents and teammates load the full `AGENTS.md`
hierarchy from their working directory, so a signpost reaches a worker scoped
to that subtree without bloating root.

## Two-Layer Sync Architecture

The project uses two complementary sync mechanisms with `.gitignore` as the
boundary:

| What | Synced by | Visibility |
|------|-----------|------------|
| Code, `AGENTS.md`, skills, layers, images | Git | Public (committed) |
| `.claude/memory/` (auto-memory) | Syncthing | Private (gitignored) |
| `.claude/settings.local.json` (personal overrides) | Syncthing | Private (gitignored) |
| `.claude/settings.json` (project policy) | Git | Public (committed) |

Memory setup: `autoMemoryDirectory: ".claude/memory"` in
`.claude/settings.local.json`. Both `settings.local.json` and `memory/`
propagate via Syncthing so your working state follows you across machines
without polluting the public repo.

Rule of thumb: **if it's useful to every contributor, it lives in git**
(skills, `AGENTS.md`, code). **If it's useful only to you, it lives in the
Syncthing-synced half** (memory, personal settings).

**A generalizable PROVEN insight saved only to memory is a knowledge LEAK — every
other charly user (and every fresh validator/teammate, which never reads your
memory) re-hits the same problem and re-derives the same fix.** So the moment an
insight is PROVEN (a spike verdict, a discovered mechanism, an operational
failure-mode + its fix), codify it into the OWNING committed skill IMMEDIATELY as
a documentation-only cutover — never batch codification to a program-end docs
phase, and never let memory be its only home. Memory keeps the operator-specific
remainder: host facts (this box's hardware, enabled features), personal
preferences, in-flight session state. The test: *would a stranger cloning the
repo want this?* Yes → committed skill, now. Only you → memory. (Skills also
survive context compaction — conversation context does not — which is the same
reason insights can't live only in the transcript.)

## The replacement claim carries the removal's burden of proof

When a doc/skill/comment is found false, the fix is TWO claims, not one: the deletion, and
whatever is written in its place. Both need evidence. The failure mode is applying rigor to the
deletion and a plausible guess to the successor — the false claim is gone, a new one ships, and
the sweep that would have caught it already passed.

This is recorded because it happened four times in one cutover, across three repos. Two of the four
were corrections to a stale `directory:` claim (charly, sdk); the other two were corrections to the
cross-reference rewriter's own documentation (plugins). What they share is not the subject but the
mistake:

| Replacement written | Why it was also false |
|---|---|
| "inline entries have no source directory of their own" | `ScanInlineCandy` is passed `rootDir`; an inline candy's SourceDir always equals its declaring file's dir |
| "the branch fires for a remote (`@github`) or submodule-vendored candy" | `CandyCopySource` early-returns on `GetRemote()` as its FIRST statement — a remote candy can never reach that branch |
| "the 11 frontmatter refs are published as plain subtitle text" | `firstLine` truncates the description to its first sentence; all 11 sit past the cut and are published nowhere |
| "3665 references … every reference is rewritten" | 3654 are rewritten; the count was taken whole-file and described as body-scope |

Every one shares a root cause: **measuring or reasoning at one scope, then describing another.**

Practical rules:

- **Trace the successor before writing it.** A claim about which branch runs needs the early
  returns above it read; a claim about what is published needs the emitter read, not the producer.
- **State a partial enumeration as partial.** "an inline candy … and a `discover:` entry can point
  elsewhere" is honest; an implied-complete list you cannot defend is not.
- **Re-derive figures at the scope the sentence names**, with the code's own matching rules where
  one exists — not an approximation of them.
- **A correction is a new claim, not a cleanup**, and gets the same gate the original edit would.
- **Sweep the claim, not the sentence — and defeat line wrapping.** A claim-keyed `git grep` of the
  phrase *as written* silently misses a copy whose text wraps mid-phrase: `… four consecutive PASS`
  / `runs proved …` matches neither `"four consecutive PASS runs"` nor a line-scoped regex. Grep the
  shortest distinctive fragment that cannot straddle a line break, or normalize first
  (`git grep -h '' -- '*.md' | tr '\n' ' '`). This bit twice in one PR: once as string-keyed vs
  claim-keyed (a table row restating corrected prose), once as a wrapped sentence reported clean.
- **Count the surfaces before declaring a sweep done.** The same claim routinely lives in a skill, a
  CHANGELOG, a candy comment and a PR body. Fixing the history and leaving the guidance is the worst
  outcome: a skill is what the next session loads, a CHANGELOG is what nobody reads for advice.

## Skill↔code source-map sync audit

Many skills carry a *source map* of the Go code — `Source:` frontmatter, file-listing
tables (`| charly/foo.go | … |`), and inline identifier references (type/function names).
These silently desync when the code is refactored (a renamed file or symbol that no
skill's grep self-test caught). Audit and fix them as follows:

1. **Extract the claims**: pull every `charly/*.go` filename reference and identifier
   reference out of the skills, and check each against the live source — file exists?
   symbol still defined? (`gopls`/`grep` confirm.)
2. **Search INSIDE the submodule**: `marketplace/` is a git submodule, so a superproject
   `git grep -- plugins` is a **FALSE ZERO** (git grep does not cross the gitlink). Use
   `git -C plugins grep` or filesystem `grep -rn`.
3. **Filter the false-positive classes** before flagging:
   - URL substrings (`pkg.go.dev`, `dl.google.com` → matches `.go`),
   - substring artifacts (`mcp.go` contains `p.go`; `localpkg.go` contains `pkg.go`),
   - legitimate non-charly refs (upstream files like `storage_dest.go`),
   - prose anti-pattern examples (a skill *mentioning* `deprecated.go` as a thing to avoid).
4. **Claim-keyed sweep (R5)**: a stale claim is fixed across EVERY skill that repeats it,
   not just the file where it surfaced — `git -C plugins grep '<claim>'` returns only
   CHANGELOG context afterwards. **Key the grep to the CLAIM CLASS — every wording AND
   syntactic variant of the false claim — NOT the one surfacing string you just edited:
   the same false claim survives under a paraphrase and under a singular/plural or
   wrapper-form variant (a retired-grammar `vms:` fix must ALSO grep the `vm:`-wrapper,
   `cpus:`, and `deployments:` variants of the same dead grammar; a sweep keyed only to
   the first string leaves a sibling variant alive and costs a second validator round).**
   The sweep also covers the cutover's OWN freshly-drafted narrative — the CHANGELOG
   entry (step 5), PR description, and commit message — since a false claim can be
   reintroduced there even when every pre-existing skill was correctly fixed.
5. **Land docs-only**: skill edits are the Documentation-only change class →
   `documentation reviewed` (no beds); the plugins commit carries a
   `CHANGELOG/<YYYY.DDD.HHMM>.md` entry too (every plugins landing does — `plugins`
   is no CHANGELOG-exception), and a cutover that regenerates the `docs` mirror
   lands a `docs` commit carrying its OWN entry (every docs landing does — `docs`
   is no CHANGELOG-exception either); plugins commit → docs commit → superproject
   pointer bumps → all tagged `v<YYYY.DDD.HHMM>` at merge (plugins and docs are
   tagged too, same as every repo — the one finalized CalVer names each repo's
   changelog file and its tag). See `/charly-internals:git-workflow`.

**Alias residue is a special case — never assert its exact location.** A `charly/*_aliases.go` line is
transitional K-wave residue that ZERO-ALIASES/K5 deletes, so a skill asserting its exact per-symbol
file:line desyncs on every alias relocation and documents a path already scheduled for deletion. State the
stable spec-source (the owning plugin/sdk package) and the K5-dissolution destination instead of a
concrete alias location, and `grep -rn` every concrete alias-attribution claim (a file:line or symbol
name) against the live tree before asserting it. Failure mode: a row naming exact residue locations
produces alias-attribution errors round after round once that residue moves.

The code-side companion (golangci-lint, the `.go` compliance checklist) is
`/charly-internals:go-quality`.

## Cross-References

- `/charly-internals:go` — Source code structure, adding new commands
- `/charly-internals:generate-source` — Understanding generated Containerfiles
- `/charly-internals:agents` — Sub-agents, dynamic workflows, agent teams; how they drive the `charly check` beds; the hooks doctrine; the signpost convention
- `/charly-build:validate` — Validation rules
- All `/charly:*` skills — Individual command documentation

## When to Use This Skill

Invoke when updating documentation, creating new skills, auditing skill coverage, or deciding where new information belongs (the project rulebook vs skill vs memory).
