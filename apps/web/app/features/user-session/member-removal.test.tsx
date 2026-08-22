import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { MutationOutcomeUnknownError } from "../../carry-api";
import { MemberSettings } from "./member-settings";

const api = vi.hoisted(() => ({
  spaceMembers: vi.fn(),
  managedInvitations: vi.fn(),
  issueInvitation: vi.fn(),
  resendInvitation: vi.fn(),
  revokeInvitation: vi.fn(),
  removeMember: vi.fn(),
}));

vi.mock("../../carry-api", () => {
  class Unknown extends Error {}
  return {
    ...api,
    MutationOutcomeUnknownError: Unknown,
  };
});

const target = {
  user_id: "30000000-0000-4000-8000-000000000001",
  display_name: "Alex",
  can_manage_members: false,
  can_enroll_machines: false,
  open_work_count: 2,
  joined_at: "2026-08-21T00:00:00Z",
};
const successor = {
  user_id: "40000000-0000-4000-8000-000000000001",
  display_name: "Sam",
  can_manage_members: true,
  can_enroll_machines: true,
  open_work_count: 0,
  joined_at: "2026-08-20T00:00:00Z",
};

beforeEach(() => {
  vi.clearAllMocks();
  api.spaceMembers.mockResolvedValue({
    members: [target, successor],
    next_cursor: null,
  });
  api.managedInvitations.mockResolvedValue([]);
  api.removeMember.mockResolvedValue(undefined);
});

test("loads active removal targets and successors beyond the first bounded page", async () => {
  api.spaceMembers
    .mockResolvedValueOnce({ members: [target], next_cursor: target.user_id })
    .mockResolvedValueOnce({ members: [successor], next_cursor: null });
  const user = userEvent.setup();
  renderSettings(vi.fn());

  await screen.findByText("Alex");
  await user.click(screen.getByRole("button", { name: "Load more members" }));

  expect(await screen.findByText("Sam")).toBeVisible();
  expect(api.spaceMembers).toHaveBeenNthCalledWith(
    2,
    "20000000-0000-4000-8000-000000000001",
    target.user_id,
  );
});

test("confirms exact removal consequences and requires one active Work successor", async () => {
  const user = userEvent.setup();
  const removed = vi.fn();
  renderSettings(removed);

  const alex = await screen.findByText("Alex");
  await user.click(
    within(alex.closest("li") as HTMLElement).getByRole("button", {
      name: "Remove from Space",
    }),
  );

  expect(
    screen.getByRole("heading", { name: "Remove Alex from Research?" }),
  ).toBeVisible();
  expect(
    screen.getByText(/Future access to Research ends immediately/),
  ).toHaveTextContent("private Conversation rows remain private and retained");
  expect(
    screen.getByText(/Future access to Research ends immediately/),
  ).toHaveTextContent(
    "Other sign-in access and Space Machines are not automatically revoked",
  );
  expect(
    screen.getByText(/Future access to Research ends immediately/),
  ).toHaveTextContent("Pending invitations remain");
  expect(screen.getByRole("button", { name: "Remove Alex" })).toBeDisabled();

  await user.selectOptions(
    screen.getByLabelText("Transfer all 2 Open Work to"),
    successor.user_id,
  );
  await user.click(screen.getByRole("button", { name: "Remove Alex" }));

  await waitFor(() => expect(api.removeMember).toHaveBeenCalledOnce());
  expect(api.removeMember.mock.calls[0]?.slice(0, 3)).toEqual([
    "20000000-0000-4000-8000-000000000001",
    target.user_id,
    successor.user_id,
  ]);
  expect(removed).toHaveBeenCalledWith(false);
  expect(screen.queryByText("Alex")).not.toBeInTheDocument();
});

test("Unknown removal retry preserves the exact target successor and command key", async () => {
  api.removeMember
    .mockRejectedValueOnce(
      new MutationOutcomeUnknownError("removal outcome is unknown"),
    )
    .mockResolvedValueOnce(undefined);
  const user = userEvent.setup();
  renderSettings(vi.fn());

  const alex = await screen.findByText("Alex");
  await user.click(
    within(alex.closest("li") as HTMLElement).getByRole("button", {
      name: "Remove from Space",
    }),
  );
  await user.selectOptions(
    screen.getByLabelText("Transfer all 2 Open Work to"),
    successor.user_id,
  );
  await user.click(screen.getByRole("button", { name: "Remove Alex" }));
  await user.click(
    await screen.findByRole("button", { name: "Try this change again" }),
  );

  await waitFor(() => expect(api.removeMember).toHaveBeenCalledTimes(2));
  expect(api.removeMember.mock.calls[1]).toEqual(
    api.removeMember.mock.calls[0],
  );
});

test("self-removal reports that routing must refresh", async () => {
  api.spaceMembers.mockResolvedValue({
    members: [{ ...target, open_work_count: 0 }, successor],
    next_cursor: null,
  });
  const user = userEvent.setup();
  const removed = vi.fn();
  renderSettings(removed, target.user_id);

  const alex = await screen.findByText("Alex");
  await user.click(
    within(alex.closest("li") as HTMLElement).getByRole("button", {
      name: "Remove from Space",
    }),
  );
  await user.click(screen.getByRole("button", { name: "Remove Alex" }));

  await waitFor(() => expect(removed).toHaveBeenCalledWith(true));
  expect(api.removeMember.mock.calls[0]?.[2]).toBeUndefined();
});

function renderSettings(
  onRemoved: (removedSelf: boolean) => void,
  currentUserID = "50000000-0000-4000-8000-000000000001",
) {
  render(
    <MemberSettings
      spaceID="20000000-0000-4000-8000-000000000001"
      spaceName="Research"
      currentUserID={currentUserID}
      canManage
      canEnroll
      onClose={() => undefined}
      onRemoved={onRemoved}
    />,
  );
}
