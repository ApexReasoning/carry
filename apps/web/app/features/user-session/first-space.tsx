import { type SubmitEvent, useState } from "react";

type FirstSpaceProps = {
  busy: boolean;
  error: string | null;
  initialName: string;
  onCreate: (displayName: string, spaceName: string) => Promise<void>;
};

export function FirstSpace({
  busy,
  error,
  initialName,
  onCreate,
}: FirstSpaceProps) {
  const [displayName, setDisplayName] = useState(initialName);
  const [spaceName, setSpaceName] = useState("");

  async function submit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!displayName.trim() || !spaceName.trim()) return;
    await onCreate(displayName, spaceName);
  }

  return (
    <main className="entry-shell">
      <section className="entry-panel" aria-labelledby="first-space-title">
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
        <h1 id="first-space-title">Create your Space</h1>
        <p className="entry-copy">
          A Space is where your team and its Work live.
        </p>
        <form onSubmit={submit} className="entry-form">
          <label htmlFor="display-name">Your name</label>
          <input
            id="display-name"
            name="display-name"
            type="text"
            autoComplete="name"
            value={displayName}
            onChange={(event) => setDisplayName(event.target.value)}
            disabled={busy}
            required
          />
          <label htmlFor="space-name">Space name</label>
          <input
            id="space-name"
            name="space-name"
            type="text"
            autoComplete="organization"
            value={spaceName}
            onChange={(event) => setSpaceName(event.target.value)}
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
            disabled={busy || !displayName.trim() || !spaceName.trim()}
          >
            {busy ? "Creating…" : "Create Space"}
          </button>
        </form>
      </section>
    </main>
  );
}
