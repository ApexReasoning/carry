import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { ConversationPanel } from "./conversation-panel";
import { pendingConversationRequestID } from "./conversation-pending";

const memberID = "member-1";
const spaceID = "11111111-1111-4111-8111-111111111111";
const secondSpaceID = "22222222-2222-4222-8222-222222222222";
const memberMessageID = "33333333-3333-4333-8333-333333333333";
const carryMessageID = "44444444-4444-4444-8444-444444444444";

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
});

test("sends with Enter, disables the pending composer, and polls only after the member message", async () => {
  const listQueries: Array<string> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const url = new URL(request.url);
      if (request.method === "GET") {
        listQueries.push(url.search);
        if (url.searchParams.get("after") === memberMessageID) {
          return json({
            messages: [
              privateMessage(
                carryMessageID,
                "carry",
                "The renewal window closes Friday.",
              ),
            ],
          });
        }
        return json({ messages: [] });
      }
      if (request.method === "POST") {
        const requestID = request.headers.get("Idempotency-Key")!;
        expect(await request.json()).toEqual({
          text: "When does the renewal window close?",
        });
        return json({
          ...privateMessage(
            memberMessageID,
            "member",
            "When does the renewal window close?",
          ),
          request_id: requestID,
        });
      }
      throw new Error(`unexpected request ${request.method}`);
    }),
  );

  const user = userEvent.setup();
  renderPanel();
  await screen.findByText(/Ask a question or give Carry a responsibility/);
  const composer = await screen.findByLabelText("Message Carry privately");
  await user.type(composer, "When does the renewal window close?{Enter}");

  await screen.findByText("When does the renewal window close?");
  expect(composer).toBeDisabled();
  expect(screen.getByText(/Waiting for Carry’s reply/)).toBeVisible();
  expect(window.sessionStorage.length).toBe(0);

  await screen.findByText(
    "The renewal window closes Friday.",
    {},
    { timeout: 2_500 },
  );
  expect(composer).toBeEnabled();
  expect(listQueries).toEqual(["", `?after=${memberMessageID}`]);
});

test("reconciles response loss only when newest messages contain the exact request id", async () => {
  const privateText = "Which launch risk needs attention?";
  let admittedRequestID: string | null = null;
  let posts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      if (request.method === "GET") {
        return json({
          messages: admittedRequestID
            ? [
                {
                  ...privateMessage(memberMessageID, "member", privateText),
                  request_id: admittedRequestID,
                },
              ]
            : [],
        });
      }
      posts += 1;
      admittedRequestID = request.headers.get("Idempotency-Key");
      throw new TypeError("response lost after admission");
    }),
  );

  const user = userEvent.setup();
  renderPanel();
  const composer = await screen.findByLabelText("Message Carry privately");
  await user.type(composer, privateText);
  await user.click(screen.getByRole("button", { name: "Send privately" }));

  await screen.findByText(privateText);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(composer).toBeDisabled();
  expect(posts).toBe(1);
  expect(window.sessionStorage.length).toBe(0);
});

test("retains and reuses the exact random request key after response loss", async () => {
  const privateText = "Keep this launch detail private";
  const requestKeys: Array<string | null> = [];
  let posts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      if (request.method === "GET") return json({ messages: [] });
      posts += 1;
      requestKeys.push(request.headers.get("Idempotency-Key"));
      if (posts === 1) throw new TypeError("response lost");
      return json({
        ...privateMessage(memberMessageID, "member", privateText),
        request_id: requestKeys[0],
      });
    }),
  );

  const user = userEvent.setup();
  renderPanel();
  const composer = await screen.findByLabelText("Message Carry privately");
  await user.type(composer, privateText);
  await user.click(screen.getByRole("button", { name: "Send privately" }));
  await screen.findByText(/may have finished, but Carry could not confirm it/);

  const digest = Array.from(
    new Uint8Array(
      await crypto.subtle.digest(
        "SHA-256",
        new TextEncoder().encode(privateText),
      ),
    ),
    (value) => value.toString(16).padStart(2, "0"),
  ).join("");
  const stored = storageContents(window.sessionStorage);
  expect(stored).not.toContain(privateText);
  expect(stored).not.toContain(digest);
  expect(window.localStorage.length).toBe(0);
  expect(composer).toHaveValue(privateText);

  await user.click(screen.getByRole("button", { name: "Send privately" }));
  await screen.findByText(privateText);
  expect(requestKeys).toHaveLength(2);
  expect(requestKeys[0]).toBeTruthy();
  expect(requestKeys[1]).toBe(requestKeys[0]);
  expect(window.sessionStorage.length).toBe(0);
});

