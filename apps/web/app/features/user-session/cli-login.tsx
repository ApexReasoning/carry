import { useEffect, useState } from "react";

import {
  approveCliLogin,
  cliCredentials,
  denyCliLogin,
  lookupCliLogin,
  MutationOutcomeUnknownError,
} from "../../carry-api";
import type {
  CliCredential,
  CliLoginPreview,
  User,
} from "../../generated/types.gen";

type PendingDecision =
  | { kind: "approve"; key: string; spaceID: string; replacement?: string }
  | { kind: "deny"; key: string };

export function CliLoginPage({ user }: { user: User }) {
  const [code, setCode] = useState("");
  const [preview, setPreview] = useState<CliLoginPreview | null>(null);
  const [credentials, setCredentials] = useState<Array<CliCredential>>([]);
  const [spaceID, setSpaceID] = useState("");
  const [replacement, setReplacement] = useState("");
  const [pending, setPending] = useState<PendingDecision | null>(null);
  const [outcome, setOutcome] = useState<"approved" | "denied" | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    void cliCredentials()
      .then((items) => {
        if (active) setCredentials(items);
      })
      .catch((caught: unknown) => {
        if (active) setError(message(caught));
      });
    return () => {
      active = false;
    };
  }, []);

  async function find(event: React.SyntheticEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const found = await lookupCliLogin(normalizeCode(code));
      setPreview(found);
      setCode(found.user_code);
      const proposed = credentials.find(
        (item) =>
          item.credential_id === found.proposed_replacement_credential_id,
      );
      setReplacement(proposed?.credential_id ?? "");
      setSpaceID(found.approved_space_id ?? user.spaces[0]?.space_id ?? "");
      if (found.approved) setOutcome("approved");
      else if (found.denied) setOutcome("denied");
      else if (found.cancelled || found.redeemed)
        setError("This CLI login request is no longer available.");
    } catch (caught) {
      setPreview(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function decide(command: PendingDecision) {
    if (!preview) return;
    setPending(command);
    setBusy(true);
    setError(null);
    try {
      if (command.kind === "approve") {
        await approveCliLogin(
          preview,
          command.spaceID,
          command.replacement,
          command.key,
        );
        setOutcome("approved");
      } else {
        await denyCliLogin(preview, command.key);
        setOutcome("denied");
      }
      setPending(null);
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError)) setPending(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="center-state cli-login-page">
      <a className="brand" href="/" aria-label="Carry home">
        Carry<span className="brand-dot">.</span>
      </a>
      <section className="identity-settings" aria-labelledby="cli-login-title">
        <div className="identity-settings-heading">
          <div>
            <p className="eyebrow">Command line</p>
            <h1 id="cli-login-title">Approve a CLI login</h1>
          </div>
        </div>
        {!preview ? (
          <form
            className="identity-code-form"
            onSubmit={(event) => void find(event)}
          >
            <label htmlFor="cli-code">Code shown by carry login</label>
            <input
              id="cli-code"
              value={code}
              onChange={(event) => setCode(event.target.value.toUpperCase())}
              placeholder="BCDF-GHJ-KLM"
              autoComplete="off"
              required
            />
            <p>
              Only continue for a command you started. Carry never puts this
              code or a credential in this page URL.
            </p>
            <button className="primary-button" disabled={busy || !code.trim()}>
              {busy ? "Checking…" : "Review login"}
            </button>
          </form>
        ) : (
          <div className="cli-login-review">
            <dl>
              <div>
                <dt>Server</dt>
                <dd>{preview.server}</dd>
              </div>
              <div>
                <dt>Code</dt>
                <dd>
                  <strong>{preview.user_code}</strong>
                </dd>
              </div>
              <div>
                <dt>CLI label</dt>
                <dd>{preview.label}</dd>
              </div>
              <div>
                <dt>Signed in User</dt>
                <dd>{user.display_name}</dd>
              </div>
              <div>
                <dt>Expires</dt>
                <dd>{new Date(preview.expires_at).toLocaleString()}</dd>
              </div>
            </dl>
            <p>
              Approval identifies this User to the CLI. It does not add a
              Membership, enroll a Machine, or grant execution authority. Every
              command still uses your current Membership.
            </p>
            {outcome ? (
              <p className="success" role="status">
                {outcome === "approved"
                  ? "Approved. Return to the terminal while it redeems the credential."
                  : "Denied. The terminal will not receive a credential."}
              </p>
            ) : (
              <>
                <label>
                  Default Space to inspect
                  <select
                    value={spaceID}
                    onChange={(event) => setSpaceID(event.target.value)}
                    disabled={busy}
                    required
                  >
                    <option value="">Choose a current Space</option>
                    {user.spaces.map((space) => (
                      <option key={space.space_id} value={space.space_id}>
                        {space.name}
                      </option>
                    ))}
                  </select>
                </label>
                {credentials.length > 0 ? (
                  <label>
                    Replace one existing CLI access (optional)
                    <select
                      value={replacement}
                      onChange={(event) => setReplacement(event.target.value)}
                      disabled={busy}
                    >
                      <option value="">Create separate CLI access</option>
                      {credentials.map((credential) => (
                        <option
                          key={credential.credential_id}
                          value={credential.credential_id}
                        >
                          {credential.label} — {credential.approved_space_name}
                        </option>
                      ))}
                    </select>
                  </label>
                ) : null}
                <div className="identity-method-actions">
                  <button
                    className="primary-button"
                    type="button"
                    disabled={busy || !spaceID}
                    onClick={() =>
                      void decide({
                        kind: "approve",
                        key: crypto.randomUUID(),
                        spaceID,
                        replacement: replacement || undefined,
                      })
                    }
                  >
                    Approve this CLI login
                  </button>
                  <button
                    className="ghost-button"
                    type="button"
                    disabled={busy}
                    onClick={() =>
                      void decide({ kind: "deny", key: crypto.randomUUID() })
                    }
                  >
                    Deny
                  </button>
                </div>
              </>
            )}
          </div>
        )}
        {pending ? (
          <button
            className="ghost-button"
            type="button"
            disabled={busy}
            onClick={() => void decide(pending)}
          >
            Retry exact decision
          </button>
        ) : null}
        {error ? (
          <p className="alert" role="alert">
            {error}
          </p>
        ) : null}
      </section>
    </main>
  );
}

function normalizeCode(value: string): string {
  const significant = value.toUpperCase().replaceAll(/[-\s]/g, "");
  if (significant.length !== 10) return value.trim().toUpperCase();
  return `${significant.slice(0, 4)}-${significant.slice(4, 7)}-${significant.slice(7)}`;
}

function message(value: unknown): string {
  return value instanceof Error ? value.message : "CLI login failed";
}
