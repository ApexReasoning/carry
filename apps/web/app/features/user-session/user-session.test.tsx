import {
  act,
  render,
  renderHook,
  screen,
  waitFor,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { App } from "../../carry-app";
import { useUserSession } from "./use-user-session";

const userID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const actualSpaceID = "11111111-1111-4111-8111-111111111111";

beforeEach(() => {
  window.localStorage.clear();
  window.sessionStorage.clear();
  window.history.replaceState(null, "", "/");
});

test("offers Google GitHub and email without implying account linking", async () => {
  mockUnauthenticatedEmail(async () => null);

  render(<App />);

  const google = await screen.findByRole("button", {
    name: "Continue with Google",
  });
  const github = screen.getByRole("button", { name: "Continue with GitHub" });
  expect(google.closest("form")).toHaveAttribute(
    "action",
    "/v1/auth/google/start",
  );
  expect(google.closest("form")).toHaveAttribute("method", "post");
  expect(github.closest("form")).toHaveAttribute(
    "action",
    "/v1/auth/github/start",
  );
  expect(
    screen.getByText(/Google, GitHub, and email accounts are not combined yet/),
  ).toBeVisible();
  expect(screen.getByLabelText("Email")).toBeVisible();
});

test("signed-out exact invitation reveals no metadata and carries only its ID", async () => {
  const invitationID = "22222222-2222-4222-8222-222222222222";
  window.history.replaceState(null, "", `/invitations/${invitationID}`);
  const paths: Array<string> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      paths.push(new URL(request.url).pathname);
      if (
        request.method === "GET" &&
        new URL(request.url).pathname === "/v1/me"
      )
        return json(
          { error: "Browser Session authentication is required" },
          401,
        );
      throw new Error(`unexpected preview ${request.method} ${request.url}`);
    }),
  );

  render(<App />);

  expect(
    await screen.findByRole("heading", {
      name: "Sign in to review this invitation",
    }),
  ).toBeVisible();
  expect(paths).toEqual(["/v1/me"]);
  expect(screen.queryByText(/Research|Manager|manage members/)).toBeNull();
  for (const name of ["Continue with Google", "Continue with GitHub"]) {
    const form = screen.getByRole("button", { name }).closest("form");
    expect(form?.querySelector('input[name="invitation_id"]')).toHaveValue(
      invitationID,
    );
  }
  expect(window.localStorage.length).toBe(0);
  expect(window.sessionStorage.length).toBe(0);
});

test("malformed exact invitation path fails before authentication or preview", async () => {
  window.history.replaceState(null, "", "/invitations/%E0%A4%A");
  const fetch = vi.fn(async () => unauthenticated());
  vi.stubGlobal("fetch", fetch);

  render(<App />);

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "This invitation link is invalid.",
  );
  expect(fetch).not.toHaveBeenCalled();

  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Return to Carry home" }));
  expect(window.location.pathname).toBe("/");
  expect(
    await screen.findByRole("heading", {
      name: "Work that stays in good hands.",
    }),
  ).toBeVisible();
  expect(fetch).toHaveBeenCalledOnce();
});

test("targeted invitation loading and read failure stay explicit", async () => {
  const invitationID = "22222222-2222-4222-8222-222222222222";
  window.history.replaceState(null, "", `/invitations/${invitationID}`);
  let resolveInvitation: (response: Response) => void = () => undefined;
  const invitationResponse = new Promise<Response>((resolve) => {
    resolveInvitation = resolve;
  });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return json(currentUser([]));
      }
      if (
        request.method === "GET" &&
        path === `/v1/invitations/${invitationID}`
      ) {
        return invitationResponse;
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  render(<App />);

  expect(await screen.findByText("Loading invitation…")).toBeVisible();
  resolveInvitation(json({ error: "targeted read failed" }, 500));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "targeted read failed",
  );
  expect(
    screen.queryByText("Confirm your Email to review this invitation."),
  ).not.toBeInTheDocument();
});

