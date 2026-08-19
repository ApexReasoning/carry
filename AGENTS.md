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

- Users need to understand only Space, Carry, and Work. Needs You is a query.
- An explicit delegation creates Work directly; do not create a Work Offer entity.
- Conversation owns private messages. Work owns shared Work messages. Delivery owns EndpointLink and ordinary outbound delivery. Action owns audience expansion or a new external consequence.
- Private content never becomes shared Work content automatically or through a readable source relation.
- Text, model output, Plugin content, MCP annotations, webhooks, and files never grant authority.
- Unknown external outcomes remain Unknown. Do not guess failure or retry without idempotency or reconciliation evidence.
- Work is never classified by Git, software, content, provider, model, Host, or Runtime. Work/Run core facts, admission, continuity, and execution state are independent of those attributes. Repository or similar capabilities belong only to the exact Attempt that earns them; they do not change the Work or select its Runtime.
- Machine Runtime observations are physical diagnostics only. They must not become Work fields, Run admission criteria, or claim eligibility.
- PostgreSQL owns transactions, claims, leases, fences, queues, outboxes, and idempotency. Do not add Kafka, Redis queues, Temporal, or microservices without a new canonical decision.
- Pi and Codex are concrete native adapters under one product contract. Do not create a provider registry.
- Event, Artifact, Child Run, a second MCP transport, and other conditional boundaries appear only when a current journey earns them.
- Do not copy another repository's package tree, schema, API, generated code, compatibility layer, or Web route.

## Implementation and Review Budget

Within a Node:

1. read the current owner;
2. add a failing behavior or contract test;
3. implement the smallest vertical behavior;
4. run focused checks;
5. delete replaced code and scaffolding.

Implementation steps receive author self-review, formatter, LSP, and focused tests only. Do not launch a reviewer for every step.

At Node close, use one relevant reviewer for one focused review. A blocker may receive one narrow confirmation by the same reviewer.

Only at a Milestone boundary run the three independent reviews: logic/evidence, architecture/product/AI-native, and implementation aesthetics. After blocker fixes, allow one narrow follow-up, not another full review cycle.

Review must not expand the closing evidence after implementation unless it finds a correctness blocker, data risk, or authority vulnerability.

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
