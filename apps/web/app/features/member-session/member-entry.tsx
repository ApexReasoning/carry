import { type SubmitEvent, useState } from "react";

type MemberEntryProps = {
  busy: boolean;
  error: string | null;
  onOpen: (token: string) => Promise<void>;
};

export function MemberEntry({ busy, error, onOpen }: MemberEntryProps) {
  const [token, setToken] = useState("");

  async function submit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    const submittedToken = token.trim();
    if (!submittedToken) {
      return;
    }
    try {
      await onOpen(submittedToken);
    } finally {
      // The bearer is used only for the exchange and never enters browser
      // storage or application-wide client configuration.
      setToken("");
    }
  }

  return (
    <main className="entry-shell">
      <section className="entry-panel" aria-labelledby="entry-title">
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
        <h1 id="entry-title">Work that stays in good hands.</h1>
        <p className="entry-copy">
          Carry is your team’s durable AI colleague. Open it once with your
          member token: the token is exchanged for a protected session and is
          never stored in this browser.
        </p>
        <form onSubmit={submit} className="entry-form">
          <label htmlFor="member-token">Member token</label>
          <input
            id="member-token"
            name="member-token"
            type="password"
            autoComplete="off"
            spellCheck={false}
            value={token}
            onChange={(event) => setToken(event.target.value)}
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
            disabled={busy || !token.trim()}
          >
            {busy ? "Opening…" : "Open Carry"}
          </button>
        </form>
        <p className="entry-note">
          Signing out ends the session on this browser. Your Work stays with the
          team and remains where you left it.
        </p>
      </section>
    </main>
  );
}
