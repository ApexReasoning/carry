import { act, renderHook, waitFor } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type {
  User,
  Work,
  WorkMessage,
  WorkSummary,
} from "../../generated/types.gen";
import { useWorkBoard } from "./use-work-board";

const userID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const spaceID = "11111111-1111-4111-8111-111111111111";
const newestWorkID = "22222222-2222-4222-8222-222222222222";
const olderWorkID = "33333333-3333-4333-8333-333333333333";
const oldestMessageID = "44444444-4444-4444-8444-444444444444";
const middleMessageID = "55555555-5555-4555-8555-555555555555";
const newestMessageID = "66666666-6666-4666-8666-666666666666";

const user: User = {
  user_id: userID,
  display_name: "Alex Morgan",
  spaces: [
    {
      space_id: spaceID,
      name: "Research",
      slug: "research",
      can_manage_members: true,
      can_enroll_machines: true,
    },
  ],
};

test("requires explicit recovery before discarding damaged Work identities", async () => {
  window.sessionStorage.clear();
  window.sessionStorage.setItem("carry.pending-work-mutations.v1", "not JSON");
  const requests: Array<Request> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      requests.push(request);
      return json({ works: [], has_earlier_works: false });
    }),
  );

  const { result } = renderHook(() => useWorkBoard(user, spaceID));
  await waitFor(() => expect(result.current.spaceID).toBe(spaceID));
  await act(() => result.current.addWork("Do not duplicate unknown Work"));

  expect(result.current.pendingIdentitiesCorrupt).toBe(true);
  expect(requests.filter((request) => request.method === "POST")).toHaveLength(
    0,
  );
  expect(window.sessionStorage.getItem("carry.pending-work-mutations.v1")).toBe(
    "not JSON",
  );

  act(() => result.current.discardDamagedPendingIdentities());
  expect(result.current.pendingIdentitiesCorrupt).toBe(false);
  expect(
    window.sessionStorage.getItem("carry.pending-work-mutations.v1"),
  ).toBeNull();
});

test("loads bounded earlier Work and message pages without duplicates", async () => {
  const requestedURLs: Array<string> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const url = new URL(request.url);
      requestedURLs.push(url.pathname + url.search);

      if (url.pathname === `/v1/spaces/${spaceID}/works`) {
        if (url.searchParams.get("before") === newestWorkID) {
          return json({
            works: [summary(olderWorkID, "Review older renewals")],
            has_earlier_works: false,
          });
        }
        return json({
          works: [summary(newestWorkID, "Review current renewals")],
          has_earlier_works: true,
        });
      }
      if (url.pathname === `/v1/spaces/${spaceID}/works/${newestWorkID}`) {
        if (url.searchParams.get("before") === middleMessageID) {
          return json({
            work: fullWork(),
            messages: [workMessage(oldestMessageID, "Oldest fact")],
            has_earlier_messages: false,
          });
        }
        return json({
          work: fullWork(),
          messages: [
            workMessage(middleMessageID, "Middle fact"),
            workMessage(newestMessageID, "Newest fact"),
          ],
          has_earlier_messages: true,
        });
      }
      throw new Error(`unexpected request ${request.method} ${url}`);
    }),
  );

  const { result } = renderHook(() => useWorkBoard(user, spaceID));
  await waitFor(() => expect(result.current.works).toHaveLength(1));
  expect(result.current.hasEarlierWorks).toBe(true);

  await act(() => result.current.loadEarlierWorks());
  expect(result.current.works.map((item) => item.work_id)).toEqual([
    newestWorkID,
    olderWorkID,
  ]);
  expect(result.current.hasEarlierWorks).toBe(false);

  await act(() => result.current.selectWork(newestWorkID));
  expect(result.current.details?.messages.map((item) => item.text)).toEqual([
    "Middle fact",
    "Newest fact",
  ]);
  expect(result.current.details?.has_earlier_messages).toBe(true);

  await act(() => result.current.loadEarlierMessages());
  expect(result.current.details?.messages.map((item) => item.text)).toEqual([
    "Oldest fact",
    "Middle fact",
    "Newest fact",
  ]);
  expect(result.current.details?.has_earlier_messages).toBe(false);
  expect(
    new Set(result.current.details?.messages.map((item) => item.message_id))
      .size,
  ).toBe(3);
  expect(requestedURLs).toContain(
    `/v1/spaces/${spaceID}/works?before=${newestWorkID}`,
  );
  expect(requestedURLs).toContain(
    `/v1/spaces/${spaceID}/works/${newestWorkID}?before=${middleMessageID}`,
  );
});

