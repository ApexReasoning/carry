import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, test, vi } from "vitest";

import { InvitationInboxView } from "./invitation-inbox";
import { MemberSettings } from "./member-settings";

afterEach(() => vi.unstubAllGlobals());

test("manager invitation defaults grants off and keeps Unknown truthful", async () => {
  const commands: Array<Record<string, unknown>> = [];
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
  const changed = vi.fn();
  const user = userEvent.setup();
  render(
    <InvitationInboxView
      user={{
        user_id: "30000000-0000-4000-8000-000000000001",
        display_name: "Member 30000000",
        spaces: [],
      }}
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
      onChanged={changed}
      onSkip={() => undefined}
    />,
  );
  expect(screen.queryByLabelText("Display name")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Accept and join" }));
  expect(acceptedBody).toBe("");
  expect(changed).toHaveBeenCalledOnce();
});

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}
