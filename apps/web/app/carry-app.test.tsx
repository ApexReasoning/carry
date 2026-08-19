import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { App } from "./carry-app";

const spaceID = "11111111-1111-4111-8111-111111111111";
const workID = "22222222-2222-4222-8222-222222222222";
const messageID = "33333333-3333-4333-8333-333333333333";
const secondWorkID = "44444444-4444-4444-8444-444444444444";

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  window.history.replaceState(null, "", "/");
});

test("exchanges a member token without storing it and creates durable Work", async () => {
  let sessionEstablished = false;
  let created = false;
  let messageAdded = false;
  const requests: Array<Request> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      requests.push(request.clone());
      const path = new URL(request.url).pathname;

      if (request.method === "GET" && path === "/v1/me") {
        if (!sessionEstablished)
          return json({ error: "member authentication is required" }, 401);
        return json({
          user_id: "member-1",
          spaces: [
            { space_id: spaceID, name: "Research", can_enroll_machines: true },
          ],
        });
      }
      if (request.method === "POST" && path === "/v1/browser/sessions") {
        expect(request.headers.get("Authorization")).toBe(
          "Bearer member-secret",
        );
        sessionEstablished = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "GET" && path === `/v1/spaces/${spaceID}/works`) {
        return json({ works: created ? [work()] : [] });
      }
      if (request.method === "POST" && path === `/v1/spaces/${spaceID}/works`) {
        expect(request.headers.get("Idempotency-Key")).toBeTruthy();
        expect(await request.json()).toEqual({
          goal: "Review customer renewals",
        });
        created = true;
        return json(work());
      }
      if (
        request.method === "POST" &&
        path === `/v1/spaces/${spaceID}/works/${workID}/messages`
      ) {
        expect(request.headers.get("Idempotency-Key")).toBeTruthy();
        expect(await request.json()).toEqual({
          text: "  Renewal date is 30 September  ",
        });
        messageAdded = true;
        return json(message());
      }
      if (
        request.method === "GET" &&
        path === `/v1/spaces/${spaceID}/works/${workID}`
      ) {
        return json({
          work: {
            ...work(),
            has_unapplied_input: !messageAdded,
            understanding: messageAdded
              ? "The renewal date is confirmed as 30 September."
              : "",
            next_step: messageAdded
              ? "Prepare the renewal recommendation."
              : "",
          },
          messages: messageAdded ? [message()] : [],
        });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);

  await user.type(
    await screen.findByLabelText("Member token"),
    "member-secret",
  );
  await user.click(screen.getByRole("button", { name: "Open Carry" }));
  await screen.findByRole("heading", {
    name: "What should Carry keep moving?",
  });

  expect(window.localStorage.length).toBe(0);
  expect(window.sessionStorage.length).toBe(0);
  expect(screen.queryByDisplayValue("member-secret")).not.toBeInTheDocument();

  await user.type(
    screen.getByLabelText("What should Carry take responsibility for?"),
    "Review customer renewals",
  );
  await user.click(screen.getByRole("button", { name: "Create Work" }));
  await screen.findByRole("heading", { name: "Review customer renewals" });

  await user.type(
    screen.getByLabelText("Add information for Carry"),
    "  Renewal date is 30 September  ",
  );
  await user.click(screen.getByRole("button", { name: "Add message" }));
  await screen.findByText("Renewal date is 30 September");
  await screen.findByText("The renewal date is confirmed as 30 September.");
  expect(screen.getByText("Prepare the renewal recommendation.")).toBeVisible();

  await waitFor(() => expect(requests.length).toBeGreaterThanOrEqual(6));
});

test("clears a message draft when the member selects another Work", async () => {
  let sessionEstablished = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;

      if (request.method === "GET" && path === "/v1/me") {
        if (!sessionEstablished)
          return json({ error: "member authentication is required" }, 401);
        return json({
          user_id: "member-1",
          spaces: [
            { space_id: spaceID, name: "Research", can_enroll_machines: true },
          ],
        });
      }
      if (request.method === "POST" && path === "/v1/browser/sessions") {
        sessionEstablished = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "GET" && path === `/v1/spaces/${spaceID}/works`) {
        return json({
          works: [
            work(workID, "Review customer renewals"),
            work(secondWorkID, "Prepare the support themes"),
          ],
        });
      }
      if (
        request.method === "GET" &&
        path === `/v1/spaces/${spaceID}/works/${workID}`
      ) {
        return json({
          work: work(workID, "Review customer renewals"),
          messages: [],
        });
      }
      if (
        request.method === "GET" &&
        path === `/v1/spaces/${spaceID}/works/${secondWorkID}`
      ) {
        return json({
          work: work(secondWorkID, "Prepare the support themes"),
          messages: [],
        });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);
  await user.type(
    await screen.findByLabelText("Member token"),
    "member-secret",
  );
  await user.click(screen.getByRole("button", { name: "Open Carry" }));
  await user.click(
    await screen.findByRole("button", { name: /Review customer renewals/ }),
  );
  await screen.findByRole("heading", { name: "Review customer renewals" });
  await user.type(
    screen.getByLabelText("Add information for Carry"),
    "Private draft for renewals",
  );

  await user.click(
    screen.getByRole("button", { name: /Prepare the support themes/ }),
  );
  await screen.findByRole("heading", { name: "Prepare the support themes" });

  expect(screen.getByLabelText("Add information for Carry")).toHaveValue("");
  expect(
    screen.queryByDisplayValue("Private draft for renewals"),
  ).not.toBeInTheDocument();
});

