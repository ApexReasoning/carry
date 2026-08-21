/// <reference types="node" />

import { expect, test } from "@playwright/test";

import { signInWithEmail } from "./email-login";

const webURL = process.env.CARRY_WEB_URL ?? "https://127.0.0.1:4173";
const emailCaptureFile = process.env.CARRY_EMAIL_CAPTURE_FILE;
const email = process.env.CARRY_LOGIN_EMAIL;
const code = process.env.CARRY_MACHINE_CODE;
const fingerprint = process.env.CARRY_MACHINE_FINGERPRINT;
const machineName = process.env.CARRY_MACHINE_NAME;
const serverOrigin = process.env.CARRY_MACHINE_SERVER_ORIGIN;
const spaceName = process.env.CARRY_MACHINE_SPACE_NAME;

if (
  !emailCaptureFile ||
  !email ||
  !code ||
  !fingerprint ||
  !machineName ||
  !serverOrigin ||
  !spaceName
) {
  throw new Error("Machine connection product fixture environment is required");
}

test("member approves, inventories, and remotely revokes one exact Machine", async ({
  page,
}) => {
  await page.goto(`${webURL}/machine-connect`);
  expect(new URL(page.url()).search).toBe("");
  await signInWithEmail(page, emailCaptureFile, email);

  await page.getByLabel("Code shown by carry host connect").fill(code);
  await page.getByRole("button", { name: "Review Machine" }).click();

  await expect(page.getByText(serverOrigin, { exact: true })).toBeVisible();
  await expect(page.getByText(machineName, { exact: true })).toBeVisible();
  await expect(page.getByText(fingerprint, { exact: true })).toBeVisible();
  await expect(page.getByText(code, { exact: true })).toBeVisible();
  await expect(page.getByLabel("Space")).toHaveValue(/.+/);
  await expect(page.getByLabel("Space").locator("option:checked")).toHaveText(
    spaceName,
  );

  await page.getByRole("button", { name: "Connect Machine" }).click();
  await expect(
    page.getByRole("heading", { name: "Machine approved" }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);

  // Approval alone does not create a Machine. The terminal owns the first
  // cadence-conforming redeem and certificate installation.
  await page.waitForTimeout(6_000);
  await page.goto(webURL);
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "Machines" }).click();
  const machineRow = page
    .getByRole("listitem")
    .filter({ has: page.getByText(machineName, { exact: true }) });
  await expect(machineRow).toBeVisible();
  await expect(machineRow).toContainText("Active");
  await expect(
    page.getByText(/does not mean the computer or Host process is online/),
  ).toBeVisible();

  await machineRow.getByRole("button", { name: "Revoke", exact: true }).click();
  await expect(
    page.getByRole("dialog", { name: "Revoke Machine" }),
  ).toContainText(fingerprint);
  await expect(
    page.getByRole("dialog", { name: "Revoke Machine" }),
  ).toContainText("does not prove a process stopped");
  await page.getByRole("button", { name: "Revoke Machine" }).click();
  await expect(
    page.getByText(machineName, { exact: true }).locator(".."),
  ).toContainText("Revoked");
});
