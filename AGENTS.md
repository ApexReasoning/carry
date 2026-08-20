# Carry Agent Contract

This file is the execution entry point for coding agents. It does not replace the canonical design documents; it tells agents which documents to read and how to turn them into bounded work.

## Sources of Truth

Before a material change, read the relevant sections of:

- `docs/product.md` for user language, journeys, privacy, and product invariants;
- `docs/architecture.md` for fact owners, transactions, authority, and dependency direction;
- `docs/code-style.md` for Go, TypeScript, React, SQL, naming, and test quality;
- `docs/repository.md` for paths, commands, generated code, CI, experiments, and deletion rules;
- `docs/implementation.md` for the current V1 Node, sequence, review budget, and closing evidence.

Do not silently choose between conflicting instructions. Stop, identify the conflict, and ask for a decision. An explicit current user decision overrides older text; update the affected canonical document before implementing the conflicting design.

## Four Operating Principles

These principles turn Carry's restraint and freedom philosophy into coding behavior:

1. **Surface uncertainty before code.** Read the owning facts first. State assumptions and materially different interpretations instead of choosing silently. Ask only when the answer changes the user journey, owner, authority, consequence, or approved scope; make routine engineering judgments without pushing them back to the user.
2. **Build only earned structure.** Implement the smallest complete structure required by the current journey. Do not add speculative flexibility, but retain every type, transaction, failure path, and test needed to protect truth. Line count is not a simplicity metric.
3. **Keep scope constrained and the vertical change complete.** Do not make unrelated improvements. Within the approved behavior, include every necessary owner, migration, protocol, generated artifact, test, and document, and delete the path it replaces. A small diff that leaves two truths is not surgical.
4. **Define completion with evidence.** Before implementation, name success, failure, authority/concurrency, and user-journey evidence. Loop until the required evidence is observed; a green command alone does not prove that the changed path executed or that the product promise is true.

At every Node entry, include read-only archaeology of the user-designated historical implementation alongside the five external primary-source comparisons. Cite exact files and symbols, extract strengths and failure modes, and re-derive the Carry design from the current journey. Never copy its package tree, schema, API, migrations, generated code, compatibility paths, or Web routes. Nodes 0 and 1 must receive this archaeology before M0 closes.

## Node Contract Before Code

Before writing production code, identify the current Node and state a short contract in the conversation, Issue, or PR. Do not create another plan file.

```text
Node:
User journey:
Fact owners:
Allowed paths:
Not doing:
Success evidence:
Failure evidence:
Authority/concurrency evidence:
Focused commands:
Milestone:
```

The contract is set after Node research and design, before implementation. If implementation needs a new owner, root directory, public protocol, or materially different journey, pause and update the contract and closing evidence first.

## Repository Skeleton and Boundaries

- Start from the buildable skeleton established by Node 0 and the target shape in `docs/repository.md`.
- Create only packages required by the current Node. Do not create empty future directories.
- Domain packages must not import server, PostgreSQL, connector, or Agent process adapters.
- `carry-server` must not import local Pi or Codex process implementations.
- Do not create `common`, `utils`, `platform`, `integration`, `registry`, `resource`, `runtime`, `orchestrator`, `readmodel`, or similarly generic ownership buckets.
- Do not maintain a central allowlist of every package. Boundary checks express stable forbidden directions.

## Product and Architecture Guardrails

`docs/product.md` and `docs/architecture.md` own the full rules. This entry point repeats only the boundaries an implementation agent must not cross silently:

- users understand Space, Carry, and Work; current persistent owners are Identity, Space, Conversation, Work, Machine, and Run;
- explicit delegation creates Work directly; private Conversation content never becomes shared Work automatically or through a readable source relation;
- content and model output never grant authority, and Unknown is never guessed into failure or success;
- Work and Run are independent of Git, provider, model, Host and Runtime; Pi and Codex remain concrete native adapters, not a registry;
- PostgreSQL owns transactions, idempotency, claims, leases and fences; Machine mTLS plus the exact current claim facts grants execution authority;
- a new owner, credential, protocol audience or external consequence requires a current journey and an updated canonical decision before code.

