import { beforeEach, expect, test, vi } from "vitest";

import {
  clearPendingConversationRequestID,
  loadPendingConversationRequestID,
  pendingConversationRequestID,
} from "./conversation-pending";

const memberID = "member-1";
const spaceID = "11111111-1111-4111-8111-111111111111";

beforeEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
  window.sessionStorage.clear();
});

test("durably reuses one random request key for the member and Space", () => {
  const generated = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
  const randomUUID = vi.spyOn(crypto, "randomUUID").mockReturnValue(generated);

  expect(pendingConversationRequestID(memberID, spaceID)).toBe(generated);
  expect(pendingConversationRequestID(memberID, spaceID)).toBe(generated);
  expect(randomUUID).toHaveBeenCalledTimes(1);
  expect(window.sessionStorage.length).toBe(1);
  expect(window.localStorage.length).toBe(0);
});

test("stores neither private text nor its deterministic digest", async () => {
  const privateText = "Please review the confidential renewal note";
  pendingConversationRequestID(memberID, spaceID);
  const digest = Array.from(
    new Uint8Array(
      await crypto.subtle.digest(
        "SHA-256",
        new TextEncoder().encode(privateText),
      ),
    ),
    (value) => value.toString(16).padStart(2, "0"),
  ).join("");
  const stored = Array.from(
    { length: window.sessionStorage.length },
    (_, index) => {
      const key = window.sessionStorage.key(index)!;
      return `${key}:${window.sessionStorage.getItem(key)}`;
    },
  ).join("|");

  expect(stored).not.toContain(privateText);
  expect(stored).not.toContain(digest);
  expect(window.localStorage.length).toBe(0);
});

test("clears only the exact admitted request key", () => {
  const requestID = pendingConversationRequestID(memberID, spaceID);
  clearPendingConversationRequestID(memberID, spaceID, crypto.randomUUID());
  expect(loadPendingConversationRequestID(memberID, spaceID)).toBe(requestID);

  clearPendingConversationRequestID(memberID, spaceID, requestID);
  expect(loadPendingConversationRequestID(memberID, spaceID)).toBeNull();
});

test("fails closed when the request key cannot be durably published", () => {
  const save = vi
    .spyOn(window.sessionStorage, "setItem")
    .mockImplementation(() => {
      throw new DOMException("storage unavailable", "QuotaExceededError");
    });

  expect(() => pendingConversationRequestID(memberID, spaceID)).toThrow(
    "storage unavailable",
  );
  save.mockRestore();
});

test("fails closed on unreadable or invalid pending storage", () => {
  const requestID = pendingConversationRequestID(memberID, spaceID);
  const storageKey = window.sessionStorage.key(0)!;
  window.sessionStorage.setItem(storageKey, "private-text-instead-of-an-id");
  expect(() => loadPendingConversationRequestID(memberID, spaceID)).toThrow(
    "Pending private message identity is invalid",
  );

  window.sessionStorage.setItem(storageKey, requestID);
  const read = vi
    .spyOn(window.sessionStorage, "getItem")
    .mockImplementation(() => {
      throw new DOMException("storage unreadable", "SecurityError");
    });
  expect(() => pendingConversationRequestID(memberID, spaceID)).toThrow(
    "storage unreadable",
  );
  read.mockRestore();
});
