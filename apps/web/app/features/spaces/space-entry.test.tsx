import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

const creation = vi.hoisted(() => ({
  createExactSpace: vi.fn(),
  discardCorruptPendingSpaceCreation: vi.fn(),
}));
vi.mock("./space-creation", () => {
  class CorruptPendingSpaceCreationError extends Error {}
  return {
    CorruptPendingSpaceCreationError,
    createExactSpace: creation.createExactSpace,
    discardCorruptPendingSpaceCreation:
      creation.discardCorruptPendingSpaceCreation,
  };
});

import {
  MutationOutcomeUnknownError,
  SpaceSlugConflictError,
} from "../../carry-api";
import type { User } from "../../generated/types.gen";
import { CorruptPendingSpaceCreationError } from "./space-creation";
import { SpaceEntry } from "./space-entry";

const user: User = {
  user_id: "10000000-0000-4000-8000-000000000013",
  display_name: "Member 10000000",
  spaces: [
    {
      space_id: "20000000-0000-4000-8000-000000000013",
      name: "Research",
      slug: "研究-team",
      can_manage_members: true,
      can_enroll_machines: true,
    },
  ],
};

beforeEach(() => {
  creation.createExactSpace.mockReset();
  creation.discardCorruptPendingSpaceCreation.mockReset();
});

test("one Space still renders the explicit chooser and URL link", () => {
  render(<SpaceEntry user={user} onEnter={vi.fn()} onSignOut={vi.fn()} />);

  expect(screen.getByRole("heading", { name: "Choose a Space" })).toBeVisible();
  expect(screen.getByRole("link", { name: "Invitations" })).toHaveAttribute(
    "href",
    "/invitations",
  );
  expect(screen.getByRole("link", { name: /Research/ })).toHaveAttribute(
    "href",
    "/s/%E7%A0%94%E7%A9%B6-team",
  );
});

test("a member without a Space can sign out of the chooser", async () => {
  const onSignOut = vi.fn();
  render(
    <SpaceEntry
      user={{ ...user, spaces: [] }}
      onEnter={vi.fn()}
      onSignOut={onSignOut}
    />,
  );

  await userEvent
    .setup()
    .click(screen.getByRole("button", { name: "Sign out" }));

  expect(onSignOut).toHaveBeenCalledOnce();
});

test("requires explicit recovery before discarding damaged creation identities", async () => {
  creation.createExactSpace.mockRejectedValue(
    new CorruptPendingSpaceCreationError("damaged"),
  );
  render(<SpaceEntry user={user} onEnter={vi.fn()} onSignOut={vi.fn()} />);
  const actor = userEvent.setup();

  await actor.type(screen.getByLabelText("Space name"), "Operations");
  await actor.click(screen.getByRole("button", { name: "Create Space" }));

  expect(creation.discardCorruptPendingSpaceCreation).not.toHaveBeenCalled();
  expect(screen.getByRole("button", { name: "Create Space" })).toBeDisabled();
  await actor.click(
    screen.getByRole("button", {
      name: "Clear saved Space creation",
    }),
  );
  expect(creation.discardCorruptPendingSpaceCreation).toHaveBeenCalledOnce();
  expect(screen.getByRole("button", { name: "Create Space" })).toBeEnabled();
});

test("keeps creation blocked when saved browser state cannot be cleared", async () => {
  creation.createExactSpace.mockRejectedValue(
    new CorruptPendingSpaceCreationError("unreadable"),
  );
  creation.discardCorruptPendingSpaceCreation.mockImplementation(() => {
    throw new Error(
      "Carry could not clear the saved Space creation from this browser.",
    );
  });
  render(<SpaceEntry user={user} onEnter={vi.fn()} onSignOut={vi.fn()} />);
  const actor = userEvent.setup();

  await actor.type(screen.getByLabelText("Space name"), "Operations");
  await actor.click(screen.getByRole("button", { name: "Create Space" }));
  await actor.click(
    screen.getByRole("button", { name: "Clear saved Space creation" }),
  );

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Carry could not clear the saved Space creation from this browser.",
  );
  expect(screen.getByRole("button", { name: "Create Space" })).toBeDisabled();
});

test("creates with one visible name and enters the committed slug", async () => {
  creation.createExactSpace.mockResolvedValue({
    ...user.spaces[0],
    name: "Operations",
    slug: "operations",
  });
  const onEnter = vi.fn();
  render(
    <SpaceEntry
      user={{ ...user, spaces: [] }}
      onEnter={onEnter}
      onSignOut={vi.fn()}
    />,
  );
  const actor = userEvent.setup();

  await actor.type(screen.getByLabelText("Space name"), "Operations");
  await actor.click(screen.getByRole("button", { name: "Create Space" }));

  expect(creation.createExactSpace).toHaveBeenCalledWith(
    user.user_id,
    "Operations",
    undefined,
  );
  expect(onEnter).toHaveBeenCalledWith("operations");
});

test("an unknown suggested suffix retries the exact accepted request", async () => {
  creation.createExactSpace
    .mockRejectedValueOnce(new SpaceSlugConflictError("acme", "acme-2", 2))
    .mockRejectedValueOnce(new MutationOutcomeUnknownError("response lost"))
    .mockResolvedValueOnce({
      ...user.spaces[0],
      name: "Acme",
      slug: "acme-2",
    });
  render(
    <SpaceEntry
      user={{ ...user, spaces: [] }}
      onEnter={vi.fn()}
      onSignOut={vi.fn()}
    />,
  );
  const actor = userEvent.setup();

  await actor.type(screen.getByLabelText("Space name"), "Acme");
  await actor.click(screen.getByRole("button", { name: "Create Space" }));
  await actor.click(screen.getByRole("button", { name: "Try /s/acme-2" }));
  await actor.click(screen.getByRole("button", { name: "Try creating again" }));

  expect(creation.createExactSpace).toHaveBeenNthCalledWith(
    3,
    user.user_id,
    "Acme",
    2,
  );
});

test("shows an unreserved suggestion and submits its suffix explicitly", async () => {
  creation.createExactSpace
    .mockRejectedValueOnce(new SpaceSlugConflictError("acme", "acme-2", 2))
    .mockResolvedValueOnce({
      ...user.spaces[0],
      name: "Acme",
      slug: "acme-2",
    });
  render(
    <SpaceEntry
      user={{ ...user, spaces: [] }}
      onEnter={vi.fn()}
      onSignOut={vi.fn()}
    />,
  );
  const actor = userEvent.setup();

  await actor.type(screen.getByLabelText("Space name"), "Acme");
  await actor.click(screen.getByRole("button", { name: "Create Space" }));
  expect(screen.getByText(/not reserved/)).toBeVisible();
  await actor.click(screen.getByRole("button", { name: "Try /s/acme-2" }));

  expect(creation.createExactSpace).toHaveBeenLastCalledWith(
    user.user_id,
    "Acme",
    2,
  );
});
