import { type SubmitEvent, useState } from "react";

import type { WorkDetails } from "../../carry-api";
import { messageInputError } from "./work-input";
import { formatWorkDate, formatWorkTime } from "./work-time";

type WorkDetailProps = {
  details: WorkDetails | null;
  busy: boolean;
  currentMemberID: string;
  onMessage: (text: string) => Promise<boolean>;
  onRetry: () => Promise<void>;
  onLoadEarlierMessages: () => void;
};

export function WorkDetail({
  details,
  busy,
  currentMemberID,
  onMessage,
  onRetry,
  onLoadEarlierMessages,
}: WorkDetailProps) {
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function submit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    const validationError = messageInputError(text);
    setError(validationError);
    if (validationError) return;
    if (await onMessage(text)) {
      setText("");
      setError(null);
    }
  }

  if (!details) {
    return (
      <section className="work-detail placeholder" aria-live="polite">
        <p>Select a Work to read its goal and messages.</p>
      </section>
    );
  }

  const responsible =
    details.work.owner_user_id === currentMemberID
      ? "You"
      : details.work.owner_display_name;

  return (
    <article className="work-detail" aria-labelledby="work-detail-title">
      <header className="work-detail-header">
        <p className="eyebrow">Open Work</p>
        <h2 id="work-detail-title">{details.work.goal}</h2>
        <dl className="work-facts">
          <div>
            <dt>Responsible</dt>
            <dd>{responsible}</dd>
          </div>
          <div>
            <dt>Created</dt>
            <dd>
              <time dateTime={details.work.created_at}>
                {formatWorkDate(details.work.created_at)}
              </time>
            </dd>
          </div>
        </dl>
      </header>

      {details.work.needs_retry ? (
        <section className="current-understanding" aria-live="polite">
          <h3>Carry needs your choice</h3>
          <p className="empty-copy">
            Carry could not confirm an update for this Work. It will not try
            again until you choose to.
          </p>
          <button
            className="secondary-button"
            type="button"
            disabled={busy}
            onClick={() => void onRetry()}
          >
            {busy ? "Trying again…" : "Try again"}
          </button>
        </section>
      ) : null}

      <section
        className="current-understanding"
        aria-labelledby="current-understanding-title"
        aria-live="polite"
      >
        <h3 id="current-understanding-title">Current understanding</h3>
        {!details.work.understanding ? (
          <p className="empty-copy">
            This Work’s information has not been applied yet.
          </p>
        ) : (
          <>
            <p className="understanding-copy">{details.work.understanding}</p>
            <div className="next-step">
              <h4>Next step</h4>
              <p>{details.work.next_step}</p>
            </div>
          </>
        )}
        {details.work.understanding && details.work.has_unapplied_input ? (
          <p className="empty-copy">
            New information has not been applied yet.
          </p>
        ) : null}
      </section>

      <section className="message-section" aria-labelledby="messages-title">
        <h3 id="messages-title">Messages</h3>
        {details.has_earlier_messages ? (
          <button
            className="ghost-button load-earlier"
            type="button"
            onClick={onLoadEarlierMessages}
            disabled={busy}
          >
            Load earlier messages
          </button>
        ) : null}
        {details.messages.length === 0 ? (
          <p className="empty-copy">
            Nothing recorded yet. Add the facts Carry needs below.
          </p>
        ) : (
          <ol className="message-list">
            {details.messages.map((message) => (
              <li className="message" key={message.message_id}>
                <p className="message-text">{message.text}</p>
                <p className="message-meta">
                  <span className="message-author">
                    {message.author_user_id === currentMemberID
                      ? "You"
                      : message.author_display_name}
                  </span>
                  <time dateTime={message.created_at}>
                    {formatWorkTime(message.created_at)}
                  </time>
                </p>
              </li>
            ))}
          </ol>
        )}
      </section>

      <form className="message-form" onSubmit={submit}>
        <label htmlFor="work-message">Add information for Carry</label>
        <textarea
          id="work-message"
          name="work-message"
          rows={3}
          placeholder="Facts, corrections, or answers Carry should keep with this Work"
          value={text}
          onChange={(event) => {
            setText(event.target.value);
            setError(null);
          }}
          disabled={busy}
          required
        />
        {error ? (
          <p className="alert" role="alert">
            {error}
          </p>
        ) : null}
        <button
          className="secondary-button"
          type="submit"
          disabled={busy || !text.trim()}
        >
          {busy ? "Adding…" : "Add message"}
        </button>
      </form>
    </article>
  );
}
