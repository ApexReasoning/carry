import { type SubmitEvent, useEffect, useState } from "react";

import {
  APIResponseError,
  identityMethods,
  MutationOutcomeUnknownError,
  requestIdentityEmailCode,
  unlinkIdentityMethod,
  verifyIdentityEmailCode,
} from "../../carry-api";
import type { IdentityMethods } from "../../generated/types.gen";

type Method = IdentityMethods["methods"][number];
type EmailProof = {
  purpose: "reauthenticate" | "link";
  challengeID: string;
  requestKey: string;
  verifyKey: string;
  candidateEmail?: string;
};
type PendingUnlink = { method: Method; requestKey: string };

const methodLabels: Record<Method, string> = {
  email: "Email",
  google: "Google",
  github: "GitHub",
};

const outcomeMessages: Record<string, { message: string; failure: boolean }> = {
  linked: {
    message: "Sign-in method linked. Other browsers were signed out.",
    failure: false,
  },
  confirmed: {
    message: "Sign-in method confirmed.",
    failure: false,
  },
  link_failed: {
    message:
      "Carry could not link this sign-in method. Your existing methods were not changed.",
    failure: true,
  },
  link_cancelled: {
    message: "Linking was cancelled. Your existing methods were not changed.",
    failure: true,
  },
  link_unavailable: {
    message:
      "Carry could not confirm whether linking completed. Check the methods below before trying again.",
    failure: true,
  },
  confirmation_failed: {
    message:
      "Carry could not confirm this sign-in method. Your existing methods were not changed.",
    failure: true,
  },
  confirmation_cancelled: {
    message:
      "Confirmation was cancelled. Your existing methods were not changed.",
    failure: true,
  },
  confirmation_unavailable: {
    message:
      "Carry could not confirm whether confirmation completed. Check the methods below before trying again.",
    failure: true,
  },
};

