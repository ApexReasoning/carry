import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";

import {
  InvitationInboxView,
  TargetedInvitationView,
} from "./invitation-inbox";
import { MemberSettings } from "./member-settings";

afterEach(() => vi.unstubAllGlobals());

test("manager invitation defaults grants off and keeps Unknown truthful", async () => {
  const commands: Array<Record<string, unknown>> = [];
  const writeText = vi.fn(async () => undefined);
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const path = new URL(request.url).pathname;
      if (request.method === "GET" && path.endsWith("/members"))
        return json({ members: [], next_cursor: null });
      if (request.method === "GET" && path.endsWith("/invitations"))
        return json({ invitations: [] });
      if (request.method === "POST" && path.endsWith("/invitations")) {
        commands.push((await request.json()) as Record<string, unknown>);
        return json(
          {
            invitation_id: "10000000-0000-4000-8000-000000000001",
            space_id: "20000000-0000-4000-8000-000000000001",
            recipient_email: "person@example.com",
            can_manage_members: false,
            can_enroll_machines: false,
            created_at: "2026-08-21T00:00:00Z",
            expires_at: "2026-08-28T00:00:00Z",
            submission: { state: "unknown" },
          },
          201,
        );
      }
      throw new Error(`unexpected ${request.method} ${path}`);
    }),
  );
  const user = userEvent.setup();
  Object.defineProperty(window.navigator, "clipboard", {
    configurable: true,
    value: { writeText },
  });
  render(
    <MemberSettings
      spaceID="20000000-0000-4000-8000-000000000001"
      spaceName="Research"
      currentUserID="30000000-0000-4000-8000-000000000001"
      canManage
      canEnroll
      onClose={() => undefined}
      onRemoved={() => undefined}
    />,
  );
  await user.type(
    await screen.findByLabelText("Invite one exact Email"),
    "person@example.com",
  );
  await user.click(screen.getByRole("button", { name: "Create invitation" }));
  expect(commands).toEqual([
    {
      email: "person@example.com",
      can_manage_members: false,
      can_enroll_machines: false,
    },
  ]);
  expect(
    await screen.findByText(
      /cannot confirm whether the email service accepted/,
    ),
  ).toBeVisible();
  const exactURL = new URL(
    "/invitations/10000000-0000-4000-8000-000000000001",
    window.location.origin,
  ).toString();
  expect(screen.getByRole("link", { name: exactURL })).toHaveAttribute(
    "href",
    "/invitations/10000000-0000-4000-8000-000000000001",
  );
  await user.click(screen.getByRole("button", { name: "Copy link" }));
  await waitFor(() => expect(writeText).toHaveBeenCalledWith(exactURL));
  expect(screen.getByText("Link copied")).toBeVisible();
});

test("recipient accepts exact grants without a name prompt", async () => {
  let acceptedBody: string | null = null;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      if (request.method === "POST") {
        acceptedBody = await request.text();
        return json({
          invitation_id: "10000000-0000-4000-8000-000000000001",
          space_id: "20000000-0000-4000-8000-000000000001",
          space_name: "Research",
          can_manage_members: true,
          can_enroll_machines: false,
          already_member: false,
        });
      }
      throw new Error("unexpected request");
    }),
  );
  const user = userEvent.setup();
  render(
    <InvitationInboxView
      initialInbox={{
        reauthentication_required: false,
        invitations: [
          {
            invitation_id: "10000000-0000-4000-8000-000000000001",
            space_id: "20000000-0000-4000-8000-000000000001",
            space_name: "Research",
            inviter_display_name: "Manager",
            can_manage_members: true,
            can_enroll_machines: false,
            created_at: "2026-08-21T00:00:00Z",
            expires_at: "2026-08-28T00:00:00Z",
          },
        ],
      }}
      onSkip={() => undefined}
    />,
  );
  expect(screen.queryByLabelText("Display name")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Accept and join" }));
  expect(acceptedBody).toBe("");
});

