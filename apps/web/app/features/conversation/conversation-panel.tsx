import { type KeyboardEvent, type SubmitEvent, useState } from "react";

import { useConversation } from "./use-conversation";

type ConversationPanelProps = {
  memberID: string;
  spaceID: string;
  workBusy: boolean;
  onOpenWork: (workID: string) => Promise<void>;
};

export function ConversationPanel({
  memberID,
  spaceID,
  workBusy,
  onOpenWork,
}: ConversationPanelProps) {
  const conversation = useConversation(memberID, spaceID);
  const [text, setText] = useState("");
  const [inputError, setInputError] = useState<string | null>(null);

  async function submit(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    const validationError = privateMessageInputError(text);
    setInputError(validationError);
    if (validationError) return;
    if (await conversation.send(text)) {
      setText("");
      setInputError(null);
    }
  }

  function submitOnEnter(event: KeyboardEvent<HTMLTextAreaElement>) {
    if (
      event.key !== "Enter" ||
      event.shiftKey ||
      event.nativeEvent.isComposing
    ) {
      return;
    }
    event.preventDefault();
    event.currentTarget.form?.requestSubmit();
  }

  const composerDisabled =
    conversation.loading ||
    conversation.sending ||
    conversation.awaitingReply ||
    conversation.identityBlocked;

  return (
    <section
      className="conversation-panel"
      aria-labelledby="conversation-title"
    >
      <header className="conversation-header">
        <div>
          <p className="eyebrow">Private with Carry</p>
          <h2 id="conversation-title">Talk to Carry</h2>
        </div>
        <p className="conversation-privacy">
          Other members in this Space cannot see this conversation. When you
          clearly ask Carry to take responsibility, it can create shared Work
          without copying your private message.
        </p>
      </header>

      {conversation.error ? (
        <p className="alert" role="alert">
          {conversation.error}
        </p>
      ) : null}

      <div
        className="conversation-transcript"
        role="log"
        aria-live="polite"
        aria-relevant="additions"
        aria-busy={conversation.loading}
      >
        {conversation.canLoadEarlier ? (
          <button
            className="ghost-button load-earlier"
            type="button"
            onClick={() => void conversation.loadEarlier()}
            disabled={conversation.loadingEarlier}
          >
            {conversation.loadingEarlier ? "Loading…" : "Load earlier"}
          </button>
        ) : null}
        {conversation.loading ? (
          <p className="conversation-empty">
            Opening your private conversation…
          </p>
        ) : conversation.messages.length === 0 ? (
          <p className="conversation-empty">
            Ask a question or give Carry a responsibility. Nothing here is
            shared with the Space unless you clearly ask Carry to take it on.
          </p>
        ) : (
          <ol className="conversation-messages">
            {conversation.messages.map((message) => (
              <li
                className={`conversation-message ${message.author}`}
                key={message.message_id}
              >
                <article>
                  <header className="conversation-message-meta">
                    <strong>
                      {message.author === "member" ? "You" : "Carry"}
                    </strong>
                    <time dateTime={message.created_at}>
                      {formatPrivateMessageTime(message.created_at)}
                    </time>
                  </header>
                  <p>{message.text}</p>
                  {message.created_work_id ? (
                    <button
                      className="secondary-button conversation-work-link"
                      type="button"
                      disabled={workBusy}
                      onClick={() => {
                        const workID = message.created_work_id;
                        if (workID) void onOpenWork(workID);
                      }}
                    >
                      Open Work
                    </button>
                  ) : null}
                </article>
              </li>
            ))}
          </ol>
        )}
      </div>

      <p className="conversation-status" aria-live="polite">
        {conversation.awaitingReply
          ? "Waiting for Carry’s reply. You can send another message after it arrives."
          : "Carry handles one private message at a time."}
      </p>

      <form className="conversation-form" onSubmit={submit}>
        <label htmlFor="private-message">Message Carry privately</label>
        <textarea
          id="private-message"
          name="private-message"
          rows={3}
          placeholder="Ask a question or give Carry a responsibility"
          value={text}
          onChange={(event) => {
            setText(event.target.value);
            setInputError(null);
          }}
          onKeyDown={submitOnEnter}
          disabled={composerDisabled}
          required
          aria-describedby="private-message-hint"
        />
        <p className="field-hint" id="private-message-hint">
          Press Enter to send. Press Shift+Enter for a new line.
        </p>
        {inputError ? (
          <p className="alert" role="alert">
            {inputError}
          </p>
        ) : null}
        <button
          className="primary-button"
          type="submit"
          disabled={composerDisabled || !text.trim()}
        >
          {conversation.sending ? "Sending…" : "Send privately"}
        </button>
      </form>
    </section>
  );
}

function privateMessageInputError(text: string): string | null {
  const trimmed = text.trim();
  if (!trimmed) return "Write a private message before sending.";
  if (new TextEncoder().encode(trimmed).byteLength > 16 * 1024) {
    return "Keep the private message under 16 KiB.";
  }
  return null;
}

function formatPrivateMessageTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : new Intl.DateTimeFormat(undefined, {
        hour: "numeric",
        minute: "2-digit",
      }).format(date);
}