test("reuses a pending key after remount without persisting the private draft", async () => {
  const privateText = "Private plan that must be retyped";
  const requestKeys: Array<string | null> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      if (request.method === "GET") return json({ messages: [] });
      requestKeys.push(request.headers.get("Idempotency-Key"));
      if (requestKeys.length === 1) throw new TypeError("response lost");
      return json({
        ...privateMessage(memberMessageID, "member", privateText),
        request_id: requestKeys[0],
      });
    }),
  );

  const user = userEvent.setup();
  const first = renderPanel();
  const firstComposer = await screen.findByLabelText("Message Carry privately");
  await user.type(firstComposer, privateText);
  await user.click(screen.getByRole("button", { name: "Send privately" }));
  await screen.findByText(/may have finished, but Carry could not confirm it/);
  expect(storageContents(window.sessionStorage)).not.toContain(privateText);
  first.unmount();

  renderPanel();
  const restoredComposer = await screen.findByLabelText(
    "Message Carry privately",
  );
  expect(restoredComposer).toHaveValue("");
  await user.type(restoredComposer, privateText);
  await user.click(screen.getByRole("button", { name: "Send privately" }));
  await screen.findByText(privateText);
  expect(requestKeys[1]).toBe(requestKeys[0]);
});

test("requires explicit recovery before discarding a damaged request identity", async () => {
  const storageKey = `carry.pending-conversation.v1:${encodeURIComponent(memberID)}:${encodeURIComponent(spaceID)}`;
  window.sessionStorage.setItem(storageKey, "damaged");
  let posts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      if (request.method === "GET") return json({ messages: [] });
      posts += 1;
      return json({});
    }),
  );

  renderPanel();
  const composer = await screen.findByLabelText("Message Carry privately");
  expect(composer).toBeDisabled();
  expect(window.sessionStorage.getItem(storageKey)).toBe("damaged");
  expect(posts).toBe(0);

  await userEvent.setup().click(
    screen.getByRole("button", {
      name: "Discard damaged message identity",
    }),
  );

  expect(window.sessionStorage.getItem(storageKey)).toBeNull();
  expect(composer).toBeEnabled();
  expect(posts).toBe(0);
});

test("does not send when a pending request key cannot be persisted", async () => {
  let posts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      if (request.method === "GET") return json({ messages: [] });
      posts += 1;
      return json({});
    }),
  );

  const user = userEvent.setup();
  renderPanel();
  const composer = await screen.findByLabelText("Message Carry privately");
  const save = vi
    .spyOn(window.sessionStorage, "setItem")
    .mockImplementation(() => {
      throw new DOMException("storage unavailable", "QuotaExceededError");
    });
  await user.type(composer, "Do not send without a durable key");
  await user.click(screen.getByRole("button", { name: "Send privately" }));
  save.mockRestore();

  await screen.findByText("storage unavailable");
  expect(posts).toBe(0);
  expect(composer).toHaveValue("Do not send without a durable key");
});

test("clears a remounted pending key only when newest messages prove its request id", async () => {
  const requestID = pendingConversationRequestID(memberID, spaceID);
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      json({
        messages: [
          {
            ...privateMessage(memberMessageID, "member", "Admitted privately"),
            request_id: requestID,
          },
        ],
      }),
    ),
  );

  renderPanel();
  await screen.findByText("Admitted privately");
  expect(window.sessionStorage.length).toBe(0);
  expect(screen.getByLabelText("Message Carry privately")).toBeDisabled();
});

