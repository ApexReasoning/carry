import { useEffect, useState } from "react";

import { machines, revokeMachine } from "../../carry-api";
import type { AgentRecord, MachineRecord } from "../../generated/types.gen";

type InventoryState =
  | {
      phase: "loading";
      items: Array<MachineRecord>;
      nextCursor: string | null;
      action: "initial" | "load-more" | "revoke";
    }
  | {
      phase: "loaded";
      items: Array<MachineRecord>;
      nextCursor: string | null;
    }
  | {
      phase: "recoverable-error";
      items: Array<MachineRecord>;
      nextCursor: string | null;
      message: string;
    };

const initialInventory: InventoryState = {
  phase: "loading",
  items: [],
  nextCursor: null,
  action: "initial",
};

export function MachineSettings({
  spaceID,
  spaceName,
  canEnroll,
  onClose,
}: {
  spaceID: string;
  spaceName: string;
  canEnroll: boolean;
  onClose: () => void;
}) {
  const [inventory, setInventory] = useState<InventoryState>(initialInventory);
  const [selected, setSelected] = useState<MachineRecord | null>(null);
  const [commandKey, setCommandKey] = useState("");

  useEffect(() => {
    let active = true;
    machines(spaceID)
      .then((page) => {
        if (active) {
          setInventory({
            phase: "loaded",
            items: page.machines,
            nextCursor: page.next_cursor ?? null,
          });
        }
      })
      .catch(() => {
        if (active) {
          setInventory({
            phase: "recoverable-error",
            items: [],
            nextCursor: null,
            message: "Host and Agent inventory failed.",
          });
        }
      });
    return () => {
      active = false;
    };
  }, [spaceID]);

  const busy = inventory.phase === "loading";

  async function revoke() {
    if (!selected || busy) return;
    const key = commandKey || crypto.randomUUID();
    setCommandKey(key);
    const previous = inventory;
    setInventory({
      phase: "loading",
      items: previous.items,
      nextCursor: previous.nextCursor,
      action: "revoke",
    });
    try {
      await revokeMachine(spaceID, selected.machine_id, key);
      const page = await machines(spaceID);
      setInventory({
        phase: "loaded",
        items: page.machines,
        nextCursor: page.next_cursor ?? null,
      });
      setSelected(null);
      setCommandKey("");
    } catch (caught) {
      setInventory({
        phase: "recoverable-error",
        items: previous.items,
        nextCursor: previous.nextCursor,
        message: message(caught, "Host revocation failed."),
      });
    }
  }

  async function loadMore() {
    if (!inventory.nextCursor || busy) return;
    const previous = inventory;
    setInventory({
      phase: "loading",
      items: previous.items,
      nextCursor: previous.nextCursor,
      action: "load-more",
    });
    try {
      const page = await machines(spaceID, previous.nextCursor ?? undefined);
      setInventory({
        phase: "loaded",
        items: [...previous.items, ...page.machines],
        nextCursor: page.next_cursor ?? null,
      });
    } catch {
      setInventory({
        phase: "recoverable-error",
        items: previous.items,
        nextCursor: previous.nextCursor,
        message: "Host and Agent inventory failed.",
      });
    }
  }

  return (
    <section className="settings-card" aria-labelledby="host-settings-title">
      <div className="settings-heading">
        <div>
          <p className="eyebrow">{spaceName}</p>
          <h2 id="host-settings-title">Hosts and Agents</h2>
        </div>
        <button className="ghost-button" type="button" onClick={onClose}>
          Close
        </button>
      </div>
      <p>
        A Host keeps durable Agent identities online while its foreground Carry
        process is running.
      </p>
      {canEnroll ? (
        <a className="primary-button" href="/machine-connect">
          Add Host
        </a>
      ) : null}
      {inventory.phase === "loading" && inventory.items.length === 0 ? (
        <p>Loading Hosts and Agents…</p>
      ) : null}
      {inventory.phase === "loaded" && inventory.items.length === 0 ? (
        <p>No Hosts have been connected to this Space.</p>
      ) : null}
      <ul className="host-list">
        {inventory.items.map((item) => (
          <li className="host-card" key={item.machine_id}>
            <div className="host-heading">
              <div>
                <strong>{item.display_name}</strong> — {item.state}
                <p>
                  Connected by {item.enrolled_by_name} ·{" "}
                  {new Date(item.enrolled_at).toLocaleString()}
                </p>
                {item.state === "Revoked" ? (
                  <p>
                    Revoked by {item.revocation_actor ?? "Not recorded"}
                    {item.revoked_at
                      ? ` · ${new Date(item.revoked_at).toLocaleString()}`
                      : ""}
                  </p>
                ) : null}
              </div>
              {item.state === "Active" && canEnroll && item.can_revoke ? (
                <button
                  className="danger-button"
                  type="button"
                  disabled={busy}
                  onClick={() => setSelected(item)}
                >
                  Revoke Host
                </button>
              ) : null}
            </div>
            {item.agents.length === 0 ? (
              <p>No supported Agents have been discovered on this Host.</p>
            ) : (
              <ul
                className="agent-list"
                aria-label={`${item.display_name} Agents`}
              >
                {item.agents.map((agent) => (
                  <AgentRow key={agent.agent_id} agent={agent} />
                ))}
              </ul>
            )}
          </li>
        ))}
      </ul>
      {inventory.nextCursor ? (
        <button
          className="ghost-button"
          type="button"
          disabled={busy}
          onClick={() => void loadMore()}
        >
          {inventory.phase === "loading" && inventory.action === "load-more"
            ? "Loading…"
            : "Load more Hosts"}
        </button>
      ) : null}
      {selected ? (
        <div className="confirm-panel" role="dialog" aria-label="Revoke Host">
          <h3>Revoke {selected.display_name}?</h3>
          <p>Space: {selected.space_name}</p>
          {selected.fingerprint ? (
            <p className="machine-fingerprint">
              Public key: {selected.fingerprint}
            </p>
          ) : null}
          <p>
            Every Active Agent on this Host becomes Removed. Their identities
            and history remain, unavailable Work is not reassigned, and this
            certificate can no longer report presence or execute Work.
          </p>
          <p>
            This does not prove a process stopped, delete files from that
            computer, or erase copied data.
          </p>
          <div className="settings-actions">
            <button
              className="ghost-button"
              type="button"
              disabled={busy}
              onClick={() => setSelected(null)}
            >
              Keep Host
            </button>
            <button
              className="danger-button"
              type="button"
              disabled={busy}
              onClick={() => void revoke()}
            >
              {inventory.phase === "loading" && inventory.action === "revoke"
                ? "Revoking…"
                : "Revoke Host"}
            </button>
          </div>
        </div>
      ) : null}
      {inventory.phase === "recoverable-error" ? (
        <p className="alert" role="alert">
          {inventory.message}
        </p>
      ) : null}
    </section>
  );
}

function AgentRow({ agent }: { agent: AgentRecord }) {
  const initials = agent.name
    .split(/\s+/)
    .slice(0, 2)
    .map((part) => part.slice(0, 1))
    .join("")
    .toUpperCase();
  return (
    <li className="agent-row">
      <span
        className={`agent-avatar agent-avatar-${agent.avatar_index}`}
        aria-hidden="true"
      >
        {initials}
      </span>
      <div>
        <strong>{agent.name}</strong>
        <p>Owned by {agent.owner_name}</p>
        <p>
          {agent.state === "active" ? "Active" : "Removed"} ·{" "}
          {agent.online ? "Online" : "Offline"} ·{" "}
          {agent.last_active_at
            ? `Last active ${new Date(agent.last_active_at).toLocaleString()}`
            : "Never active"}
        </p>
      </div>
    </li>
  );
}

function message(value: unknown, fallback: string) {
  return value instanceof Error ? value.message : fallback;
}