test("targeted invitation method lookup failure becomes an explicit error", async () => {
  const invitationID = "22222222-2222-4222-8222-222222222222";
  window.history.replaceState(null, "", `/invitations/${invitationID}`);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return json(currentUser([]));
      }
      if (
        request.method === "GET" &&
        path === `/v1/invitations/${invitationID}`
      ) {
        return json({ error: "Space invitation is unavailable" }, 404);
      }
      if (request.method === "GET" && path === "/v1/identity/methods") {
        return json({ error: "method lookup failed" }, 500);
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  render(<App />);

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "method lookup failed",
  );
  expect(
    screen.queryByText("Confirm your Email to review this invitation."),
  ).not.toBeInTheDocument();
});

test("provider outcome cleanup preserves the exact invitation path", async () => {
  const invitationID = "22222222-2222-4222-8222-222222222222";
  window.history.replaceState(
    null,
    "",
    `/invitations/${invitationID}?sign_in=unavailable`,
  );
  mockUnauthenticatedEmail(async () => null);

  render(<App />);

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Carry could not confirm sign-in. Start a fresh sign-in.",
  );
  expect(window.location.pathname).toBe(`/invitations/${invitationID}`);
  expect(window.location.search).toBe("");
});

test("shows a neutral provider callback outcome and removes it from the URL", async () => {
  window.history.replaceState(null, "", "/?sign_in=unavailable");
  mockUnauthenticatedEmail(async () => null);

  render(<App />);

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Carry could not confirm sign-in. Start a fresh sign-in.",
  );
  expect(window.location.search).toBe("");
  expect(window.localStorage.length).toBe(0);
  expect(window.sessionStorage.length).toBe(0);
});

test("pending invitation outranks a current Space but Not now defers without restoring the session", async () => {
  window.history.replaceState(null, "", "/s/research");
  let currentUserCalls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        currentUserCalls += 1;
        return json(
          currentUser([
            {
              space_id: actualSpaceID,
              name: "Research",
              slug: "research",
              can_manage_members: true,
              can_enroll_machines: true,
            },
          ]),
        );
      }
      if (request.method === "GET" && path === "/v1/invitations") {
        return json({
          invitations: [
            {
              invitation_id: "22222222-2222-4222-8222-222222222222",
              space_id: "33333333-3333-4333-8333-333333333333",
              space_name: "Operations",
              inviter_display_name: "Member bbbbbbbb",
              can_manage_members: false,
              can_enroll_machines: false,
              created_at: "2026-08-21T00:00:00Z",
              expires_at: "2026-08-28T00:00:00Z",
            },
          ],
          reauthentication_required: false,
        });
      }
      if (request.method === "GET" && path.endsWith("/conversation/messages"))
        return json({ messages: [] });
      if (request.method === "GET" && path.endsWith("/works"))
        return json({ works: [], has_earlier_works: false });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  const firstMount = render(<App />);

  expect(
    await screen.findByRole("heading", { name: "Space invitations" }),
  ).toBeVisible();
  expect(screen.getByText("Operations")).toBeVisible();
  expect(
    screen.queryByRole("heading", { name: "What should Carry keep moving?" }),
  ).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Not now" }));
  expect(window.location.pathname).toBe("/");
  expect(
    await screen.findByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  expect(screen.getByRole("link", { name: /Research/ })).toBeVisible();
  expect(screen.queryByText("Opening Carry…")).not.toBeInTheDocument();
  expect(currentUserCalls).toBe(1);
  expect(window.sessionStorage).toHaveLength(1);
  expect(window.sessionStorage.getItem("carry.deferred-invitations.v1")).toBe(
    userID,
  );

  firstMount.unmount();
  window.history.replaceState(null, "", "/s/research");
  render(<App />);
  expect(
    await screen.findByRole("heading", {
      name: "What should Carry keep moving?",
    }),
  ).toBeVisible();
  expect(currentUserCalls).toBe(2);
});