test("reuses the same Work identity after a create response is lost", async () => {
  let sessionEstablished = false;
  const createKeys: Array<string | null> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;

      if (request.method === "GET" && path === "/v1/me") {
        if (!sessionEstablished)
          return json({ error: "member authentication is required" }, 401);
        return json({
          user_id: "member-1",
          spaces: [
            { space_id: spaceID, name: "Research", can_enroll_machines: true },
          ],
        });
      }
      if (request.method === "POST" && path === "/v1/browser/sessions") {
        sessionEstablished = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "GET" && path === `/v1/spaces/${spaceID}/works`) {
        return json({ works: [] });
      }
      if (request.method === "POST" && path === `/v1/spaces/${spaceID}/works`) {
        createKeys.push(request.headers.get("Idempotency-Key"));
        if (createKeys.length === 1) {
          throw new TypeError("response was lost");
        }
        return json(work());
      }
      if (
        request.method === "GET" &&
        path === `/v1/spaces/${spaceID}/works/${workID}`
      ) {
        return json({
          work: {
            ...work(),
            understanding: "The saved renewal facts are current.",
            next_step: "Review the existing member message.",
            has_unapplied_input: false,
          },
          messages: [message()],
        });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);
  await user.type(
    await screen.findByLabelText("Member token"),
    "member-secret",
  );
  await user.click(screen.getByRole("button", { name: "Open Carry" }));
  await screen.findByRole("heading", {
    name: "What should Carry keep moving?",
  });

  const goal = screen.getByLabelText(
    "What should Carry take responsibility for?",
  );
  await user.type(goal, "Review customer renewals");
  await user.click(screen.getByRole("button", { name: "Create Work" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Create Work outcome is unknown; retry the same command to reconcile",
  );
  expect(goal).toHaveValue("Review customer renewals");

  await user.click(screen.getByRole("button", { name: "Create Work" }));
  await screen.findByRole("heading", { name: "Review customer renewals" });
  await screen.findByText("The saved renewal facts are current.");
  await screen.findByText("Renewal date is 30 September");

  expect(createKeys).toHaveLength(2);
  expect(createKeys[0]).toBeTruthy();
  expect(createKeys[1]).toBe(createKeys[0]);
});

test("reuses a pending Work identity after remount", async () => {
  let sessionEstablished = false;
  const createKeys: Array<string | null> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        if (!sessionEstablished)
          return json({ error: "authentication required" }, 401);
        return json({
          user_id: "member-1",
          spaces: [
            { space_id: spaceID, name: "Research", can_enroll_machines: true },
          ],
        });
      }
      if (request.method === "POST" && path === "/v1/browser/sessions") {
        sessionEstablished = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "GET" && path === `/v1/spaces/${spaceID}/works`)
        return json({ works: [] });
      if (request.method === "POST" && path === `/v1/spaces/${spaceID}/works`) {
        createKeys.push(request.headers.get("Idempotency-Key"));
        if (createKeys.length === 1) throw new TypeError("response lost");
        return json(work());
      }
      if (
        request.method === "GET" &&
        path === `/v1/spaces/${spaceID}/works/${workID}`
      ) {
        return json({ work: work(), messages: [] });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );
  const user = userEvent.setup();
  const first = render(<App />);
  await user.type(
    await screen.findByLabelText("Member token"),
    "member-secret",
  );
  await user.click(screen.getByRole("button", { name: "Open Carry" }));
  const goal = await screen.findByLabelText(
    "What should Carry take responsibility for?",
  );
  await user.type(goal, "Review customer renewals");
  await user.click(screen.getByRole("button", { name: "Create Work" }));
  await screen.findByRole("alert");
  const stored =
    window.sessionStorage.getItem("carry.pending-work-mutations.v1") ?? "";
  expect(stored).not.toContain("Review customer renewals");
  expect(stored).not.toContain("member-secret");
  first.unmount();

  render(<App />);
  const restoredGoal = await screen.findByLabelText(
    "What should Carry take responsibility for?",
  );
  await user.type(restoredGoal, "Review customer renewals");
  await user.click(screen.getByRole("button", { name: "Create Work" }));
  await screen.findByRole("heading", { name: "Review customer renewals" });
  expect(createKeys[1]).toBe(createKeys[0]);
});

test("requires an explicit Space choice when several are available", async () => {
  let sessionEstablished = false;
  const workLists: Array<string> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        if (!sessionEstablished)
          return json({ error: "authentication required" }, 401);
        return json({
          user_id: "member-1",
          spaces: [
            { space_id: spaceID, name: "Research", can_enroll_machines: true },
            {
              space_id: secondWorkID,
              name: "Operations",
              can_enroll_machines: false,
            },
          ],
        });
      }
      if (request.method === "POST" && path === "/v1/browser/sessions") {
        sessionEstablished = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "GET" && path.endsWith("/works")) {
        workLists.push(path);
        return json({ works: [] });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );
  const user = userEvent.setup();
  render(<App />);
  await user.type(
    await screen.findByLabelText("Member token"),
    "member-secret",
  );
  await user.click(screen.getByRole("button", { name: "Open Carry" }));
  await screen.findByText("Choose a Space before opening shared Work.");
  expect(workLists).toEqual([]);
  await user.selectOptions(screen.getByLabelText("Space"), secondWorkID);
  await waitFor(() =>
    expect(workLists).toEqual([`/v1/spaces/${secondWorkID}/works`]),
  );
});

