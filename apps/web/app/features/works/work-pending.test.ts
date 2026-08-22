import { beforeEach, expect, test, vi } from "vitest";

import {
  clearPendingIdentity,
  CorruptPendingWorkIdentitiesError,
  discardCorruptPendingWorkIdentities,
  pendingCreateIdentity,
  pendingMessageIdentity,
  pendingReviewIdentity,
  pendingRetryIdentity,
} from "./work-pending";

const storageKey = "carry.pending-work-mutations.v1";

beforeEach(() => {
  window.sessionStorage.clear();
});

test("persists separate content-free identities for pending Work mutations", async () => {
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

test("binds a pending review identity to the exact review without storing content", async () => {
  const first = await pendingReviewIdentity(
    "member-1",
    "space-1",
    "work-1",
    "11111111-1111-4111-8111-111111111111",
  );
  const replayed = await pendingReviewIdentity(
    "member-1",
    "space-1",
    "work-1",
    "11111111-1111-4111-8111-111111111111",
  );
  const nextReview = await pendingReviewIdentity(
    "member-1",
    "space-1",
    "work-1",
    "22222222-2222-4222-8222-222222222222",
  );

  expect(replayed).toEqual(first);
  expect(nextReview.idempotencyKey).not.toBe(first.idempotencyKey);
  const stored = window.sessionStorage.getItem(storageKey) ?? "";
  expect(stored).not.toContain("11111111-1111-4111-8111-111111111111");
  expect(stored).not.toContain("22222222-2222-4222-8222-222222222222");
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

test("fails closed on malformed pending identities", async () => {
  for (const malformed of [
    "not JSON",
    "[]",
    JSON.stringify({ invalid: crypto.randomUUID() }),
    JSON.stringify({ ["a".repeat(64)]: "not-a-uuid" }),
    JSON.stringify({
      ["a".repeat(64)]: { idempotencyKey: crypto.randomUUID() },
    }),
  ]) {
    window.sessionStorage.setItem(storageKey, malformed);
    await expect(
      pendingCreateIdentity("member-1", "space-1", "Review renewals"),
    ).rejects.toThrow(CorruptPendingWorkIdentitiesError);
  }
});

test("discards damaged identities only after explicit recovery", async () => {
  window.sessionStorage.setItem(storageKey, "not JSON");

  await expect(
    pendingCreateIdentity("member-1", "space-1", "Review renewals"),
  ).rejects.toThrow(CorruptPendingWorkIdentitiesError);
  discardCorruptPendingWorkIdentities();
  expect(window.sessionStorage.getItem(storageKey)).toBeNull();
  await expect(
    pendingCreateIdentity("member-1", "space-1", "Review renewals"),
  ).resolves.toMatchObject({ idempotencyKey: expect.any(String) });
});

test("fails closed when a pending identity cannot be published", async () => {
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

test("fails closed when a pending identity write cannot be read back", async () => {
  const read = vi
    .spyOn(window.sessionStorage, "getItem")
    .mockReturnValueOnce(null)
    .mockReturnValueOnce(null);

  await expect(
    pendingCreateIdentity("member-1", "space-1", "Review renewals"),
  ).rejects.toThrow("could not be saved");
  read.mockRestore();
});

test("fails closed when a reconciled identity cannot be cleared", async () => {
  const identity = await pendingRetryIdentity("member-1", "space-1", "work-1");
  const remove = vi
    .spyOn(window.sessionStorage, "removeItem")
    .mockImplementation(() => undefined);

  expect(() => clearPendingIdentity(identity)).toThrow("could not be cleared");
  remove.mockRestore();
});