test("a foreign or malformed invitation deferral never hides this User's invitation", async () => {
  for (const stored of [
    "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
    "not-a-user-id",
  ]) {
    window.sessionStorage.setItem("carry.deferred-invitations.v1", stored);
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        const path = new URL(request.url).pathname;
        if (request.method === "GET" && path === "/v1/me")
          return json(currentUser([]));
        if (request.method === "GET" && path === "/v1/invitations") {
          return json({
            invitations: [
              {
                invitation_id: "22222222-2222-4222-8222-222222222222",
                space_id: "33333333-3333-4333-8333-333333333333",
                space_name: "Operations",
                inviter_display_name: "Member bbbbbbbb",
                can_manage_members: false,
                can_enroll_machines: false,
                created_at: "2026-08-21T00:00:00Z",
                expires_at: "2026-08-28T00:00:00Z",
              },
            ],
            reauthentication_required: false,
          });
        }
        throw new Error(`unexpected request: ${request.method} ${path}`);
      }),
    );

    const mounted = renderHook(() => useUserSession());
    await waitFor(() =>
      expect(mounted.result.current.phase).toBe("invitations"),
    );
    mounted.unmount();
  }
  expect(window.sessionStorage).toHaveLength(0);
});

test("an explicit invitation inbox clears and bypasses the current User's deferral", async () => {
  window.sessionStorage.setItem("carry.deferred-invitations.v1", userID);
  window.history.replaceState(null, "", "/invitations");
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me")
        return json(currentUser([]));
      if (request.method === "GET" && path === "/v1/invitations")
        return json({ invitations: [], reauthentication_required: false });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const { result } = renderHook(() => useUserSession());
  await waitFor(() => expect(result.current.phase).toBe("invitations"));
  expect(window.sessionStorage).toHaveLength(0);
});

test("Not now leaves a terminal invitation even when session storage is unavailable", async () => {
  const invitationID = "22222222-2222-4222-8222-222222222222";
  window.history.replaceState(null, "", `/invitations/${invitationID}`);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me")
        return json(currentUser([]));
      if (
        request.method === "GET" &&
        path === `/v1/invitations/${invitationID}`
      ) {
        return json({
          invitation_id: invitationID,
          space_id: "33333333-3333-4333-8333-333333333333",
          space_name: "Operations",
          inviter_display_name: "Member bbbbbbbb",
          can_manage_members: false,
          can_enroll_machines: false,
          created_at: "2026-08-21T00:00:00Z",
          expires_at: "2026-08-28T00:00:00Z",
          state: "accepted",
          accept_result: "joined",
          current_member: true,
          reauthentication_required: false,
        });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const { result } = renderHook(() => useUserSession());
  await waitFor(() => expect(result.current.phase).toBe("invitation"));
  const storageFailure = vi
    .spyOn(window.sessionStorage, "setItem")
    .mockImplementation(() => {
      throw new Error("storage unavailable");
    });
  act(() => result.current.skipInvitations());
  expect(result.current.phase).toBe("ready");
  expect(window.location.pathname).toBe("/");
  storageFailure.mockRestore();
});

test("Not now stays on a pending invitation when session deferral cannot be confirmed", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me")
        return json(currentUser([]));
      if (request.method === "GET" && path === "/v1/invitations") {
        return json({
          invitations: [
            {
              invitation_id: "22222222-2222-4222-8222-222222222222",
              space_id: "33333333-3333-4333-8333-333333333333",
              space_name: "Operations",
              inviter_display_name: "Member bbbbbbbb",
              can_manage_members: false,
              can_enroll_machines: false,
              created_at: "2026-08-21T00:00:00Z",
              expires_at: "2026-08-28T00:00:00Z",
            },
          ],
          reauthentication_required: false,
        });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );
  const storageFailure = vi
    .spyOn(window.sessionStorage, "setItem")
    .mockImplementationOnce(() => {
      throw new Error("storage unavailable");
    });

  const { result } = renderHook(() => useUserSession());
  await waitFor(() => expect(result.current.phase).toBe("invitations"));
  act(() => result.current.skipInvitations());
  expect(result.current.phase).toBe("invitations");
  expect(result.current.error).toContain(
    "Carry could not defer this invitation in the current tab",
  );
  storageFailure.mockRestore();
});

