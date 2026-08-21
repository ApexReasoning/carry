import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { CliLoginPage } from "./cli-login";

const api = vi.hoisted(() => ({
  approveCliLogin: vi.fn(),
  cliCredentials: vi.fn(),
  denyCliLogin: vi.fn(),
  lookupCliLogin: vi.fn(),
}));

vi.mock("../../carry-api", () => {
  class Unknown extends Error {}
  return { ...api, MutationOutcomeUnknownError: Unknown };
});

const user = {
  user_id: "11111111-1111-4111-8111-111111111111",
  display_name: "Ada",
  spaces: [
    {
      space_id: "22222222-2222-4222-8222-222222222222",
      name: "Research",
      can_manage_members: true,
      can_enroll_machines: true,
    },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  api.cliCredentials.mockResolvedValue([]);
  api.lookupCliLogin.mockResolvedValue({
    request_id: "33333333-3333-4333-8333-333333333333",
    user_code: "BCDF-GHJ-KLM",
    label: "Desk CLI",
    server: "https://carry.example",
    created_at: "2026-08-21T00:00:00Z",
    expires_at: "2026-08-21T00:15:00Z",
    approved: false,
    denied: false,
    cancelled: false,
    redeemed: false,
  });
  api.approveCliLogin.mockResolvedValue(undefined);
  api.denyCliLogin.mockResolvedValue(undefined);
});

test("requires manual code review and explicit Space approval", async () => {
  const terminal = userEvent.setup();
  render(<CliLoginPage user={user} />);

  expect(window.location.search).toBe("");
  await terminal.type(
    screen.getByLabelText("Code shown by carry login"),
    "bcdfghjklm",
  );
  await terminal.click(screen.getByRole("button", { name: "Review login" }));

  expect(await screen.findByText("https://carry.example")).toBeVisible();
  expect(screen.getByText("BCDF-GHJ-KLM")).toBeVisible();
  expect(screen.getByText("Desk CLI")).toBeVisible();
  expect(screen.getByText("Ada")).toBeVisible();
  expect(screen.getByText(/does not add a Membership/)).toBeVisible();
  expect(api.approveCliLogin).not.toHaveBeenCalled();

  await terminal.click(
    screen.getByRole("button", { name: "Approve this CLI login" }),
  );
  await waitFor(() => expect(api.approveCliLogin).toHaveBeenCalledTimes(1));
  expect(api.approveCliLogin.mock.calls[0]?.[1]).toBe(
    "22222222-2222-4222-8222-222222222222",
  );
  expect(await screen.findByText(/Return to the terminal/)).toBeVisible();
});

test("offers an equally explicit denial without issuing approval", async () => {
  const terminal = userEvent.setup();
  render(<CliLoginPage user={user} />);
  await terminal.type(
    screen.getByLabelText("Code shown by carry login"),
    "BCDF-GHJ-KLM",
  );
  await terminal.click(screen.getByRole("button", { name: "Review login" }));
  await terminal.click(await screen.findByRole("button", { name: "Deny" }));
  await waitFor(() => expect(api.denyCliLogin).toHaveBeenCalledTimes(1));
  expect(api.approveCliLogin).not.toHaveBeenCalled();
  expect(
    await screen.findByText(/will not receive a credential/),
  ).toBeVisible();
});
