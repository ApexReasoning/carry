import type { ConversationMessage } from "../../generated/types.gen";

export function appendUniqueMessages(
  current: Array<ConversationMessage>,
  incoming: Array<ConversationMessage>,
): Array<ConversationMessage> {
  const known = new Set(current.map((message) => message.message_id));
  return [
    ...current,
    ...incoming.filter((message) => !known.has(message.message_id)),
  ];
}

export function prependUniqueMessages(
  earlier: Array<ConversationMessage>,
  current: Array<ConversationMessage>,
): Array<ConversationMessage> {
  const known = new Set(current.map((message) => message.message_id));
  return [
    ...earlier.filter((message) => !known.has(message.message_id)),
    ...current,
  ];
}
