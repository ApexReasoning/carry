import { type SubmitEvent, useState } from "react";

type UserEntryProps = {
  step: "email" | "code";
  email: string;
  busy: boolean;
  error: string | null;
  invitationID: string | null;
  onSendCode: (email: string) => Promise<void>;
  canRetryCodeRequest: boolean;
  onRetryCodeRequest: () => Promise<void>;
  onVerifyCode: (code: string) => Promise<void>;
  onResendCode: () => Promise<void>;
  onBack: () => void;
};

export function UserEntry({
  step,
  email,
  busy,
  error,
  invitationID,
  canRetryCodeRequest,
  onSendCode,
  onRetryCodeRequest,
  onVerifyCode,
  onResendCode,
  onBack,
}: UserEntryProps) {
  const [address, setAddress] = useState(email);
  const [code, setCode] = useState("");

  async function submitEmail(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!address.trim()) return;
    await onSendCode(address);
  }

  async function submitCode(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!/^\d{6}$/.test(code)) return;
    await onVerifyCode(code);
  }

  return (
    <main className="entry-shell">
      <section className="entry-panel" aria-labelledby="entry-title">
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
        {step === "email" ? (
          <>
            <h1 id="entry-title">
              {invitationID
                ? "Sign in to review this invitation"
                : "Work that stays in good hands."}
            </h1>
            <p className="entry-copy">
              {invitationID
                ? "Authentication does not accept an invitation."
                : "Carry is your team’s durable AI colleague. Sign in to open your Work or create a Space."}
            </p>
            <div
              className="provider-entry"
              role="group"
              aria-label="Sign-in methods"
            >
              <form method="post" action="/v1/auth/google/start">
                {invitationID ? (
                  <input
                    type="hidden"
                    name="invitation_id"
                    value={invitationID}
                  />
                ) : null}
                <button
                  type="submit"
                  className="secondary-button"
                  disabled={busy}
                >
                  Continue with Google
                </button>
              </form>
              <form method="post" action="/v1/auth/github/start">
                {invitationID ? (
                  <input
                    type="hidden"
                    name="invitation_id"
                    value={invitationID}
                  />
                ) : null}
                <button
                  type="submit"
                  className="secondary-button"
                  disabled={busy}
                >
                  Continue with GitHub
                </button>
              </form>
            </div>
            <p className="entry-method-note">
              Use the same sign-in method you used before to return to existing
              Work. Google, GitHub, and email accounts are not combined yet.
            </p>
            <div className="entry-divider" aria-hidden="true">
              <span>or continue with email</span>
            </div>
            <form onSubmit={submitEmail} className="entry-form">
              <label htmlFor="email">Email</label>
              <input
                id="email"
                name="email"
                type="email"
                autoComplete="email"
                value={address}
                onChange={(event) => setAddress(event.target.value)}
                disabled={busy}
                required
              />
              {error ? (
                <p className="alert" role="alert">
                  {error}
                </p>
              ) : null}
              <button
                type="submit"
                className="primary-button entry-submit"
                disabled={busy || !address.trim()}
              >
                {busy ? "Sending…" : "Send code"}
              </button>
            </form>
          </>
        ) : (
          <>
            <h1 id="entry-title">Check your email</h1>
            <p className="entry-copy">
              Enter the newest six-digit code sent to {email}. It expires in
              five minutes.
            </p>
            <form onSubmit={submitCode} className="entry-form">
              <label htmlFor="email-code">Email code</label>
              <input
                id="email-code"
                name="email-code"
                type="text"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]{6}"
                maxLength={6}
                value={code}
                onChange={(event) =>
                  setCode(event.target.value.replace(/\D/g, "").slice(0, 6))
                }
                disabled={busy}
                required
              />
              {error ? (
                <p className="alert" role="alert">
                  {error}
                </p>
              ) : null}
              <button
                type="submit"
                className="primary-button entry-submit"
                disabled={busy || !/^\d{6}$/.test(code)}
              >
                {busy ? "Verifying…" : "Verify"}
              </button>
            </form>
            <div className="entry-secondary-actions">
              {canRetryCodeRequest ? (
                <button
                  type="button"
                  className="ghost-button"
                  onClick={() => void onRetryCodeRequest()}
                  disabled={busy}
                >
                  Try sending again
                </button>
              ) : null}
              <button
                type="button"
                className="ghost-button"
                onClick={() => void onResendCode()}
                disabled={busy}
              >
                Send a new code
              </button>
              <button
                type="button"
                className="ghost-button"
                onClick={onBack}
                disabled={busy}
              >
                Use another email
              </button>
            </div>
          </>
        )}
        <p className="entry-note">
          Signing out ends this browser session. Your Work stays with the team.
        </p>
      </section>
    </main>
  );
}