test("invitation deferral is cleared before a different User signs in", async () => {
  let signedOut = false;
  let secondSignedIn = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        if (signedOut && !secondSignedIn) return unauthenticated();
        return json(currentUser([]));
      }
      if (request.method === "GET" && path === "/v1/invitations") {
        return json({
          invitations: [
            {
              invitation_id: "22222222-2222-4222-8222-222222222222",
              space_id: "33333333-3333-4333-8333-333333333333",
              space_name: "Operations",
              inviter_display_name: "Member bbbbbbbb",
              can_manage_members: false,
              can_enroll_machines: false,
              created_at: "2026-08-21T00:00:00Z",
              expires_at: "2026-08-28T00:00:00Z",
            },
          ],
          reauthentication_required: false,
        });
      }
      if (
        request.method === "DELETE" &&
        path === "/v1/browser/sessions/current"
      ) {
        signedOut = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "POST" && path === "/v1/auth/email/challenges")
        return acceptedChallenge(request);
      if (request.method === "POST" && path.endsWith("/verify")) {
        secondSignedIn = true;
        return json({
          user_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
          display_name: "Member bbbbbbbb",
          spaces: [],
        });
      }
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const { result } = renderHook(() => useUserSession());
  await waitFor(() => expect(result.current.phase).toBe("invitations"));
  act(() => result.current.skipInvitations());
  expect(result.current.phase).toBe("ready");
  expect(window.sessionStorage).toHaveLength(1);
  await act(async () => result.current.signOut());
  expect(result.current.phase).toBe("email");
  expect(window.sessionStorage).toHaveLength(0);
  await act(async () => result.current.sendCode("second@example.com"));
  await act(async () => result.current.verifyCode("123456"));
  await waitFor(() => expect(result.current.phase).toBe("invitations"));
});

test("an email submission 503 remains a known rejection", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me")
        return unauthenticated();
      if (request.method === "POST" && path === "/v1/auth/email/challenges")
        return json({ error: "email code could not be submitted" }, 503);
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);
  await user.type(await screen.findByLabelText("Email"), "alex@example.com");
  await user.click(screen.getByRole("button", { name: "Send code" }));

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "email code could not be submitted",
  );
  expect(screen.getByLabelText("Email")).toHaveValue("alex@example.com");
  expect(
    screen.queryByRole("button", { name: "Try sending again" }),
  ).not.toBeInTheDocument();
});

test("shows a wrong email code and lets the User retry", async () => {
  mockUnauthenticatedEmail(async (request, path) => {
    if (request.method === "POST" && path.endsWith("/verify")) {
      return json({ error: "email code is invalid or expired" }, 401);
    }
    return null;
  });

  const user = userEvent.setup();
  render(<App />);
  await requestCode(user);
  await user.type(await screen.findByLabelText("Email code"), "000000");
  await user.click(screen.getByRole("button", { name: "Verify" }));

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "email code is invalid or expired",
  );
  expect(screen.getByLabelText("Email code")).toHaveValue("000000");
  expect(screen.getByRole("button", { name: "Verify" })).toBeEnabled();
});

