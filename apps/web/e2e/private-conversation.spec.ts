/// <reference types="node" />

import { expect, test, type Page } from "@playwright/test";

import { signInWithEmail } from "./email-login";

const questionText =
  "What should I check before a customer renewal? Private reference ORCHID-QUESTION-739.";
const questionReply =
  "Review the renewal date, notice window, owner, and approval dependencies first.";
const delegationText =
  "Carry, take responsibility for preparing the renewal brief. Private reference ORCHID-DELEGATION-842.";
const delegationReply = "I’ll keep the renewal brief moving as shared Work.";
const delegationGoal = "Prepare the renewal brief";
const questionRequestID = "11111111-1111-4111-8111-111111111111";
const delegationRequestID = "22222222-2222-4222-8222-222222222222";

test("member talks privately to Carry and delegates one shared Work", async ({
  context,
  page,
}) => {
  const emailCaptureFile = process.env.CARRY_EMAIL_CAPTURE_FILE;
  if (!emailCaptureFile) {
    throw new Error("CARRY_EMAIL_CAPTURE_FILE is required");
  }

  await page.goto("/");
  await signInWithEmail(page, emailCaptureFile, "conversation@example.com");
  await page.getByRole("link", { name: /Carry Space/ }).click();
  await expect(
    page.getByRole("heading", { name: "Talk to Carry" }),
  ).toBeVisible();
  const browserSession = (await context.cookies()).find(
    (cookie) => cookie.name === "__Host-carry_session",
  );
  expect(browserSession?.httpOnly).toBe(true);
  expect(browserSession?.secure).toBe(true);

  await useNextRequestID(page, questionRequestID);
  await page.getByLabel("Message Carry privately").fill(questionText);
  await page.getByRole("button", { name: "Send privately" }).click();
  await expect(page.getByText(questionReply, { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Open Work" })).toHaveCount(0);

  await page.reload();
  await expect(page.getByText(questionText, { exact: true })).toBeVisible();
  await expect(page.getByText(questionReply, { exact: true })).toBeVisible();

  await useNextRequestID(page, delegationRequestID);
  await page.getByLabel("Message Carry privately").fill(delegationText);
  await page.getByRole("button", { name: "Send privately" }).click();
  await expect(page.getByText(delegationReply, { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Open Work" }).click();
  await expect(
    page.getByRole("heading", { name: delegationGoal, exact: true }),
  ).toBeVisible();
  await expect(
    page.getByText("Carry owns the renewal brief.", { exact: true }),
  ).toBeVisible();

  await expect.poll(() => page.evaluate(() => localStorage.length)).toBe(0);
  const sessionStorageEntries = await page.evaluate(() =>
    Object.entries(sessionStorage),
  );
  const persistedSessionState = JSON.stringify(sessionStorageEntries);
  for (const privateValue of [
    questionText,
    delegationText,
    "ORCHID-QUESTION-739",
    "ORCHID-DELEGATION-842",
    "conversation@example.com",
  ]) {
    expect(persistedSessionState).not.toContain(privateValue);
  }

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByLabel("Email")).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Talk to Carry" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: delegationGoal, exact: true }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "Shared Work", exact: true }),
  ).toHaveCount(0);
  expect(
    (await context.cookies()).some(
      (cookie) => cookie.name === "__Host-carry_session",
    ),
  ).toBe(false);
});

async function useNextRequestID(page: Page, requestID: string): Promise<void> {
  await page.evaluate((nextRequestID) => {
    Object.defineProperty(globalThis.crypto, "randomUUID", {
      configurable: true,
      value: () => nextRequestID,
    });
  }, requestID);
}
