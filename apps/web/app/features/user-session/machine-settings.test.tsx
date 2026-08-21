import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { MachineSettings } from "./machine-settings";

const api = vi.hoisted(() => ({
  machines: vi.fn(),
  revokeMachine: vi.fn(),
}));

vi.mock("../../carry-api", () => api);

const active = {
  machine_id: "11111111-1111-4111-8111-111111111111",
  space_id: "22222222-2222-4222-8222-222222222222",
  space_name: "Research",
  display_name: "Desk Mac",
  fingerprint: "SHA256:exact-full-fingerprint",
  state: "Active" as const,
  enrolled_by_user_id: "33333333-3333-4333-8333-333333333333",
  enrolled_by_name: "Ada",
  enrolled_at: "2026-08-21T00:00:00Z",
  can_revoke: true,
};

beforeEach(() => {
  vi.clearAllMocks();
  const revoked = {
    ...active,
    state: "Revoked" as const,
    revocation_actor: "Ada",
    revoked_at: "2026-08-21T01:00:00Z",
  };
  api.machines
    .mockResolvedValueOnce({ machines: [active] })
    .mockResolvedValue({ machines: [revoked] });
  api.revokeMachine.mockResolvedValue(revoked);
});

test("inventory distinguishes server authority from telemetry and confirms limited revocation consequences", async () => {
  const person = userEvent.setup();
  render(
    <MachineSettings
      spaceID={active.space_id}
      spaceName="Research"
      canEnroll
      onClose={() => undefined}
    />,
  );

  const machineName = await screen.findByText("Desk Mac");
  expect(machineName.parentElement).toHaveTextContent("Desk Mac — Active");
  expect(
    screen.getByText(/does not mean the computer or Host process is online/),
  ).toBeVisible();
  expect(screen.queryByText(/last seen/i)).not.toBeInTheDocument();
  expect(
    screen.queryByText("SHA256:exact-full-fingerprint"),
  ).not.toBeInTheDocument();

  await person.click(screen.getByRole("button", { name: "Revoke" }));
  const dialog = screen.getByRole("dialog", { name: "Revoke Machine" });
  expect(dialog).toHaveTextContent("SHA256:exact-full-fingerprint");
  expect(dialog).toHaveTextContent("does not prove a process stopped");
  await person.click(screen.getByRole("button", { name: "Revoke Machine" }));

  await waitFor(() => expect(api.revokeMachine).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(screen.getByText("Desk Mac").parentElement).toHaveTextContent(
      "Desk Mac — Revoked",
    ),
  );
});

test("loads every bounded inventory page so later Machines remain reachable", async () => {
  const person = userEvent.setup();
  const later = {
    ...active,
    machine_id: "44444444-4444-4444-8444-444444444444",
    display_name: "Later Mac",
  };
  api.machines.mockReset();
  api.machines
    .mockResolvedValueOnce({
      machines: [active],
      next_cursor: active.machine_id,
    })
    .mockResolvedValueOnce({ machines: [later] });

  render(
    <MachineSettings
      spaceID={active.space_id}
      spaceName="Research"
      canEnroll
      onClose={() => undefined}
    />,
  );

  await screen.findByText("Desk Mac");
  expect(screen.queryByText("Later Mac")).not.toBeInTheDocument();
  await person.click(
    screen.getByRole("button", { name: "Load more Machines" }),
  );
  await screen.findByText("Later Mac");
  expect(api.machines).toHaveBeenNthCalledWith(
    2,
    active.space_id,
    active.machine_id,
  );
  expect(
    screen.queryByRole("button", { name: "Load more Machines" }),
  ).not.toBeInTheDocument();
});
