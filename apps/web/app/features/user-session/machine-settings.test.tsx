import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { MachineSettings } from "./machine-settings";

const api = vi.hoisted(() => ({
  machines: vi.fn(),
  revokeMachine: vi.fn(),
}));

vi.mock("../../carry-api", () => api);

const pi = {
  agent_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  name: "Pi",
  avatar_index: 3,
  owner_user_id: "33333333-3333-4333-8333-333333333333",
  owner_name: "Ada",
  state: "active" as const,
  online: true,
  last_active_at: "2026-08-21T00:04:00Z",
};

const codex = {
  ...pi,
  agent_id: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
  name: "Codex",
  avatar_index: 6,
  online: false,
  last_active_at: null,
};

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
  agents: [pi, codex],
};

beforeEach(() => {
  vi.clearAllMocks();
  const revoked = {
    ...active,
    state: "Revoked" as const,
    revocation_actor: "Ada",
    revoked_at: "2026-08-21T01:00:00Z",
    agents: active.agents.map((agent) => ({
      ...agent,
      state: "removed" as const,
      online: false,
    })),
  };
  api.machines
    .mockResolvedValueOnce({ machines: [active] })
    .mockResolvedValue({ machines: [revoked] });
  api.revokeMachine.mockResolvedValue(revoked);
});

test("groups durable Agent identities under their Host with owner lifecycle and presence", async () => {
  render(
    <MachineSettings
      spaceID={active.space_id}
      spaceName="Research"
      canEnroll
      onClose={() => undefined}
    />,
  );

  expect(await screen.findByText("Desk Mac")).toBeVisible();
  expect(screen.getByRole("link", { name: "Add Host" })).toHaveAttribute(
    "href",
    "/machine-connect",
  );
  const agents = screen.getByRole("list", { name: "Desk Mac Agents" });
  expect(agents).toHaveTextContent("Pi");
  expect(agents).toHaveTextContent("Owned by Ada");
  expect(agents).toHaveTextContent("Active · Online · Last active");
  expect(agents).toHaveTextContent("Codex");
  expect(agents).toHaveTextContent("Active · Offline · Never active");
  expect(
    screen.queryByText(/adapter|occurrence|provider|model/i),
  ).not.toBeInTheDocument();
});

test("revoke explains and then shows the exact Removed Agent consequence", async () => {
  const person = userEvent.setup();
  render(
    <MachineSettings
      spaceID={active.space_id}
      spaceName="Research"
      canEnroll
      onClose={() => undefined}
    />,
  );
  await screen.findByText("Desk Mac");
  await person.click(screen.getByRole("button", { name: "Revoke Host" }));
  const dialog = screen.getByRole("dialog", { name: "Revoke Host" });
  expect(dialog).toHaveTextContent(
    "Every Active Agent on this Host becomes Removed",
  );
  expect(dialog).toHaveTextContent("unavailable Work is not reassigned");
  expect(dialog).toHaveTextContent("SHA256:exact-full-fingerprint");
  await person.click(
    within(dialog).getByRole("button", { name: "Revoke Host" }),
  );

  await waitFor(() => expect(api.revokeMachine).toHaveBeenCalledTimes(1));
  await waitFor(() =>
    expect(screen.getByText("Pi").parentElement).toHaveTextContent(
      "Removed · Offline",
    ),
  );
});

test("shows exact empty Host and empty Agent states", async () => {
  api.machines.mockReset();
  api.machines.mockResolvedValueOnce({ machines: [] });
  const { rerender } = render(
    <MachineSettings
      spaceID={active.space_id}
      spaceName="Research"
      canEnroll
      onClose={() => undefined}
    />,
  );
  expect(
    await screen.findByText("No Hosts have been connected to this Space."),
  ).toBeVisible();

  api.machines.mockResolvedValueOnce({ machines: [{ ...active, agents: [] }] });
  rerender(
    <MachineSettings
      spaceID="44444444-4444-4444-8444-444444444444"
      spaceName="Research"
      canEnroll
      onClose={() => undefined}
    />,
  );
  expect(
    await screen.findByText(
      "No supported Agents have been discovered on this Host.",
    ),
  ).toBeVisible();
});

test("shows the exact combined inventory failure", async () => {
  api.machines.mockReset();
  api.machines.mockRejectedValue(new Error("private database detail"));
  render(
    <MachineSettings
      spaceID={active.space_id}
      spaceName="Research"
      canEnroll
      onClose={() => undefined}
    />,
  );
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Host and Agent inventory failed.",
  );
});