test("replays an unknown verification exactly before loading the User", async () => {
  let sessionEstablished = false;
  const events: Array<{ action: string; body?: unknown; key?: string | null }> =
    [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        events.push({ action: "me" });
        if (!sessionEstablished) return unauthenticated();
        return json(currentUser([]));
      }
      if (request.method === "POST" && path === "/v1/auth/email/challenges") {
        return acceptedChallenge(request);
      }
      if (request.method === "POST" && path.endsWith("/verify")) {
        events.push({
          action: "verify",
          body: await request.json(),
          key: request.headers.get("Idempotency-Key"),
        });
        sessionEstablished = true;
        if (events.filter((event) => event.action === "verify").length === 1) {
          return json({ error: "private verification operation" }, 500);
        }
        return new Response(null, { status: 204 });
      }
      if (request.method === "GET" && path === "/v1/invitations")
        return json({ invitations: [], reauthentication_required: false });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);
  await requestCode(user);
  await user.type(await screen.findByLabelText("Email code"), "123456");
  await user.click(screen.getByRole("button", { name: "Verify" }));
  await screen.findByRole("heading", { name: "Choose a Space" });

  const reconciliation = events.slice(-3);
  expect(reconciliation.map((event) => event.action)).toEqual([
    "verify",
    "verify",
    "me",
  ]);
  expect(reconciliation[0]).toEqual(reconciliation[1]);
});

test("explicit resend creates a new challenge and only its newest code succeeds", async () => {
  let sessionEstablished = false;
  const challenges: Array<{ id: string; key: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return sessionEstablished ? json(currentUser([])) : unauthenticated();
      }
      if (request.method === "POST" && path === "/v1/auth/email/challenges") {
        const body = (await request.json()) as { challenge_id: string };
        challenges.push({
          id: body.challenge_id,
          key: request.headers.get("Idempotency-Key"),
        });
        return json(
          {
            challenge_id: body.challenge_id,
            expires_at: "2026-08-20T12:05:00Z",
          },
          202,
        );
      }
      if (request.method === "POST" && path.endsWith("/verify")) {
        const challengeID = path.split("/").at(-2);
        const body = (await request.json()) as { code: string };
        if (challengeID !== challenges[1]?.id || body.code !== "222222") {
          return json({ error: "email code is invalid or expired" }, 401);
        }
        sessionEstablished = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "GET" && path === "/v1/invitations")
        return json({ invitations: [], reauthentication_required: false });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);
  await requestCode(user);
  await user.click(screen.getByRole("button", { name: "Send a new code" }));
  await waitFor(() => expect(challenges).toHaveLength(2));
  expect(challenges[1]?.id).not.toBe(challenges[0]?.id);
  expect(challenges[1]?.key).not.toBe(challenges[0]?.key);

  await user.type(screen.getByLabelText("Email code"), "111111");
  await user.click(screen.getByRole("button", { name: "Verify" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "email code is invalid or expired",
  );
  await user.clear(screen.getByLabelText("Email code"));
  await user.type(screen.getByLabelText("Email code"), "222222");
  await user.click(screen.getByRole("button", { name: "Verify" }));
  await screen.findByRole("heading", { name: "Choose a Space" });

  expect(window.location.href).not.toContain("111111");
  expect(window.location.href).not.toContain("222222");
  expect(window.localStorage.length).toBe(0);
  expect(window.sessionStorage.length).toBe(0);
});

test("a committed logout with a lost response reconciles from current User 401", async () => {
  window.history.replaceState(null, "", "/s/research");
  let revoked = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return revoked
          ? unauthenticated()
          : json(
              currentUser([
                {
                  space_id: actualSpaceID,
                  name: "Research",
                  slug: "research",
                  can_manage_members: true,
                  can_enroll_machines: true,
                },
              ]),
            );
      }
      if (request.method === "GET" && path.endsWith("/conversation/messages")) {
        return json({ messages: [] });
      }
      if (request.method === "GET" && path.endsWith("/works")) {
        return json({ works: [], has_earlier_works: false });
      }
      if (
        request.method === "DELETE" &&
        path === "/v1/browser/sessions/current"
      ) {
        revoked = true;
        throw new TypeError("logout response lost after commit");
      }
      if (request.method === "GET" && path === "/v1/invitations")
        return json({ invitations: [], reauthentication_required: false });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);
  await screen.findByRole("heading", {
    name: "What should Carry keep moving?",
  });
  await user.click(screen.getByRole("button", { name: "Sign out" }));

  await screen.findByLabelText("Email");
  expect(screen.queryByText("Your Work is hidden on this browser.")).toBeNull();
  expect(window.location.search).not.toContain("carry-signing-out");
  expect(window.sessionStorage.length).toBe(0);
});