test.each([
  [
    "accepted",
    "Carry handed the invitation email to the email service. Delivery is not confirmed.",
  ],
  [
    "rejected",
    "The invitation exists, but the email service did not accept this delivery request.",
  ],
  [
    "unknown",
    "Carry cannot confirm whether the email service accepted the invitation. Sending again is a new action.",
  ],
  [
    "prepared",
    "Carry recorded the request to send the invitation but has not confirmed what happened outside Carry.",
  ],
] as const)(
  "invitation submission %s keeps its exact external truth",
  async (state, copy) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const request =
          input instanceof Request ? input : new Request(input, init);
        const path = new URL(request.url).pathname;
        if (request.method === "GET" && path.endsWith("/members"))
          return json({ members: [], next_cursor: null });
        if (request.method === "GET" && path.endsWith("/invitations"))
          return json({
            invitations: [
              {
                invitation_id: "10000000-0000-4000-8000-000000000001",
                space_id: "20000000-0000-4000-8000-000000000001",
                recipient_email: "person@example.com",
                can_manage_members: false,
                can_enroll_machines: false,
                created_at: "2026-08-21T00:00:00Z",
                expires_at: "2026-08-28T00:00:00Z",
                submission: { state },
              },
            ],
          });
        throw new Error(`unexpected ${request.method} ${path}`);
      }),
    );

    render(
      <MemberSettings
        spaceID="20000000-0000-4000-8000-000000000001"
        spaceName="Research"
        currentUserID="30000000-0000-4000-8000-000000000001"
        canManage
        canEnroll
        onClose={() => undefined}
        onRemoved={() => undefined}
      />,
    );

    expect(await screen.findByText(copy)).toBeVisible();
  },
);

test("generic Email proof also offers exact retry and a fresh code request", async () => {
  const requests: Array<{ challengeID: string; key: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const body = (await request.json()) as { challenge_id: string };
      requests.push({
        challengeID: body.challenge_id,
        key: request.headers.get("Idempotency-Key") ?? "",
      });
      return json({
        challenge_id: body.challenge_id,
        expires_at: "2026-08-21T00:10:00Z",
      });
    }),
  );
  const user = userEvent.setup();
  render(
    <InvitationInboxView
      initialInbox={{
        reauthentication_required: true,
        invitations: [
          {
            invitation_id: "10000000-0000-4000-8000-000000000001",
            space_id: "20000000-0000-4000-8000-000000000001",
            space_name: "Research",
            inviter_display_name: "Manager",
            can_manage_members: false,
            can_enroll_machines: false,
            created_at: "2026-08-21T00:00:00Z",
            expires_at: "2026-08-28T00:00:00Z",
          },
        ],
      }}
      onSkip={() => undefined}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Confirm with Email" }));
  await waitFor(() => expect(requests).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "Try sending again" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]).toEqual(requests[0]);
  await user.click(screen.getByRole("button", { name: "Send a new code" }));
  await waitFor(() => expect(requests).toHaveLength(3));
  expect(requests[2]?.challengeID).not.toBe(requests[0]?.challengeID);
  expect(requests[2]?.key).not.toBe(requests[0]?.key);
});

test("targeted Email proof recovers with an exact retry or a fresh code request", async () => {
  const requests: Array<{ challengeID: string; key: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const request =
        input instanceof Request ? input : new Request(input, init);
      const body = (await request.json()) as { challenge_id: string };
      requests.push({
        challengeID: body.challenge_id,
        key: request.headers.get("Idempotency-Key") ?? "",
      });
      if (requests.length === 1) throw new Error("lost response");
      return json({
        challenge_id: body.challenge_id,
        expires_at: "2026-08-21T00:10:00Z",
      });
    }),
  );
  const user = userEvent.setup();
  render(
    <TargetedInvitationView
      state={{
        status: "owner",
        invitation: {
          invitation_id: "10000000-0000-4000-8000-000000000001",
          space_id: "20000000-0000-4000-8000-000000000001",
          space_name: "Research",
          inviter_display_name: "Manager",
          can_manage_members: true,
          can_enroll_machines: false,
          created_at: "2026-08-21T00:00:00Z",
          expires_at: "2026-08-28T00:00:00Z",
          state: "pending",
          accept_result: "",
          current_member: false,
          reauthentication_required: true,
        },
      }}
      onReload={() => undefined}
      onSkip={() => undefined}
      onSignOut={() => undefined}
    />,
  );

  await user.click(screen.getByRole("button", { name: "Confirm with Email" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Confirm email may have finished, but Carry could not confirm it",
  );
  await user.click(screen.getByRole("button", { name: "Try sending again" }));
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[1]).toEqual(requests[0]);

  await user.click(screen.getByRole("button", { name: "Send a new code" }));
  await waitFor(() => expect(requests).toHaveLength(3));
  expect(requests[2]?.challengeID).not.toBe(requests[0]?.challengeID);
  expect(requests[2]?.key).not.toBe(requests[0]?.key);
});

