import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, test, vi } from "vitest";

import { APIResponseError, MutationOutcomeUnknownError } from "../../carry-api";
import { IdentityMethodSettings } from "./identity-methods";

const api = vi.hoisted(() => ({
  identityMethods: vi.fn(),
  requestIdentityEmailCode: vi.fn(),
  verifyIdentityEmailCode: vi.fn(),
  unlinkIdentityMethod: vi.fn(),
}));

vi.mock("../../carry-api", () => {
  class Unknown extends Error {}
  class ResponseError extends Error {
    constructor(
      message: string,
      readonly status: number,
    ) {
      super(message);
    }
  }
  return {
    ...api,
    APIResponseError: ResponseError,
    MutationOutcomeUnknownError: Unknown,
  };
});

beforeEach(() => {
  vi.clearAllMocks();
  window.history.replaceState(null, "", "/");
  api.identityMethods.mockResolvedValue({
    methods: ["email"],
    reauthentication_required: true,
  });
});

test.each([
  [
    "link_failed",
    "Carry could not link this sign-in method. Your existing methods were not changed.",
  ],
  [
    "link_cancelled",
    "Linking was cancelled. Your existing methods were not changed.",
  ],
  [
    "link_unavailable",
    "Carry could not confirm whether linking completed. Check the methods below before trying again.",
  ],
  [
    "confirmation_failed",
    "Carry could not confirm this sign-in method. Your existing methods were not changed.",
  ],
  [
    "confirmation_cancelled",
    "Confirmation was cancelled. Your existing methods were not changed.",
  ],
  [
    "confirmation_unavailable",
    "Carry could not confirm whether confirmation completed. Check the methods below before trying again.",
  ],
])("shows neutral Settings copy for %s", async (status, message) => {
  window.history.replaceState(null, "", `/?identity_change=${status}`);

  render(<IdentityMethodSettings onClose={() => undefined} />);

  expect(await screen.findByRole("alert")).toHaveTextContent(message);
  expect(window.location.search).toBe("");
});

test("shows only fixed method labels and replays an unknown email request exactly", async () => {
  api.requestIdentityEmailCode
    .mockRejectedValueOnce(
      new MutationOutcomeUnknownError("email submission outcome is unknown"),
    )
    .mockResolvedValueOnce({
      challenge_id: "challenge",
      expires_at: "2026-08-21T00:05:00Z",
    });
  const user = userEvent.setup();

  render(<IdentityMethodSettings onClose={() => undefined} />);
  await screen.findByText("Linked");
  expect(screen.getByText("Email")).toBeVisible();
  expect(screen.getByText("Google")).toBeVisible();
  expect(screen.getByText("GitHub")).toBeVisible();
  expect(screen.queryByText(/@/)).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Confirm with Email" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("unknown");
  await user.click(screen.getByRole("button", { name: "Try sending again" }));
  await waitFor(() =>
    expect(api.requestIdentityEmailCode).toHaveBeenCalledTimes(2),
  );
  expect(api.requestIdentityEmailCode.mock.calls[0]).toEqual(
    api.requestIdentityEmailCode.mock.calls[1],
  );
});

test("requires another linked method before removing the method used for proof", async () => {
  api.identityMethods.mockResolvedValue({
    methods: ["email", "google"],
    reauthentication_required: false,
  });
  api.unlinkIdentityMethod.mockRejectedValue(
    new APIResponseError("recent confirmation is required", 428),
  );
  const user = userEvent.setup();

  render(<IdentityMethodSettings onClose={() => undefined} />);
  await screen.findByText("GitHub");
  await user.click(screen.getAllByRole("button", { name: "Remove" })[0]!);

  expect(
    await screen.findByRole("heading", {
      name: "Confirm another linked method",
    }),
  ).toBeVisible();
  expect(screen.getByText(/other than Email/)).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Confirm with Email" }),
  ).not.toBeInTheDocument();
  expect(
    screen.getByRole("button", { name: "Confirm with Google" }),
  ).toBeVisible();
});

test("replays email verification and unlink with stable command identities", async () => {
  api.identityMethods
    .mockResolvedValueOnce({
      methods: ["google"],
      reauthentication_required: false,
    })
    .mockResolvedValue({
      methods: ["email", "google"],
      reauthentication_required: false,
    });
  api.requestIdentityEmailCode.mockResolvedValue({
    challenge_id: "challenge",
    expires_at: "2026-08-21T00:05:00Z",
  });
  api.verifyIdentityEmailCode
    .mockRejectedValueOnce(new MutationOutcomeUnknownError("verify unknown"))
    .mockResolvedValueOnce(undefined);
  api.unlinkIdentityMethod
    .mockRejectedValueOnce(new MutationOutcomeUnknownError("unlink unknown"))
    .mockResolvedValueOnce(undefined);
  const user = userEvent.setup();

  render(<IdentityMethodSettings onClose={() => undefined} />);
  await screen.findByLabelText("Email to link");
  await user.type(screen.getByLabelText("Email to link"), "new@example.com");
  await user.click(screen.getByRole("button", { name: "Link Email" }));
  await user.type(
    await screen.findByLabelText("Newest six-digit email code"),
    "123456",
  );
  await user.click(screen.getAllByRole("button", { name: "Link Email" })[0]!);
  expect(await screen.findByRole("alert")).toHaveTextContent("verify unknown");
  await user.click(screen.getAllByRole("button", { name: "Link Email" })[0]!);
  await waitFor(() =>
    expect(api.verifyIdentityEmailCode).toHaveBeenCalledTimes(2),
  );
  expect(api.verifyIdentityEmailCode.mock.calls[0]).toEqual(
    api.verifyIdentityEmailCode.mock.calls[1],
  );

  await user.click(screen.getAllByRole("button", { name: "Remove" })[0]!);
  expect(await screen.findByRole("alert")).toHaveTextContent("unlink unknown");
  await user.click(screen.getByRole("button", { name: "Try removing again" }));
  await waitFor(() =>
    expect(api.unlinkIdentityMethod).toHaveBeenCalledTimes(2),
  );
  expect(api.unlinkIdentityMethod.mock.calls[0]).toEqual(
    api.unlinkIdentityMethod.mock.calls[1],
  );
});