test("ordinary Space creation replays exactly after response loss", async () => {
  let sessionEstablished = false;
  let spaceCreated = false;
  const spaceCommands: Array<{ body: unknown; key: string | null }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        if (!sessionEstablished) return unauthenticated();
        return json(
          currentUser([
            {
              space_id: actualSpaceID,
              name: "Operations",
              slug: "operations",
              can_manage_members: true,
              can_enroll_machines: true,
            },
            ...(spaceCreated
              ? [
                  {
                    space_id: "22222222-2222-4222-8222-222222222222",
                    name: "Attempted Research",
                    slug: "attempted-research",
                    can_manage_members: true,
                    can_enroll_machines: true,
                  },
                ]
              : []),
          ]),
        );
      }
      if (request.method === "POST" && path === "/v1/auth/email/challenges") {
        return acceptedChallenge(request);
      }
      if (request.method === "POST" && path.endsWith("/verify")) {
        sessionEstablished = true;
        return new Response(null, { status: 204 });
      }
      if (request.method === "POST" && path === "/v1/spaces") {
        spaceCommands.push({
          body: await request.json(),
          key: request.headers.get("Idempotency-Key"),
        });
        spaceCreated = true;
        if (spaceCommands.length === 1) {
          throw new TypeError("Space response unknown");
        }
        return json(
          {
            space_id: "22222222-2222-4222-8222-222222222222",
            name: "Attempted Research",
            slug: "attempted-research",
            can_manage_members: true,
            can_enroll_machines: true,
          },
          201,
        );
      }
      if (request.method === "GET" && path.endsWith("/conversation/messages")) {
        return json({ messages: [] });
      }
      if (request.method === "GET" && path.endsWith("/works")) {
        return json({ works: [], has_earlier_works: false });
      }
      if (request.method === "GET" && path === "/v1/invitations")
        return json({ invitations: [], reauthentication_required: false });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );

  const user = userEvent.setup();
  render(<App />);
  await requestCode(user);
  await user.type(await screen.findByLabelText("Email code"), "123456");
  await user.click(screen.getByRole("button", { name: "Verify" }));
  await user.type(
    await screen.findByLabelText("Space name"),
    "Attempted Research",
  );
  await user.click(screen.getByRole("button", { name: "Create Space" }));

  await waitFor(() => expect(spaceCommands).toHaveLength(2));
  expect(spaceCommands[0]).toEqual(spaceCommands[1]);
});

function mockUnauthenticatedEmail(
  additional: (request: Request, path: string) => Promise<Response | null>,
) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path === "/v1/me") {
        return unauthenticated();
      }
      if (request.method === "POST" && path === "/v1/auth/email/challenges") {
        return acceptedChallenge(request);
      }
      const response = await additional(request, path);
      if (response) return response;
      if (request.method === "GET" && path === "/v1/invitations")
        return json({ invitations: [], reauthentication_required: false });
      throw new Error(`unexpected request: ${request.method} ${path}`);
    }),
  );
}

async function requestCode(user: ReturnType<typeof userEvent.setup>) {
  await user.type(await screen.findByLabelText("Email"), "alex@example.com");
  await user.click(screen.getByRole("button", { name: "Send code" }));
}

async function acceptedChallenge(request: Request): Promise<Response> {
  const body = (await request.json()) as { challenge_id: string };
  return json(
    {
      challenge_id: body.challenge_id,
      expires_at: "2026-08-20T12:05:00Z",
    },
    202,
  );
}

function currentUser(spaces: Array<Record<string, unknown>>) {
  return { user_id: userID, display_name: "Alex Morgan", spaces };
}

function unauthenticated(): Response {
  return json({ error: "User authentication is required" }, 401);
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