## Implementation and Review Budget

Within a Node:

1. read the current owner;
2. add a failing behavior or contract test;
3. implement the smallest vertical behavior;
4. run focused checks;
5. delete replaced code and scaffolding.

Implementation steps receive author self-review, formatter, LSP, and focused tests only. Do not launch a reviewer for every step.

Every Node close requires three independent fresh-context reviews:

1. **Logic/evidence:** verify the frozen user journey, success and failure behavior, PostgreSQL concurrency/authority evidence, and direct proof that the changed path executed. Keep uncertainty explicit; never infer success, failure, retry safety, or completion from a green command alone.
2. **Architecture/product/AI-native:** apply **responsibility fixed, path free**. Verify one authoritative owner for identity, authority, causality, time, privacy, and external outcome; reject content-derived authority and unearned structure; preserve natural-language, concrete-adapter, and execution freedom inside those boundaries.
3. **Implementation aesthetics:** verify accurate names and file ownership, a linear main path, restrained dependencies, deletion of replaced/scaffold code, and vertical completeness. Fewer lines are not simpler when they weaken truth, authority, failure handling, or evidence.

All three gates apply the four operating principles: surface uncertainty before code; build only earned structure; keep scope constrained while completing the vertical journey; and define completion with direct evidence. Reviewers receive the same frozen Node contract and inspect only their assigned gate. They report blockers and high-value deletion opportunities, not speculative future scope.

After blocker fixes, the reviewer who found the blocker may perform one narrow confirmation. Do not rerun the full three-review fleet unless fixes materially cross multiple gates. A Milestone boundary runs the same three gates over the complete cumulative Milestone diff and journeys, rather than substituting for Node-close review.

Review must not expand the frozen closing evidence unless it finds a correctness blocker, data risk, authority/privacy vulnerability, or a direct violation of the current journey or architecture philosophy.

## Delegated Agents

- Launch every child with the repository root as its working directory so project instructions are discovered.
- The task must explicitly say: read `AGENTS.md`, name the current Node, and obey its contract.
- Give one child one bounded role. Do not let ordinary implementation children redesign the roadmap or launch further review fleets.
- Fresh-context reviewers receive the frozen Node contract and report only blockers in their assigned gate.

## Verification

Use repository commands as the stable interface when they exist:

```text
make check-go
make check-web
make check-product
make check
```

- `check-*` targets are read-only; they must not format or generate before passing.
- PostgreSQL focused tests use a real isolated database. A missing database or skipped test is not a pass.
- Go HTTP routing uses chi; do not mix routers or add a controller framework.
- The nested `carry` CLI uses Cobra only inside `internal/cli/`; construct commands explicitly without globals, `init()` registration, registries, or a universal dependency factory. Keep `carry-server` on standard `flag` while its operator surface remains shallow.
- PostgreSQL application queries use sqlc and generated code stays in `internal/postgres/dbsqlc/`.
- Run sqlc only through `make generate`; do not hand-edit generated files.
- Web uses the repository-pinned Node version and pnpm lockfile.
- Local and CI checks must invoke the same Make targets.

## Documentation and Git Hygiene

- Keep current design in the five documents listed above. Node research and review details belong in Issues or PRs.
- Do not add `docs/plans`, `docs/reviews`, `docs/handoffs`, `docs/audits`, or evidence archives.
- Delete experiments, scripts, dependencies, generated artifacts, and docs when their consumer or decision disappears.
- At Node close, after required checks and reviews pass, inspect the complete diff, exclude unrelated files, create one ordinary Node-scoped commit, and push the current branch before stopping for user feedback.
- Do not force-push, rewrite history, reset, clean, or stage unrelated work unless the user explicitly asks.