test("keeps Work hidden across an unconfirmed sign-out reload", async () => {
  let finishSignOut = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return json({
          user_id: "member-1",
          spaces: [
            {
              space_id: spaceID,
              name: "Research",
              can_enroll_machines: true,
            },
          ],
        });
      }
      if (request.method === "GET" && path.endsWith("/works")) {
        return json({ works: [work()] });
      }
      if (
        request.method === "DELETE" &&
        path === "/v1/browser/sessions/current"
      ) {
        if (!finishSignOut) throw new TypeError("response lost");
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );
  const user = userEvent.setup();
  const first = render(<App />);
  await screen.findByText("Review customer renewals");
  const storageWrite = vi
    .spyOn(window.sessionStorage, "setItem")
    .mockImplementation(() => {
      throw new DOMException("storage unavailable", "QuotaExceededError");
    });

  await user.click(screen.getByRole("button", { name: "Sign out" }));
  expect(
    screen.queryByText("Review customer renewals"),
  ).not.toBeInTheDocument();
  await screen.findByText("Your Work is hidden on this browser.");
  await screen.findByText(/Sign-out revocation is not confirmed/);
  expect(
    new URL(window.location.href).searchParams.get("carry-signing-out"),
  ).toBe("1");

  first.unmount();
  render(<App />);
  await screen.findByText("Your Work is hidden on this browser.");
  expect(
    screen.queryByText("Review customer renewals"),
  ).not.toBeInTheDocument();
  finishSignOut = true;
  await user.click(screen.getByRole("button", { name: "Finish signing out" }));
  await screen.findByLabelText("Member token");
  expect(
    new URL(window.location.href).searchParams.get("carry-signing-out"),
  ).toBeNull();
  storageWrite.mockRestore();
});

test("falls back from a failed URL latch without reopening Work", async () => {
  let finishSignOut = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return json({
          user_id: "member-1",
          spaces: [
            {
              space_id: spaceID,
              name: "Research",
              can_enroll_machines: true,
            },
          ],
        });
      }
      if (request.method === "GET" && path.endsWith("/works")) {
        return json({ works: [work()] });
      }
      if (
        request.method === "DELETE" &&
        path === "/v1/browser/sessions/current"
      ) {
        if (!finishSignOut) throw new TypeError("response lost");
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  const first = render(<App />);
  await screen.findByText("Review customer renewals");
  const historyWrite = vi
    .spyOn(window.history, "replaceState")
    .mockImplementation(() => {
      throw new DOMException("history unavailable", "SecurityError");
    });

  await user.click(screen.getByRole("button", { name: "Sign out" }));
  await screen.findByText(/Sign-out revocation is not confirmed/);
  expect(window.sessionStorage.getItem("carry.pending-sign-out.v1")).toBe("1");
  expect(
    screen.queryByText("Review customer renewals"),
  ).not.toBeInTheDocument();

  first.unmount();
  render(<App />);
  await screen.findByText("Your Work is hidden on this browser.");
  expect(
    screen.queryByText("Review customer renewals"),
  ).not.toBeInTheDocument();

  finishSignOut = true;
  await user.click(screen.getByRole("button", { name: "Finish signing out" }));
  await screen.findByLabelText("Member token");
  expect(window.sessionStorage.getItem("carry.pending-sign-out.v1")).toBeNull();
  historyWrite.mockRestore();
});

