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
    await screen.findByText(/cannot confirm whether the provider accepted/),
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
    screen.getByText("This Carry User cannot review this invitation."),
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
    await screen.findByText(/cannot confirm whether acceptance completed/),
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