test("prepends before pages and appends after polling without a full-history reload", async () => {
  const newest = Array.from({ length: 50 }, (_, index) =>
    numberedMessage(index + 51),
  );
  const fullLoads: Array<string> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const url = new URL(request.url);
      fullLoads.push(url.search);
      if (url.searchParams.has("before")) {
        return json({ messages: [numberedMessage(49), numberedMessage(50)] });
      }
      if (url.searchParams.has("after")) {
        return json({ messages: [numberedMessage(101)] });
      }
      return json({ messages: newest });
    }),
  );

  const user = userEvent.setup();
  renderPanel();
  await screen.findByText("private message 51");
  await user.click(screen.getByRole("button", { name: "Load earlier" }));
  await screen.findByText("private message 49");
  await screen.findByText("private message 101", {}, { timeout: 2_500 });

  expect(screen.getAllByText("private message 51")).toHaveLength(1);
  expect(fullLoads.filter((query) => query === "")).toHaveLength(1);
  expect(fullLoads.some((query) => query.startsWith("?before="))).toBe(true);
  expect(fullLoads.some((query) => query.startsWith("?after="))).toBe(true);
});

test("replaces a stale private request identity after an authoritative conflict", async () => {
  const staleRequestID = pendingConversationRequestID(memberID, spaceID);
  const requestIDs: Array<string> = [];
  let posts = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      if (request.method === "GET") {
        return json({
          messages: Array.from({ length: 50 }, (_, index) =>
            numberedMessage(index + 50),
          ),
        });
      }
      posts += 1;
      const requestID = request.headers.get("Idempotency-Key") ?? "";
      requestIDs.push(requestID);
      if (posts === 1) {
        return new Response(
          JSON.stringify({
            error: "idempotency key refers to different private input",
          }),
          { status: 409, headers: { "Content-Type": "application/json" } },
        );
      }
      return json({
        ...privateMessage(
          memberMessageID,
          "member",
          "Send the current question",
        ),
        request_id: requestID,
      });
    }),
  );

  const user = userEvent.setup();
  renderPanel();
  const composer = await screen.findByLabelText("Message Carry privately");
  await user.type(composer, "Send the current question");
  await user.click(screen.getByRole("button", { name: "Send privately" }));

  await screen.findByText(/changed in another browser/);
  expect(window.sessionStorage.length).toBe(0);
  expect(requestIDs).toEqual([staleRequestID]);

  await user.click(screen.getByRole("button", { name: "Send privately" }));
  await screen.findByText("Send the current question");
  expect(requestIDs).toHaveLength(2);
  expect(requestIDs[1]).not.toBe(staleRequestID);
});

test("resets the visible private transcript when the selected Space changes", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      return path.includes(spaceID)
        ? json({
            messages: [
              privateMessage(carryMessageID, "carry", "Research private"),
            ],
          })
        : json({
            messages: [
              privateMessage(memberMessageID, "carry", "Operations private"),
            ],
          });
    }),
  );

  const view = renderPanel();
  await screen.findByText("Research private");
  view.rerender(
    <ConversationPanel
      key={secondSpaceID}
      memberID={memberID}
      spaceID={secondSpaceID}
      workBusy={false}
      onOpenWork={async () => {}}
    />,
  );
  await screen.findByText("Operations private");
  expect(screen.queryByText("Research private")).not.toBeInTheDocument();
});

function renderPanel() {
  return render(
    <ConversationPanel
      key={spaceID}
      memberID={memberID}
      spaceID={spaceID}
      workBusy={false}
      onOpenWork={async () => {}}
    />,
  );
}

function privateMessage(
  messageID: string,
  author: "member" | "carry",
  text: string,
) {
  return {
    message_id: messageID,
    author,
    text,
    created_at: "2026-08-20T12:00:00Z",
  };
}

function numberedMessage(number: number) {
  const suffix = String(number).padStart(12, "0");
  return privateMessage(
    `00000000-0000-4000-8000-${suffix}`,
    number % 2 === 0 ? "member" : "carry",
    `private message ${number}`,
  );
}

function storageContents(storage: Storage): string {
  return Array.from({ length: storage.length }, (_, index) => {
    const key = storage.key(index)!;
    return `${key}:${storage.getItem(key)}`;
  }).join("|");
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}
