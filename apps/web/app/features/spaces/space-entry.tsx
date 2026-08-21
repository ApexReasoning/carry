import type { User } from "../../generated/types.gen";
import {
  MutationOutcomeUnknownError,
  SpaceSlugConflictError,
} from "../../carry-api";
import { createExactSpace } from "./space-creation";
import { useState } from "react";

type CreationState =
  | { kind: "idle" }
  | { kind: "creating"; suffix: number | undefined }
  | { kind: "error"; message: string }
  | { kind: "unknown"; message: string; suffix: number | undefined }
  | { kind: "conflict"; value: SpaceSlugConflictError };

export function SpaceEntry({
  user,
  onEnter,
  notice,
}: {
  user: User;
  onEnter: (slug: string) => void;
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
    if (!name.trim()) return;
    setCreation({ kind: "creating", suffix });
    try {
      const created = await createExactSpace(user.user_id, name, suffix);
      setCreation({ kind: "idle" });
      onEnter(created.slug);
    } catch (caught) {
      if (caught instanceof SpaceSlugConflictError) {
        setCreation({ kind: "conflict", value: caught });
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
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
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
            disabled={busy || !name.trim()}
          >
            {busy
              ? "Creating…"
              : creation.kind === "unknown"
                ? "Retry exact request"
                : "Create Space"}
          </button>
        </form>
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
