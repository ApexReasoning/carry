# Carry

Carry is an AI teammate. A person hands a responsibility over in carry.ai Web, a named Agent owns keeping it moving, and the Work stays true across Conversations, Agent sessions, Hosts, channels and time.

People use the Web product; sign-in is GitHub, Google or Email. A Space holds members, Hosts, Agents, Conversations and Work. The `carry` executable is the long-running Host, the shallow operator commands that attach a Host to a Space, and an Agent-facing interface — never a parallel human product. An Agent is a durable, named, user-visible identity bound to one Host; it never holds member credentials or a direct path to Carry Server.

Every Work has exactly one human owner and exactly one Agent owner, and Work is always created through an Agent. Carry Server never pushes work: a record names its target Agent and the owning Host pulls and claims it through PostgreSQL. Host or Agent loss never deletes Work or silently chooses a replacement; the human owner explicitly transfers future responsibility, while old execution authority and provider-private state do not cross the handoff.

Nodes 0–12 are technical evidence for an older contract; the old Node 13–19 route is void. The roadmap reset is closed, and the active replacement route (Nodes 13–30), its research procedure and its three-review protocol live in [`docs/implementation.md`](docs/implementation.md).

Start with [AGENTS.md](AGENTS.md), then the document that owns your change:

- [`docs/product.md`](docs/product.md) — screens, actions, vocabulary, privacy, invariants
- [`docs/architecture.md`](docs/architecture.md) — owners, topology, authority, concurrency, package direction
- [`docs/code-style.md`](docs/code-style.md) — aesthetics gates
- [`docs/repository.md`](docs/repository.md) — paths, generation, commands, CI, deletion
- [`docs/implementation.md`](docs/implementation.md) — active route

Each Node's journey freeze, research, evidence and design freeze live in that Node's GitHub Issue. There are no plan, review, audit, handoff or evidence directories; review conclusions stay in the PR and CI evidence stays in CI artifacts.

```bash
make build
make test
make check
```

The existing foundation (Web identity and Space, durable Work, Machine connection, fenced Pi/Codex execution, private Conversation) is evidence, not a requirement. The route deletes any UI, CLI, API, schema, query, test or package that contradicts the complete journey.