export function IdentityMethodSettings({ onClose }: { onClose: () => void }) {
  const [outcome] = useState(takeIdentityChangeStatus);
  const [state, setState] = useState<IdentityMethods | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [emailProof, setEmailProof] = useState<EmailProof | null>(null);
  const [pendingUnlink, setPendingUnlink] = useState<PendingUnlink | null>(
    null,
  );
  const [removalToConfirm, setRemovalToConfirm] = useState<Method | null>(null);

  useEffect(() => {
    void refresh();
  }, []);

  async function refresh() {
    setBusy(true);
    setError(null);
    try {
      setState(await identityMethods());
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }

  async function requestEmailProof(
    event: SubmitEvent<HTMLFormElement> | null,
    purpose: "reauthenticate" | "link",
    exactReplay?: EmailProof,
  ) {
    event?.preventDefault();
    const proof =
      exactReplay ??
      ({
        purpose,
        challengeID: crypto.randomUUID(),
        requestKey: crypto.randomUUID(),
        verifyKey: crypto.randomUUID(),
        candidateEmail: purpose === "link" ? email : undefined,
      } satisfies EmailProof);
    setEmailProof(proof);
    setBusy(true);
    setError(null);
    try {
      await requestIdentityEmailCode(
        proof.purpose,
        proof.challengeID,
        proof.requestKey,
        proof.candidateEmail,
      );
      setCode("");
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError)) {
        setEmailProof(null);
      }
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }

  async function verifyEmailProof(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!emailProof || !/^\d{6}$/.test(code)) return;
    setBusy(true);
    setError(null);
    try {
      await verifyIdentityEmailCode(
        emailProof.purpose,
        emailProof.challengeID,
        code,
        emailProof.verifyKey,
      );
      setEmailProof(null);
      setRemovalToConfirm(null);
      setEmail("");
      setCode("");
      await refresh();
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }

  async function unlink(method: Method, command?: PendingUnlink) {
    const pending = command ?? { method, requestKey: crypto.randomUUID() };
    setPendingUnlink(pending);
    setBusy(true);
    setError(null);
    try {
      await unlinkIdentityMethod(pending.method, pending.requestKey);
      setPendingUnlink(null);
      setRemovalToConfirm(null);
      await refresh();
    } catch (caught) {
      if (caught instanceof APIResponseError && caught.status === 428) {
        setRemovalToConfirm(pending.method);
      }
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
    }
  }

  const linked = new Set(state?.methods ?? []);
  return (
    <section
      className="identity-settings"
      aria-labelledby="identity-settings-title"
    >
      <div className="identity-settings-heading">
        <div>
          <p className="eyebrow">Settings</p>
          <h2 id="identity-settings-title">Sign-in methods</h2>
        </div>
        <button className="ghost-button" type="button" onClick={onClose}>
          Close
        </button>
      </div>
      <p>
        Link another way to return to the same Carry User and Work. Carry never
        combines accounts just because their email looks the same.
      </p>
      {outcome && outcomeMessages[outcome] ? (
        <p
          className={outcomeMessages[outcome].failure ? "alert" : undefined}
          role={outcomeMessages[outcome].failure ? "alert" : "status"}
        >
          {outcomeMessages[outcome].message}
        </p>
      ) : null}
      {error ? (
        <p className="alert" role="alert">
          {error}
        </p>
      ) : null}
      {!state ? <p aria-live="polite">Loading sign-in methods…</p> : null}
      {state?.reauthentication_required || removalToConfirm ? (
        <div className="identity-confirmation">
          <h3>Confirm another linked method</h3>
          <p>
            {removalToConfirm
              ? `Confirm a method other than ${methodLabels[removalToConfirm]} before removing it.`
              : "Recently confirm any linked method before changing access."}{" "}
            This does not claim MFA or that a provider asked for a password
            again.
          </p>
          <div className="identity-method-actions">
            {linked.has("email") && removalToConfirm !== "email" ? (
              <form
                onSubmit={(event) => requestEmailProof(event, "reauthenticate")}
              >
                <button
                  className="secondary-button"
                  type="submit"
                  disabled={busy}
                >
                  Confirm with Email
                </button>
              </form>
            ) : null}
            {linked.has("google") && removalToConfirm !== "google" ? (
              <form
                method="post"
                action="/v1/identity/reauthentication/google/start"
              >
                <button
                  className="secondary-button"
                  type="submit"
                  disabled={busy}
                >
                  Confirm with Google
                </button>
              </form>
            ) : null}
            {linked.has("github") && removalToConfirm !== "github" ? (
              <form
                method="post"
                action="/v1/identity/reauthentication/github/start"
              >
                <button
                  className="secondary-button"
                  type="submit"
                  disabled={busy}
                >
                  Confirm with GitHub
                </button>
              </form>
            ) : null}
          </div>
        </div>
      ) : null}
      {emailProof ? (
        <form className="identity-code-form" onSubmit={verifyEmailProof}>
          <label htmlFor="identity-email-code">
            Newest six-digit email code
          </label>
          <input
            id="identity-email-code"
            inputMode="numeric"
            autoComplete="one-time-code"
            value={code}
            onChange={(event) =>
              setCode(event.target.value.replace(/\D/g, "").slice(0, 6))
            }
            maxLength={6}
          />
          <button
            className="primary-button"
            type="submit"
            disabled={busy || !/^\d{6}$/.test(code)}
          >
            {emailProof.purpose === "link" ? "Link Email" : "Confirm Email"}
          </button>
          <button
            className="ghost-button"
            type="button"
            disabled={busy}
            onClick={() =>
              void requestEmailProof(null, emailProof.purpose, emailProof)
            }
          >
            Retry sending this code
          </button>
        </form>
      ) : null}
      <ul className="identity-method-list">
        {(["email", "google", "github"] as const).map((method) => (
          <li key={method}>
            <div>
              <strong>{methodLabels[method]}</strong>
              <span>{linked.has(method) ? "Linked" : "Not linked"}</span>
            </div>
            {linked.has(method) ? (
              <button
                className="ghost-button"
                type="button"
                disabled={
                  busy ||
                  state?.methods.length === 1 ||
                  state?.reauthentication_required
                }
                onClick={() => void unlink(method)}
              >
                Remove
              </button>
            ) : method === "email" ? (
              <form onSubmit={(event) => requestEmailProof(event, "link")}>
                <label className="sr-only" htmlFor="link-email">
                  Email to link
                </label>
                <input
                  id="link-email"
                  type="email"
                  autoComplete="email"
                  placeholder="Email to link"
                  value={email}
                  onChange={(event) => setEmail(event.target.value)}
                  disabled={busy || state?.reauthentication_required}
                  required
                />
                <button
                  className="secondary-button"
                  type="submit"
                  disabled={
                    busy || state?.reauthentication_required || !email.trim()
                  }
                >
                  Link Email
                </button>
              </form>
            ) : (
              <form
                method="post"
                action={`/v1/identity/methods/${method}/start`}
              >
                <button
                  className="secondary-button"
                  type="submit"
                  disabled={busy || state?.reauthentication_required}
                >
                  Link {methodLabels[method]}
                </button>
              </form>
            )}
          </li>
        ))}
      </ul>
      {pendingUnlink && !removalToConfirm ? (
        <button
          className="ghost-button"
          type="button"
          disabled={busy}
          onClick={() => void unlink(pendingUnlink.method, pendingUnlink)}
        >
          Retry exact removal
        </button>
      ) : null}
      <p className="identity-recovery-note">
        Keep at least one method you can use. If every linked method is lost,
        Carry cannot bypass proof or merge another account to recover this User.
        Changing methods signs out other browsers.
      </p>
    </section>
  );
}

function takeIdentityChangeStatus(): string | null {
  const url = new URL(window.location.href);
  const status = url.searchParams.get("identity_change");
  if (status === null) return null;
  url.searchParams.delete("identity_change");
  window.history.replaceState(
    null,
    "",
    `${url.pathname}${url.search}${url.hash}`,
  );
  return status;
}

function errorMessage(value: unknown): string {
  return value instanceof Error
    ? value.message
    : "Carry could not change this sign-in method";
}
