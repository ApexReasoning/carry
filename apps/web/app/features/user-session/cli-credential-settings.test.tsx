import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { CliCredentialSettings } from "./cli-credential-settings";

const api = vi.hoisted(() => ({
  cliCredentials: vi.fn(),
  revokeCliCredential: vi.fn(),
}));

vi.mock("../../carry-api", () => {
  class Unknown extends Error {}
  return { ...api, MutationOutcomeUnknownError: Unknown };
});

beforeEach(() => {
  vi.clearAllMocks();
  api.cliCredentials.mockResolvedValue([
    {
      credential_id: "11111111-1111-4111-8111-111111111111",
      label: "Desk CLI",
      approved_space_id: "22222222-2222-4222-8222-222222222222",
      approved_space_name: "Research",
      created_at: "2026-08-21T00:00:00Z",
      expires_at: "2026-11-19T00:00:00Z",
    },
  ]);
  api.revokeCliCredential.mockResolvedValue(undefined);
});

test("lists and revokes one exact CLI credential without showing a secret", async () => {
  const user = userEvent.setup();
  render(<CliCredentialSettings onClose={() => undefined} />);

  expect(await screen.findByText("Desk CLI")).toBeVisible();
  expect(screen.getByText("Default context: Research")).toBeVisible();
  expect(screen.queryByText(/carry_cli_/)).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Revoke CLI access" }));
  await waitFor(() =>
    expect(api.revokeCliCredential).toHaveBeenCalledWith(
      "11111111-1111-4111-8111-111111111111",
      expect.any(String),
    ),
  );
  expect(screen.queryByText("Desk CLI")).not.toBeInTheDocument();
});
