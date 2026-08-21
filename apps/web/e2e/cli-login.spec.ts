/// <reference types="node" />

import { expect, test } from "@playwright/test";

import { signInWithEmail } from "./email-login";

const webURL = process.env.CARRY_WEB_URL ?? "https://127.0.0.1:4173";
const emailCaptureFile = process.env.CARRY_EMAIL_CAPTURE_FILE;
const email = process.env.CARRY_LOGIN_EMAIL;
const cliCode = process.env.CARRY_CLI_LOGIN_CODE;
const serverOrigin = process.env.CARRY_CLI_SERVER_ORIGIN;
const cliLabel = process.env.CARRY_CLI_LABEL;

if (!emailCaptureFile || !email || !cliCode || !serverOrigin || !cliLabel) {
  throw new Error("CLI login product fixture environment is required");
}

test("member reviews and explicitly approves one CLI login", async ({
  page,
}) => {
  await page.goto(`${webURL}/cli-login`);
  expect(new URL(page.url()).search).toBe("");
  await signInWithEmail(page, emailCaptureFile, email);

  await page.getByLabel("Code shown by carry login").fill(cliCode);
  await page.getByRole("button", { name: "Review login" }).click();

  await expect(page.getByText(serverOrigin, { exact: true })).toBeVisible();
  await expect(page.getByText(cliCode, { exact: true })).toBeVisible();
  await expect(page.getByText(cliLabel, { exact: true })).toBeVisible();
  await expect(page.getByText(/does not add a Membership/)).toBeVisible();
  await expect(page.getByLabel("Default Space to inspect")).toHaveValue(/.+/);

  await page.getByRole("button", { name: "Approve this CLI login" }).click();
  await expect(page.getByText(/Return to the terminal/)).toBeVisible();
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);
});
