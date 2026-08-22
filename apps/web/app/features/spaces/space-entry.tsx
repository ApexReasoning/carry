import type { User } from "../../generated/types.gen";
import {
  MutationOutcomeUnknownError,
  SpaceSlugConflictError,
} from "../../carry-api";
import {
  CorruptPendingSpaceCreationError,
  createExactSpace,
  discardCorruptPendingSpaceCreation,
} from "./space-creation";
import { useState } from "react";

type CreationState =
  | { kind: "idle" }
  | { kind: "creating"; suffix: number | undefined }
  | { kind: "error"; message: string }
  | { kind: "damaged"; clearError?: string }
  | { kind: "unknown"; message: string; suffix: number | undefined }
  | { kind: "conflict"; value: SpaceSlugConflictError };

export function SpaceEntry({
  user,
  onEnter,
  onSignOut,
  notice,
}: {
  user: User;
  onEnter: (slug: string) => void;
  onSignOut: () => void;
  notice?: string | null;
}) {
  const [name, setName] = useState("");
  const [creation, setCreation] = useState<CreationState>({ kind: "idle" });
  const busy = creation.kind === "creating";
  const conflict = creation.kind === "conflict" ? creation.value : null;
  const error =
    creation.kind === "error" || creation.kind === "unknown"
      ? creation.message
      : conflict?.message;

  async function create(suffix?: number) {
    if (!name.trim() || creation.kind === "damaged") return;
    setCreation({ kind: "creating", suffix });
    try {
      const created = await createExactSpace(user.user_id, name, suffix);
      setCreation({ kind: "idle" });
      onEnter(created.slug);
    } catch (caught) {
      if (caught instanceof SpaceSlugConflictError) {
        setCreation({ kind: "conflict", value: caught });
      } else if (caught instanceof CorruptPendingSpaceCreationError) {
        setCreation({ kind: "damaged" });
      } else if (caught instanceof MutationOutcomeUnknownError) {
        setCreation({ kind: "unknown", message: caught.message, suffix });
      } else {
        setCreation({ kind: "error", message: message(caught) });
      }
    }
  }

  return (
    <main className="space-entry">
      <section
        className="space-entry-panel"
        aria-labelledby="space-entry-title"
      >
        <div className="space-entry-account">
          <p className="brand-mark">
            Carry<span className="brand-dot">.</span>
          </p>
          <div>
            <span className="member-name">{user.display_name}</span>
            <a className="ghost-button" href="/invitations">
              Invitations
            </a>
            <button className="ghost-button" type="button" onClick={onSignOut}>
              Sign out
            </button>
          </div>
        </div>
        <p className="eyebrow">Your Spaces</p>
        <h1 id="space-entry-title">Choose a Space</h1>
        {user.spaces.length === 0 ? (
          <p className="empty-panel">You do not belong to a Space yet.</p>
        ) : (
          <ul className="space-entry-list">
            {user.spaces.map((space) => (
              <li key={space.space_id}>
                <a href={`/s/${encodeURIComponent(space.slug)}`}>
                  <strong>{space.name}</strong>
                  <span>/s/{space.slug}</span>
                </a>
              </li>
            ))}
          </ul>
        )}
        <form
          onSubmit={(event) => {
            event.preventDefault();
            void create(
              creation.kind === "unknown" ? creation.suffix : undefined,
            );
          }}
        >
          <label>
            Space name
            <input
              value={name}
              autoComplete="organization"
              onChange={(event) => {
                setName(event.target.value);
                setCreation({ kind: "idle" });
              }}
            />
          </label>
          <button
            className="primary-button"
            type="submit"
            disabled={busy || creation.kind === "damaged" || !name.trim()}
          >
            {busy
              ? "Creating…"
              : creation.kind === "unknown"
                ? "Try creating again"
                : "Create Space"}
          </button>
        </form>
        {creation.kind === "damaged" ? (
          <section className="identity-confirmation">
            <p>
              Carry cannot read the saved Space creation on this browser. Check
              the Space list above. If it is not there, clear the saved entry
              before creating again.
            </p>
            <button
              className="secondary-button"
              type="button"
              onClick={() => {
                try {
                  discardCorruptPendingSpaceCreation();
                  setCreation({ kind: "idle" });
                } catch (caught) {
                  setCreation({ kind: "damaged", clearError: message(caught) });
                }
              }}
            >
              Clear saved Space creation
            </button>
            {creation.clearError ? (
              <p className="alert" role="alert">
                {creation.clearError}
              </p>
            ) : null}
          </section>
        ) : null}
        {conflict?.suggestedSlug && conflict.suggestedSuffix ? (
          <div className="space-conflict">
            <p>
              <code>/s/{conflict.slug}</code> is already in use. The next
              suggestion is not reserved.
            </p>
            <button
              className="secondary-button"
              type="button"
              disabled={busy}
              onClick={() => void create(conflict.suggestedSuffix)}
            >
              Try /s/{conflict.suggestedSlug}
            </button>
          </div>
        ) : null}
        {conflict && !conflict.suggestedSlug ? (
          <p>Shorten the Space name so a numbered URL can fit.</p>
        ) : null}
        {(error ?? notice) ? (
          <p className="alert" role="alert">
            {error ?? notice}
          </p>
        ) : null}
      </section>
    </main>
  );
}

function message(value: unknown): string {
  return value instanceof Error
    ? value.message
    : "Carry could not create the Space";
}