test("reconciles a lost Work retry response by reloading the Work", async () => {
  let retryRequested = false;
  const retryKeys: Array<string | null> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return json({
          user_id: "member-1",
          spaces: [
            {
              space_id: spaceID,
              name: "Research",
              can_enroll_machines: true,
            },
          ],
        });
      }
      if (request.method === "GET" && path.endsWith("/works")) {
        return json({ works: [{ ...work(), needs_retry: true }] });
      }
      if (
        request.method === "GET" &&
        path === `/v1/spaces/${spaceID}/works/${workID}`
      ) {
        return json({
          work: { ...work(), needs_retry: !retryRequested },
          messages: [],
        });
      }
      if (
        request.method === "POST" &&
        path === `/v1/spaces/${spaceID}/works/${workID}/retry`
      ) {
        retryKeys.push(request.headers.get("Idempotency-Key"));
        retryRequested = true;
        throw new TypeError("response lost");
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );
  const user = userEvent.setup();
  render(<App />);
  await user.click(
    await screen.findByRole("button", { name: /Review customer renewals/ }),
  );
  await user.click(await screen.findByRole("button", { name: "Try again" }));

  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull(),
  );
  expect(retryKeys).toHaveLength(1);
  expect(retryKeys[0]).toBeTruthy();
});

test("reconciles an old retry identity before authorizing a later terminal Run", async () => {
  const retryKeys: Array<string | null> = [];
  let loadCount = 0;
  let needsRetry = true;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return json({
          user_id: "member-1",
          spaces: [
            {
              space_id: spaceID,
              name: "Research",
              can_enroll_machines: true,
            },
          ],
        });
      }
      if (request.method === "GET" && path.endsWith("/works")) {
        return json({ works: [{ ...work(), needs_retry: true }] });
      }
      if (
        request.method === "GET" &&
        path === `/v1/spaces/${spaceID}/works/${workID}`
      ) {
        loadCount += 1;
        if (loadCount === 2) throw new TypeError("reconciliation lost");
        return json({
          work: { ...work(), needs_retry: needsRetry },
          messages: [],
        });
      }
      if (
        request.method === "POST" &&
        path === `/v1/spaces/${spaceID}/works/${workID}/retry`
      ) {
        retryKeys.push(request.headers.get("Idempotency-Key"));
        if (retryKeys.length === 1) {
          throw new TypeError("retry response lost");
        }
        if (retryKeys.length === 3) needsRetry = false;
        return new Response(null, { status: 204 });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  const first = render(<App />);
  await user.click(
    await screen.findByRole("button", { name: /Review customer renewals/ }),
  );
  await user.click(await screen.findByRole("button", { name: "Try again" }));
  await screen.findByText(/reconciliation lost/);
  first.unmount();

  render(<App />);
  await user.click(
    await screen.findByRole("button", { name: /Review customer renewals/ }),
  );
  await user.click(await screen.findByRole("button", { name: "Try again" }));
  await screen.findByText(/needs a new choice/);
  expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Try again" }));
  await waitFor(() =>
    expect(screen.queryByRole("button", { name: "Try again" })).toBeNull(),
  );
  expect(retryKeys).toHaveLength(3);
  expect(retryKeys[0]).toBeTruthy();
  expect(retryKeys[1]).toBe(retryKeys[0]);
  expect(retryKeys[2]).not.toBe(retryKeys[0]);
});

test("reconciles an unknown browser-session exchange", async () => {
  let sessionEstablished = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "POST" && path === "/v1/browser/sessions") {
        sessionEstablished = true;
        throw new TypeError("response lost");
      }
      if (request.method === "GET" && path === "/v1/me") {
        if (!sessionEstablished)
          return json({ error: "authentication required" }, 401);
        return json({
          user_id: "member-1",
          spaces: [
            { space_id: spaceID, name: "Research", can_enroll_machines: true },
          ],
        });
      }
      if (request.method === "GET" && path === `/v1/spaces/${spaceID}/works`)
        return json({ works: [] });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );
  const user = userEvent.setup();
  render(<App />);
  await user.type(
    await screen.findByLabelText("Member token"),
    "member-secret",
  );
  await user.click(screen.getByRole("button", { name: "Open Carry" }));
  await screen.findByRole("heading", {
    name: "What should Carry keep moving?",
  });
});

function work(id = workID, goal = "Review customer renewals") {
  return {
    work_id: id,
    space_id: spaceID,
    goal,
    lifecycle: "open",
    owner_user_id: "member-1",
    creator_user_id: "member-1",
    understanding: "",
    has_unapplied_input: true,
    needs_retry: false,
    next_step: "",
    created_at: "2026-08-19T00:00:00+08:00",
  };
}

function message() {
  return {
    message_id: messageID,
    work_id: workID,
    author_user_id: "member-1",
    text: "  Renewal date is 30 September  ",
    created_at: "2026-08-19T00:01:00+08:00",
  };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
