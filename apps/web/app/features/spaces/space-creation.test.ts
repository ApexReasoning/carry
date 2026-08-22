import { beforeEach, expect, test, vi } from "vitest";

const api = vi.hoisted(() => ({ createSpace: vi.fn() }));

vi.mock("../../carry-api", () => {
  class MutationOutcomeUnknownError extends Error {}
  return {
    createSpace: api.createSpace,
    MutationOutcomeUnknownError,
  };
});

import { MutationOutcomeUnknownError } from "../../carry-api";
import {
  CorruptPendingSpaceCreationError,
  createExactSpace,
  discardCorruptPendingSpaceCreation,
  pendingSpaceCreationIdentity,
} from "./space-creation";

beforeEach(() => {
  window.sessionStorage.clear();
  api.createSpace.mockReset();
});

test("persists only a content-free identity and reuses it for exact input", async () => {
  const first = await pendingSpaceCreationIdentity("member-1", "Secret Team");
  const replay = await pendingSpaceCreationIdentity("member-1", "Secret Team");
  const changed = await pendingSpaceCreationIdentity("member-1", "Other Team");

  expect(replay).toEqual(first);
  expect(changed.idempotencyKey).not.toBe(first.idempotencyKey);
  const stored = window.sessionStorage.getItem(
    "carry.pending-space-creation.v1",
  );
  expect(stored).not.toContain("Secret Team");
  expect(stored).not.toContain("Other Team");
});

test("retains malformed app-owned state until explicit recovery", async () => {
  const storageKey = "carry.pending-space-creation.v1";
  window.sessionStorage.setItem(storageKey, "{not-json");
  window.sessionStorage.setItem("unrelated", "preserved");

  await expect(
    pendingSpaceCreationIdentity("member-1", "Research"),
  ).rejects.toThrow(CorruptPendingSpaceCreationError);
  expect(window.sessionStorage.getItem(storageKey)).toBe("{not-json");
  expect(window.sessionStorage.getItem("unrelated")).toBe("preserved");

  discardCorruptPendingSpaceCreation();
  expect(window.sessionStorage.getItem(storageKey)).toBeNull();
  const recovered = await pendingSpaceCreationIdentity("member-1", "Research");
  expect(recovered.idempotencyKey).toBeTruthy();
  expect(window.sessionStorage.getItem(storageKey)).toBeTruthy();
});

test("replays an unknown outcome with the exact request identity", async () => {
  api.createSpace
    .mockRejectedValueOnce(new MutationOutcomeUnknownError("lost"))
    .mockResolvedValueOnce({
      space_id: "space-1",
      name: "Research",
      slug: "research",
      can_manage_members: true,
      can_enroll_machines: true,
    });

  const created = await createExactSpace("member-1", "Research");

  expect(created.slug).toBe("research");
  expect(api.createSpace).toHaveBeenCalledTimes(2);
  expect(api.createSpace.mock.calls[0]?.[2]).toBe(
    api.createSpace.mock.calls[1]?.[2],
  );
  expect(window.sessionStorage.length).toBe(0);
});

test("retains the request identity when replay is still unknown", async () => {
  api.createSpace.mockRejectedValue(new MutationOutcomeUnknownError("lost"));

  await expect(createExactSpace("member-1", "Research")).rejects.toThrow(
    MutationOutcomeUnknownError,
  );
  const firstKey = api.createSpace.mock.calls[0]?.[2];
  api.createSpace.mockReset();
  api.createSpace.mockResolvedValue({
    space_id: "space-1",
    name: "Research",
    slug: "research",
    can_manage_members: true,
    can_enroll_machines: true,
  });
  await createExactSpace("member-1", "Research");

  expect(api.createSpace.mock.calls[0]?.[2]).toBe(firstKey);
});
