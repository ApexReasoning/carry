import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { MachineConnectPage } from "./machine-connect";

const api = vi.hoisted(() => ({
  approveMachineConnection: vi.fn(),
  denyMachineConnection: vi.fn(),
  lookupMachineConnection: vi.fn(),
}));

vi.mock("../../carry-api", () => api);

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
    {
      space_id: "33333333-3333-4333-8333-333333333333",
      name: "Read only",
      can_manage_members: false,
      can_enroll_machines: false,
    },
  ],
};

beforeEach(() => {
  vi.clearAllMocks();
  api.lookupMachineConnection.mockResolvedValue({
    request_id: "44444444-4444-4444-8444-444444444444",
    user_code: "BCDF-GHJ-KLM",
    display_name: "Desk Mac",
    fingerprint: "SHA256:exact-full-fingerprint",
    server: "https://carry.example",
    created_at: "2026-08-21T00:00:00Z",
    expires_at: "2026-08-21T00:15:00Z",
  });
  api.approveMachineConnection.mockResolvedValue(undefined);
  api.denyMachineConnection.mockResolvedValue(undefined);
});

test("shows exact origin, name, full fingerprint, and only eligible Spaces before approval", async () => {
  const person = userEvent.setup();
  render(<MachineConnectPage user={user} />);

  await person.type(
    screen.getByLabelText("Code shown by carry host connect"),
    "bcdfghjklm",
  );
  await person.click(screen.getByRole("button", { name: "Review Machine" }));

  expect(await screen.findByText("https://carry.example")).toBeVisible();
  expect(screen.getByText("Desk Mac")).toBeVisible();
  expect(screen.getByText("SHA256:exact-full-fingerprint")).toBeVisible();
  expect(screen.getByText("BCDF-GHJ-KLM")).toBeVisible();
  expect(screen.getByRole("option", { name: "Research" })).toBeVisible();
  expect(
    screen.queryByRole("option", { name: "Read only" }),
  ).not.toBeInTheDocument();
  expect(api.approveMachineConnection).not.toHaveBeenCalled();

  await person.click(screen.getByRole("button", { name: "Connect Machine" }));
  await waitFor(() =>
    expect(api.approveMachineConnection).toHaveBeenCalledTimes(1),
  );
  expect(api.approveMachineConnection.mock.calls[0]?.[1]).toBe(
    "22222222-2222-4222-8222-222222222222",
  );
  expect(
    await screen.findByRole("heading", { name: "Machine approved" }),
  ).toBeVisible();
});

test("denial never invokes approval", async () => {
  const person = userEvent.setup();
  render(<MachineConnectPage user={user} />);
  await person.type(
    screen.getByLabelText("Code shown by carry host connect"),
    "BCDF-GHJ-KLM",
  );
  await person.click(screen.getByRole("button", { name: "Review Machine" }));
  await person.click(await screen.findByRole("button", { name: "Deny" }));
  await waitFor(() =>
    expect(api.denyMachineConnection).toHaveBeenCalledTimes(1),
  );
  expect(api.approveMachineConnection).not.toHaveBeenCalled();
  expect(
    await screen.findByRole("heading", { name: "Machine denied" }),
  ).toBeVisible();
});
