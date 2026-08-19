import { beforeEach, expect, test, vi } from "vitest";

import {
  clearPendingIdentity,
  pendingCreateIdentity,
  pendingMessageIdentity,
  pendingRetryIdentity,
} from "./work-pending";

const storageKey = "carry.pending-work-mutations.v1";

beforeEach(() => {
  window.sessionStorage.clear();
});

test("persists separate private identities for pending Work mutations", async () => {
  const create = await pendingCreateIdentity(
    "member-1",
    "space-1",
    "Review customer renewals",
  );
  const message = await pendingMessageIdentity(
    "member-1",
    "space-1",
    "work-1",
    "The renewal date is 30 September",
  );
  const replayedCreate = await pendingCreateIdentity(
    "member-1",
    "space-1",
    "Review customer renewals",
  );
  const replayedMessage = await pendingMessageIdentity(
    "member-1",
    "space-1",
    "work-1",
    "The renewal date is 30 September",
  );

  expect(replayedCreate).toEqual(create);
  expect(replayedMessage).toEqual(message);
  expect(create.idempotencyKey).not.toBe(message.idempotencyKey);
  const stored = window.sessionStorage.getItem(storageKey) ?? "";
  expect(stored).not.toContain("Review customer renewals");
  expect(stored).not.toContain("The renewal date is 30 September");
  expect(Object.keys(JSON.parse(stored) as object)).toHaveLength(2);
});

test("retains one retry identity until its exact request is reconciled", async () => {
  const first = await pendingRetryIdentity("member-1", "space-1", "work-1");
  const replayed = await pendingRetryIdentity("member-1", "space-1", "work-1");
  expect(replayed).toEqual(first);

  clearPendingIdentity(first);
  const nextChoice = await pendingRetryIdentity(
    "member-1",
    "space-1",
    "work-1",
  );
  expect(nextChoice.idempotencyKey).not.toBe(first.idempotencyKey);
});

test("fails closed when a pending identity cannot be saved", async () => {
  const save = vi
    .spyOn(window.sessionStorage, "setItem")
    .mockImplementation(() => {
      throw new DOMException("storage unavailable", "QuotaExceededError");
    });

  await expect(
    pendingCreateIdentity("member-1", "space-1", "Review renewals"),
  ).rejects.toThrow("storage unavailable");
  save.mockRestore();
});
