# Carry Agent Contract

Current gate: **Issue #6 — non-Node pre-Node15 coherence corrective.** Issues #3/#5 and Node 14 are closed. Node 15 research and production remain paused until Issue #6 has one fresh coherence Gate, `make check`, one corrective commit, push, terminal CI, and closure; then Node 15 restarts from its simplified journey rather than the paused design.

Carry is an AI teammate: a person hands a responsibility over in carry.ai Web, a named Agent owns keeping it moving, and the Work stays true across Conversations, Agent sessions, Hosts, channels and time.

Nodes 0–12 are technical evidence for an older contract. The old Node 13–19 route is void. `docs/implementation.md` owns the only active route, Nodes 13–30.

## Canonical documents

| File | Owns |
| --- | --- |
| `AGENTS.md` | this contract and the gate order |
| `README.md` | repository entry point |
| `docs/product.md` | screens, actions, product vocabulary, privacy, invariants |
| `docs/architecture.md` | fact owners, topology, authority, concurrency, package direction |
| `docs/code-style.md` | enforceable aesthetics gates |
| `docs/repository.md` | paths, generation, commands, CI, Issue-owned artifacts, deletion |
| `docs/implementation.md` | Node route, research procedure, review protocol, evidence |

Read the parts that own the change you are making. A current explicit user decision wins; update the owning document before implementing against it. Do not keep an obsolete path alive unless a real published consumer depends on it.

## Settled topology

Do not reopen silently:

- humans use carry.ai Web; sign-in is GitHub, Google or Email, and GitHub sign-in grants no repository authority; MVP has no onboarding form; after sign-in a member accepts an invited Space or chooses/creates one;
- a Space has a display name that may repeat and a globally unique normalized slug derived from it; MVP slugs are immutable; invite links are single-use, expiring and revocable; Membership carries only the two already-earned independent authorities to manage members and connect Hosts, with no role hierarchy or generic permission system; until Space end each authority has at least one Active holder;
- a Space has many logical Hosts; V1 statically composes at most one default Pi and one default Codex Agent per Host, with no slot/profile/registry product, and an Agent belongs to exactly one Host;
- Agent is a durable fact owner and a user-visible identity: stable ID, Space, PostgreSQL-allocated Space-unique normalized name (`Pi`/`Codex`, paired numeric suffixes for later Hosts), deterministic preset avatar, one human owner, one Host binding, Active/Removed lifecycle, created time; a new Agent's human owner is the still-active authenticated member who approved that Host connection, while repeat discovery never changes an existing Agent's owner, name or lifecycle; every Active Agent is selectable by members of its Space, and a Host going offline does not delete Agent identity;
- Machine owns complete Pi/Codex present/absent observations; Online and Last active derive from Agent lifecycle plus database-time freshness, while current Runs and any optional model choice remain separate derived facts; copied Machine state is one logical Host, fresh setup after state loss is a new Host, and there is no physical-clone guess or provider/model/runtime registry;
- the `carry` executable is the long-running Host, shallow operator commands for Host setup, and an Agent-facing interface; it is never a parallel human product, and an Agent never holds member credentials, the Machine private key or a direct Server path;
- a Conversation fixes its Agent at the first message; changing Agent starts a new Conversation; provider continuation handles stay private to the owning Host and there is no public Session API;
- every Work has exactly one human owner (goal, scope, external authorization, Inbox response, acceptance, closure) and exactly one Agent owner (progress, plan, comment interpretation and routing, collaborator choice, notices, schedules); Host or Agent loss never deletes Work or silently replaces that owner;
- Work is created only through an Agent — Conversation, the Web `Create with <Agent>` form, or a registered local Agent calling the CLI — never by a direct Server or Web bypass;
- Server never pushes work: Conversation, Work and Run record the exact target Agent, the owning Host pulls and claims through PostgreSQL, and an unavailable target becomes explicit user-visible state instead of a fallback to another Agent;
- emergency Host/Agent revoke takes effect without waiting for handoff; removal authority never grants authority over affected Work; only each Work's human owner may transfer future Agent responsibility to an Active Agent in the same Space that is available at commit time; owner-unavailable Work derives directly into that human owner's Inbox; the old Agent remains historical, old submissions are rejected, schedules pause, and the replacement starts from durable Work facts rather than provider-private state;
- a current owner may voluntarily transfer only their own Work or Agent responsibility; a member manager who forcibly removes another member cannot choose third-party successors and must atomically take human ownership of that member's Open/Paused Work while the removed member's Active Agents become Removed; if the target is the last Host-connection authority holder, the remover also inherits that one narrow authority; Membership revocation and both Web-request/local Agent Work creation serialize in PostgreSQL so no Active Agent or Open/Paused Work points to an invalid member;
- Space end never closes Work on another human owner's behalf: every Open/Paused Work must first be closed through its ordinary lifecycle; ending then revokes future Space authority while preserving existing Work lifecycle and history under the researched retention boundary;
- Work shows both owners, participating Agents, each Agent's current activity, an ordered plan that is display truth only, and outputs; Inbox is a query over Work facts; email and Feishu deliver automatically once a channel is connected, and delivery failure or Unknown never changes Inbox;
- V1 needs one real non-code journey before code-to-PR.

