import { expect, test } from "vitest";

import type { ConversationMessage } from "../../generated/types.gen";
import {
  appendUniqueMessages,
  prependUniqueMessages,
} from "./conversation-page";

test("appends only messages not already visible", () => {
  const first = message("11111111-1111-4111-8111-111111111111", "First");
  const second = message("22222222-2222-4222-8222-222222222222", "Second");

  expect(appendUniqueMessages([first], [first, second])).toEqual([
    first,
    second,
  ]);
});

test("prepends earlier messages without duplicating the page boundary", () => {
  const first = message("11111111-1111-4111-8111-111111111111", "First");
  const second = message("22222222-2222-4222-8222-222222222222", "Second");

  expect(prependUniqueMessages([first, second], [second])).toEqual([
    first,
    second,
  ]);
});

function message(messageID: string, text: string): ConversationMessage {
  return {
    message_id: messageID,
    author: "member",
    text,
    created_at: "2026-08-20T12:00:00Z",
  };
}