test("reuses the exact acceptance identity when a lost request did not commit", async () => {
  let accepted = false;
  let acceptanceAttempts = 0;
  const idempotencyKeys: Array<string | null> = [];
  const reviewID = "77777777-7777-4777-8777-777777777777";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname === `/v1/spaces/${spaceID}/works`
      ) {
        return json({
          works: accepted
            ? []
            : [
                {
                  ...summary(newestWorkID, "Review recommendation"),
                  needs_review: true,
                },
              ],
          has_earlier_works: false,
        });
      }
      if (
        request.method === "GET" &&
        url.pathname === `/v1/spaces/${spaceID}/works/${newestWorkID}`
      ) {
        return json({
          work: accepted
            ? fullWork()
            : { ...fullWork(), needs_review: true, review_id: reviewID },
          messages: [],
          has_earlier_messages: false,
        });
      }
      if (
        request.method === "POST" &&
        url.pathname ===
          `/v1/spaces/${spaceID}/works/${newestWorkID}/reviews/${reviewID}/accept`
      ) {
        acceptanceAttempts += 1;
        idempotencyKeys.push(request.headers.get("Idempotency-Key"));
        if (acceptanceAttempts === 1) {
          throw new TypeError("request outcome unknown");
        }
        accepted = true;
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected request ${request.method} ${url}`);
    }),
  );

  const { result } = renderHook(() => useWorkBoard(user, spaceID));
  await waitFor(() => expect(result.current.works).toHaveLength(1));
  await act(() => result.current.selectWork(newestWorkID));

  await act(() => result.current.acceptCurrentReview());
  expect(result.current.details?.work.needs_review).toBe(true);

  await act(() => result.current.acceptCurrentReview());
  expect(result.current.details?.work.needs_review).toBe(false);
  expect(idempotencyKeys).toHaveLength(2);
  expect(idempotencyKeys[0]).toMatch(/^[0-9a-f-]{36}$/);
  expect(idempotencyKeys[1]).toBe(idempotencyKeys[0]);
});

test("accepts the exact Needs You result and reconciles a lost response", async () => {
  let accepted = false;
  const acceptanceRequests: Array<Request> = [];
  const reviewID = "77777777-7777-4777-8777-777777777777";
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const url = new URL(request.url);
      if (
        request.method === "GET" &&
        url.pathname === `/v1/spaces/${spaceID}/works`
      ) {
        return json({
          works:
            url.searchParams.get("needs_you") === "true" && !accepted
              ? [
                  {
                    ...summary(newestWorkID, "Review recommendation"),
                    needs_review: true,
                  },
                ]
              : [],
          has_earlier_works: false,
        });
      }
      if (
        request.method === "GET" &&
        url.pathname === `/v1/spaces/${spaceID}/works/${newestWorkID}`
      ) {
        return json({
          work: accepted
            ? fullWork()
            : { ...fullWork(), needs_review: true, review_id: reviewID },
          messages: [],
          has_earlier_messages: false,
        });
      }
      if (
        request.method === "POST" &&
        url.pathname ===
          `/v1/spaces/${spaceID}/works/${newestWorkID}/reviews/${reviewID}/accept`
      ) {
        acceptanceRequests.push(request);
        accepted = true;
        throw new TypeError("response lost");
      }
      throw new Error(`unexpected request ${request.method} ${url}`);
    }),
  );

  const { result } = renderHook(() => useWorkBoard(user, spaceID));
  await waitFor(() => expect(result.current.spaceID).toBe(spaceID));
  expect(result.current.works).toHaveLength(0);

  await act(() => result.current.showNeedsYou(true));
  expect(result.current.needsYouOnly).toBe(true);
  expect(result.current.works).toHaveLength(1);

  await act(() => result.current.selectWork(newestWorkID));
  expect(result.current.details?.work.review_id).toBe(reviewID);

  await act(() => result.current.acceptCurrentReview());
  expect(result.current.details?.work.needs_review).toBe(false);
  expect(result.current.works).toHaveLength(0);
  expect(acceptanceRequests).toHaveLength(1);
  expect(acceptanceRequests[0]?.headers.get("Idempotency-Key")).toMatch(
    /^[0-9a-f-]{36}$/,
  );
});

function summary(workID: string, goal: string): WorkSummary {
  return {
    work_id: workID,
    space_id: spaceID,
    goal,
    lifecycle: "open",
    owner_user_id: userID,
    owner_display_name: "Alex Morgan",
    creator_user_id: userID,
    creator_display_name: "Alex Morgan",
    has_unapplied_input: false,
    needs_retry: false,
    needs_review: false,
    created_at: "2026-08-20T12:00:00Z",
  };
}

function fullWork(): Work {
  return {
    ...summary(newestWorkID, "Review current renewals"),
    understanding: "The current renewal facts are confirmed.",
    next_step: "Prepare the recommendation.",
  };
}

function workMessage(messageID: string, text: string): WorkMessage {
  return {
    message_id: messageID,
    work_id: newestWorkID,
    author_user_id: userID,
    author_display_name: "Alex Morgan",
    text,
    created_at: "2026-08-20T12:00:00Z",
  };
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