test("targeted invitation renders uniform recovery, proof gate, terminal truth, and Unknown", async () => {
  const signOut = vi.fn();
  const reload = vi.fn();
  const user = userEvent.setup();
  const { rerender } = render(
    <TargetedInvitationView
      state={{ status: "loading" }}
      onReload={reload}
      onSkip={() => undefined}
      onSignOut={signOut}
    />,
  );
  expect(screen.getByText("Loading invitation…")).toBeVisible();
  rerender(
    <TargetedInvitationView
      state={{ status: "error", message: "targeted read failed" }}
      onReload={reload}
      onSkip={() => undefined}
      onSignOut={signOut}
    />,
  );
  expect(screen.getByRole("alert")).toHaveTextContent("targeted read failed");
  rerender(
    <TargetedInvitationView
      state={{ status: "unavailable", hasEmail: true }}
      onReload={reload}
      onSkip={() => undefined}
      onSignOut={signOut}
    />,
  );
  expect(
    screen.getByText("This signed-in account cannot review this invitation."),
  ).toBeVisible();
  expect(
    screen.getByText(/email address that received this invitation/),
  ).toBeVisible();
  await user.click(screen.getByRole("button", { name: "Sign out" }));
  expect(signOut).toHaveBeenCalledOnce();

  const pending = {
    invitation_id: "10000000-0000-4000-8000-000000000001",
    space_id: "20000000-0000-4000-8000-000000000001",
    space_name: "Research",
    inviter_display_name: "Manager",
    can_manage_members: true,
    can_enroll_machines: false,
    created_at: "2026-08-21T00:00:00Z",
    expires_at: "2026-08-28T00:00:00Z",
    state: "pending" as const,
    accept_result: "" as const,
    current_member: false,
    reauthentication_required: true,
  };
  rerender(
    <TargetedInvitationView
      state={{ status: "owner", invitation: pending }}
      onReload={reload}
      onSkip={() => undefined}
      onSignOut={signOut}
    />,
  );
  expect(screen.getByText("Research")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Accept and join" })).toBeNull();
  expect(
    screen.getByRole("button", { name: "Confirm with Email" }),
  ).toBeVisible();

  rerender(
    <TargetedInvitationView
      state={{
        status: "owner",
        invitation: {
          ...pending,
          state: "accepted",
          accept_result: "already_member",
          current_member: false,
          reauthentication_required: false,
        },
      }}
      onReload={reload}
      onSkip={() => undefined}
      onSignOut={signOut}
    />,
  );
  expect(screen.getByText(/already a member/)).toBeVisible();
  expect(screen.getByText(/no longer a current member/)).toBeVisible();

  vi.stubGlobal(
    "fetch",
    vi.fn(async () => Promise.reject(new Error("lost response"))),
  );
  rerender(
    <TargetedInvitationView
      state={{
        status: "owner",
        invitation: { ...pending, reauthentication_required: false },
      }}
      onReload={reload}
      onSkip={() => undefined}
      onSignOut={signOut}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Accept and join" }));
  expect(
    await screen.findByText(/cannot confirm whether you joined the Space/),
  ).toBeVisible();
  expect(
    screen.getByRole("button", { name: "Try accepting again" }),
  ).toBeVisible();
  await user.click(
    screen.getByRole("button", { name: "Reload invitation status" }),
  );
  expect(reload).toHaveBeenCalledOnce();
});

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
