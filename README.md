# Carry

Carry is an AI teammate that keeps responsibility alive across conversations, agents, tools, machines, and time.

The repository is under active V1 construction. Start with [AGENTS.md](AGENTS.md) and the canonical documents in [`docs/`](docs/).

## Current development commands

```bash
make build
make test
make check
```

The current V1 foundation supports member login, bounded durable Work, independent Machine enrollment, fenced Pi/Codex execution and recovery, and private member Conversations that can directly create shared Work. Node 10 adds an optional bounded read-only Reference Catalog for Host execution; configure its fixed HTTPS base URL with `CARRY_REFERENCE_BASE_URL`. Product behavior is added through the vertical Nodes in [`docs/implementation.md`](docs/implementation.md); unearned future owners and protocols are intentionally absent.
