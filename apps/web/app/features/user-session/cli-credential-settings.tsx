import { useEffect, useState } from "react";

import {
  cliCredentials,
  MutationOutcomeUnknownError,
  revokeCliCredential,
} from "../../carry-api";
import type { CliCredential } from "../../generated/types.gen";

export function CliCredentialSettings({ onClose }: { onClose: () => void }) {
  const [credentials, setCredentials] = useState<Array<CliCredential>>([]);
  const [pending, setPending] = useState<{
    credential: CliCredential;
    key: string;
  } | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    void cliCredentials()
      .then((items) => {
        if (active) setCredentials(items);
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
  }, []);

  async function revoke(command: { credential: CliCredential; key: string }) {
    setPending(command);
    setBusy(true);
    setError(null);
    try {
      await revokeCliCredential(command.credential.credential_id, command.key);
      setCredentials((items) =>
        items.filter(
          (item) => item.credential_id !== command.credential.credential_id,
        ),
      );
      setPending(null);
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError)) setPending(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section
      className="identity-settings"
      aria-labelledby="cli-credential-settings-title"
    >
      <div className="identity-settings-heading">
        <div>
          <p className="eyebrow">Settings</p>
          <h2 id="cli-credential-settings-title">CLI access</h2>
        </div>
        <button className="ghost-button" type="button" onClick={onClose}>
          Close
        </button>
      </div>
      <p>
        Each entry identifies you to one Carry server for 90 days. Space access
        still follows your current Membership. Revocation does not enroll,
        revoke, or stop a Machine.
      </p>
      {error ? (
        <p className="alert" role="alert">
          {error}
        </p>
      ) : null}
      {credentials.length === 0 && !busy ? <p>No active CLI access.</p> : null}
      <ul className="identity-method-list">
        {credentials.map((credential) => (
          <li key={credential.credential_id}>
            <div>
              <strong>{credential.label}</strong>
              <span>
                Default context:{" "}
                {credential.approved_space_name || "No longer available"}
              </span>
              <span>
                Expires {new Date(credential.expires_at).toLocaleString()}
              </span>
            </div>
            <button
              className="ghost-button"
              type="button"
              disabled={busy}
              onClick={() =>
                void revoke({ credential, key: crypto.randomUUID() })
              }
            >
              Revoke CLI access
            </button>
          </li>
        ))}
      </ul>
      {pending ? (
        <button
          className="ghost-button"
          type="button"
          disabled={busy}
          onClick={() => void revoke(pending)}
        >
          Retry exact revocation
        </button>
      ) : null}
    </section>
  );
}

function message(value: unknown): string {
  return value instanceof Error ? value.message : "CLI access request failed";
}