Representation is not topology. Table shapes, columns, indexes, cardinalities, transports and command names are earned by a Node's research and frozen in its design freeze, not assumed here.

Forbidden without a new user loss, identity, lifecycle and authority boundary: `Assignment`, `Coordinate`, a `Plan`/`Step`/`Artifact` owner, a generic `Effect`/`Action`/`Capability`, a Session owner, a participant owner, a notification owner, a scheduler owner, a workflow engine, a provider/model/runtime registry, and `Orchestrator`/`Manager`/`common`/`utils` layers. Try an existing owner, field, function or local value first.

## Operating rules

1. Ask only when the answer changes the journey, owner, authority, consequence or approved scope. Decide ordinary engineering yourself.
2. Use the fewest concepts that still protect truth, privacy, authority, failure and evidence.
3. A Node is vertically complete: owner, migration, query, protocol, client, UI, CLI, test, docs and deletion. New and old paths never both live at Node close.
4. A green command is not evidence. Observe the changed path succeed, fail, recover and reach the user.

## Gate order

Before a Node starts, open one GitHub Issue. That Issue is the durable owner of the Node's journey freeze, research, evidence rows, canaries, design freeze and exact file budget. The repository never gains plan, review, audit, handoff or evidence documents.

Every Node runs these gates in order; skipping one is a blocker. The executable procedures live in `docs/implementation.md`. The only lighter path is a non-Node coherence corrective that satisfies every condition in `docs/implementation.md` §1; it uses one Issue, one fresh coherence Gate, `make check`, one corrective commit, push and terminal CI, and cannot carry a product journey or architecture migration.

1. **Journey freeze** — the ten-field block in `docs/implementation.md` §2, recorded in the Issue. A product reviewer may reject it before any research.
2. **Question-led research** — `docs/implementation.md` §3: a frozen question plus a disconfirming question, sources chosen by the relevance matrix rather than popularity, an exact read-only archaeology target in `/Users/zane/Dev/loop`, the eight-column evidence rows, and a real or explicitly blocked canary.
3. **`carry.supervisor` research audit** — `docs/implementation.md` §3.7. Blockers stop the design freeze.
4. **Exact design freeze** — `docs/implementation.md` §4: owners, packages, exact files, deletions, transaction phases, evidence, commands, not-doing. Needing another package, file family, route, table or credential means reopening the contract first.
5. **`carry.supervisor` design audit.**
6. **Implementation** — inside the frozen budget: read the owner, add one failing behavior test, make the smallest vertical change, run formatter/LSP/focused tests, delete the replaced path in the same Node, reread the diff.
7. **Three fresh-context Node-close reviews** — `docs/implementation.md` §6, run in parallel, read-only, blockers only.
8. **`carry.supervisor` diff audit**, then `make check`, one Node-scoped commit, push, terminal CI.

The open research gates in `docs/implementation.md` §8 must be closed with first-party evidence before the Node they guard writes code. Deciding one without evidence is a blocker.

`carry.supervisor` (Claude Opus 5, read-only) is a continuity gate at steps 3, 5 and 8. It never replaces the three independent reviews. Give it the frozen contract, the delta and the changed files only.

One writer owns the worktree. Children do not redesign the route or launch subagents.

## Aesthetics gates

`docs/code-style.md` is binding. The blockers most often hit: condition/error grouping that merges different recoveries, splits one recovery into repeated same-error branches, or intentionally unifies security/privacy failures without explaining that constraint (including a condition hidden behind a helper); a command literal carrying facts the receiver owns or can derive; a function mixing wire parsing, product policy, transaction and network I/O; a transaction without named phases; an abstraction that only forwards; and a test that protects ceremony instead of behavior. A package exists for a fact owner, a concrete process adapter, or an explicitly named composition/transport boundary that owns no product policy; a file is named after one cohesive behavior at that boundary. Split by responsibility and transaction phase, never by line count. Generated code is excluded.

`less is more` means fewer concepts and decisions for the reader, not fewer lines.

## Commands

```text
make check-go
make check-web
make check-product
make check
```

PostgreSQL tests use a real isolated database; a skipped database test is a failure. Go routing uses chi. Cobra lives only in `internal/cli/`. Application SQL goes through sqlc and `make generate`. Web uses the pinned Node version and the `apps/web` lockfile.

Never force-push, rewrite history, reset, clean, or stage unrelated work. Preserve unrelated user changes in the worktree.
