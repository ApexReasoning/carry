import { useMemo, useState } from "react";

import {
  approveMachineConnection,
  denyMachineConnection,
  lookupMachineConnection,
} from "../../carry-api";
import type { MachineConnectionPreview, User } from "../../generated/types.gen";

type Decision = {
  key: string;
  kind: "approve" | "deny";
};

export function MachineConnectPage({ user }: { user: User }) {
  const [code, setCode] = useState("");
  const [preview, setPreview] = useState<MachineConnectionPreview | null>(null);
  const [spaceID, setSpaceID] = useState("");
  const [decision, setDecision] = useState<Decision | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [complete, setComplete] = useState<"approved" | "denied" | null>(null);

  const eligibleSpaces = useMemo(
    () => user.spaces.filter((space) => space.can_enroll_machines),
    [user.spaces],
  );

  async function review() {
    setBusy(true);
    setError(null);
    try {
      const found = await lookupMachineConnection(code);
      setPreview(found);
      const current = eligibleSpaces.find(
        (space) => space.space_id === found.approved_space_id,
      );
      setSpaceID(current?.space_id ?? eligibleSpaces[0]?.space_id ?? "");
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function decide(kind: "approve" | "deny") {
    if (!preview) return;
    if (kind === "approve" && !spaceID) {
      setError("Choose a Space where you may connect Hosts.");
      return;
    }
    const command =
      decision?.kind === kind ? decision : { kind, key: crypto.randomUUID() };
    setDecision(command);
    setBusy(true);
    setError(null);
    try {
      if (kind === "approve") {
        await approveMachineConnection(preview, spaceID, command.key);
      } else {
        await denyMachineConnection(preview, command.key);
      }
      setComplete(kind === "approve" ? "approved" : "denied");
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  if (complete) {
    return (
      <main className="center-state machine-connect-page">
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
        <h1>{complete === "approved" ? "Host approved" : "Host denied"}</h1>
        <p className="center-state-copy">
          {complete === "approved"
            ? "Return to the terminal. The same carry setup command will install this Host and start Agent reporting."
            : "Return to the terminal. No Host certificate will be issued."}
        </p>
      </main>
    );
  }

  return (
    <main className="center-state machine-connect-page">
      <p className="brand-mark">
        Carry<span className="brand-dot">.</span>
      </p>
      <h1>Connect a Host</h1>
      {!preview ? (
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void review();
          }}
        >
          <label>
            Code shown by carry setup
            <input
              value={code}
              onChange={(event) => setCode(event.target.value)}
              autoComplete="off"
              spellCheck={false}
              disabled={busy}
            />
          </label>
          <button type="submit" disabled={busy || code.trim() === ""}>
            {busy ? "Checking…" : "Review Host"}
          </button>
        </form>
      ) : (
        <section className="settings-card machine-approval">
          <p className="eyebrow">Check the terminal before approving</p>
          <dl>
            <dt>Server</dt>
            <dd>{preview.server}</dd>
            <dt>Code</dt>
            <dd>{preview.user_code}</dd>
            <dt>Host name</dt>
            <dd>{preview.display_name}</dd>
            <dt>Public key</dt>
            <dd className="machine-fingerprint">{preview.fingerprint}</dd>
          </dl>
          <label>
            Space
            <select
              value={spaceID}
              onChange={(event) => setSpaceID(event.target.value)}
              disabled={busy}
            >
              {eligibleSpaces.length === 0 ? (
                <option value="">No eligible Space</option>
              ) : null}
              {eligibleSpaces.map((space) => (
                <option key={space.space_id} value={space.space_id}>
                  {space.name}
                </option>
              ))}
            </select>
          </label>
          <p>
            This Host may report Agents and execute Work for the selected Space.
            The full key, server, name, and code must match the terminal.
          </p>
          <div className="settings-actions">
            <button
              className="ghost-button"
              type="button"
              disabled={busy}
              onClick={() => void decide("deny")}
            >
              Deny
            </button>
            <button
              type="button"
              disabled={busy || !spaceID}
              onClick={() => void decide("approve")}
            >
              {busy ? "Saving decision…" : "Connect Host"}
            </button>
          </div>
        </section>
      )}
      {error ? (
        <p className="alert" role="alert">
          {error}
        </p>
      ) : null}
    </main>
  );
}

function message(value: unknown) {
  return value instanceof Error ? value.message : "Machine connection failed";
}
