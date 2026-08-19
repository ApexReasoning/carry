import { type SubmitEvent, useState } from "react";

import type { WorkDetails } from "../../carry-api";
import { formatWorkDate, formatWorkTime } from "./work-time";

type WorkDetailProps = {
  details: WorkDetails | null;
  busy: boolean;
  currentMemberID: string;
  onMessage: (text: string) => Promise<boolean>;
};

export function WorkDetail({
  details,
  busy,
  currentMemberID,
  onMessage,
}: WorkDetailProps) {
  const [text, setText] = useState("");

  async function submit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!text.trim()) {
      return;
    }
    if (await onMessage(text)) {
      setText("");
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
    details.work.owner_user_id === currentMemberID ? "You" : "Another member";

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

      <section className="message-section" aria-labelledby="messages-title">
        <h3 id="messages-title">Messages</h3>
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
                      : "Another member"}
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
          maxLength={61_440}
          placeholder="Facts, corrections, or answers Carry should keep with this Work"
          value={text}
          onChange={(event) => setText(event.target.value)}
          disabled={busy}
          required
        />
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
