import { useEffect, useState } from "react";

import { machines, revokeMachine } from "../../carry-api";
import type { MachineRecord } from "../../generated/types.gen";

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
  const [items, setItems] = useState<Array<MachineRecord>>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<MachineRecord | null>(null);
  const [commandKey, setCommandKey] = useState("");
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    machines(spaceID)
      .then((page) => {
        if (active) {
          setItems(page.machines);
          setNextCursor(page.next_cursor ?? null);
        }
      })
      .catch((caught: unknown) => {
        if (active) setError(message(caught));
      })
      .finally(() => {
        if (active) setBusy(false);
      });
    return () => {
      active = false;
    };
  }, [spaceID]);

  async function revoke() {
    if (!selected) return;
    const key = commandKey || crypto.randomUUID();
    setCommandKey(key);
    setBusy(true);
    setError(null);
    try {
      await revokeMachine(spaceID, selected.machine_id, key);
      const page = await machines(spaceID);
      setItems(page.machines);
      setNextCursor(page.next_cursor ?? null);
      setSelected(null);
      setCommandKey("");
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function loadMore() {
    if (!nextCursor) return;
    setBusy(true);
    setError(null);
    try {
      const page = await machines(spaceID, nextCursor);
      setItems((current) => [...current, ...page.machines]);
      setNextCursor(page.next_cursor ?? null);
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="settings-card" aria-labelledby="machine-settings-title">
      <div className="settings-heading">
        <div>
          <p className="eyebrow">{spaceName}</p>
          <h2 id="machine-settings-title">Machines</h2>
        </div>
        <button className="ghost-button" type="button" onClick={onClose}>
          Close
        </button>
      </div>
      <p>
        Active means Carry has not revoked this Machine. It does not mean the
        computer or Host process is online.
      </p>
      {busy && items.length === 0 ? <p>Loading Machines…</p> : null}
      {items.length === 0 && !busy ? (
        <p>No Machines have been connected to this Space.</p>
      ) : null}
      <ul className="settings-list machine-list">
        {items.map((item) => (
          <li key={item.machine_id}>
            <div>
              <strong>{item.display_name}</strong> — {item.state}
              <p>Space: {item.space_name}</p>
              <p>
                Enrolled by {item.enrolled_by_name} ·{" "}
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
                Revoke
              </button>
            ) : null}
          </li>
        ))}
      </ul>
      {nextCursor ? (
        <button
          className="ghost-button"
          type="button"
          disabled={busy}
          onClick={() => void loadMore()}
        >
          {busy ? "Loading…" : "Load more Machines"}
        </button>
      ) : null}
      {selected ? (
        <div
          className="confirm-panel"
          role="dialog"
          aria-label="Revoke Machine"
        >
          <h3>Revoke {selected.display_name}?</h3>
          <p>Space: {selected.space_name}</p>
          {selected.fingerprint ? (
            <p className="machine-fingerprint">
              Public key: {selected.fingerprint}
            </p>
          ) : null}
          <p>
            Carry will reject future claims, renewals, commits, and finishes
            from this Machine certificate. This does not prove a process
            stopped, delete files from that computer, or erase copied data.
          </p>
          <div className="settings-actions">
            <button
              className="ghost-button"
              type="button"
              disabled={busy}
              onClick={() => setSelected(null)}
            >
              Keep Machine
            </button>
            <button
              className="danger-button"
              type="button"
              disabled={busy}
              onClick={() => void revoke()}
            >
              {busy ? "Revoking…" : "Revoke Machine"}
            </button>
          </div>
        </div>
      ) : null}
      {error ? (
        <p className="alert" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}

function message(value: unknown) {
  return value instanceof Error ? value.message : "Machine inventory failed";
}
