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
          work: { ...work(), input_head_seq: messageAdded ? 2 : 1 },
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

  expect(createKeys).toHaveLength(2);
  expect(createKeys[0]).toBeTruthy();
  expect(createKeys[1]).toBe(createKeys[0]);
});

function work(id = workID, goal = "Review customer renewals") {
  return {
    work_id: id,
    space_id: spaceID,
    goal,
    lifecycle: "open",
    owner_user_id: "member-1",
    creator_user_id: "member-1",
    input_head_seq: 1,
    created_at: "2026-08-19T00:00:00+08:00",
  };
}

function message() {
  return {
    message_id: messageID,
    work_id: workID,
    author_user_id: "member-1",
    text: "  Renewal date is 30 September  ",
    input_seq: 2,
    created_at: "2026-08-19T00:01:00+08:00",
  };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
