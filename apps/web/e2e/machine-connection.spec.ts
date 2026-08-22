/// <reference types="node" />

import { existsSync, writeFileSync } from "node:fs";

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
const offlineRequestFile = process.env.CARRY_MACHINE_OFFLINE_REQUEST_FILE;
const offlineReadyFile = process.env.CARRY_MACHINE_OFFLINE_READY_FILE;
const restartRequestFile = process.env.CARRY_MACHINE_RESTART_REQUEST_FILE;
const onlineReadyFile = process.env.CARRY_MACHINE_ONLINE_READY_FILE;

if (
  !emailCaptureFile ||
  !email ||
  !code ||
  !fingerprint ||
  !machineName ||
  !serverOrigin ||
  !spaceName ||
  !offlineRequestFile ||
  !offlineReadyFile ||
  !restartRequestFile ||
  !onlineReadyFile
) {
  throw new Error("Machine connection product fixture environment is required");
}
const expectedSpaceName = spaceName;

test("member adds, inventories, recovers, and remotely revokes one exact Host", async ({
  page,
}) => {
  await page.goto(webURL);
  await signInWithEmail(page, emailCaptureFile, email);
  await page.getByRole("link", { name: new RegExp(expectedSpaceName) }).click();
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "Hosts" }).click();
  await page.getByRole("link", { name: "Add Host" }).click();
  await expect(page).toHaveURL(`${webURL}/machine-connect`);
  expect(new URL(page.url()).search).toBe("");

  await page.getByLabel("Code shown by carry setup").fill(code);
  await page.getByRole("button", { name: "Review Host" }).click();

  await expect(page.getByText(serverOrigin, { exact: true })).toBeVisible();
  await expect(page.getByText(machineName, { exact: true })).toBeVisible();
  await expect(page.getByText(fingerprint, { exact: true })).toBeVisible();
  await expect(page.getByText(code, { exact: true })).toBeVisible();
  await expect(page.getByLabel("Space")).toHaveValue(/.+/);
  await expect(page.getByLabel("Space").locator("option:checked")).toHaveText(
    spaceName,
  );

  await page.getByRole("button", { name: "Connect Host" }).click();
  await expect(
    page.getByRole("heading", { name: "Host approved" }),
  ).toBeVisible();
  expect(
    await page.evaluate(() => localStorage.length + sessionStorage.length),
  ).toBe(0);

  // Approval alone does not create a Machine. The terminal owns the first
  // cadence-conforming redeem and certificate installation.
  await page.waitForTimeout(6_000);
  await openHosts(page);
  const machineRow = page
    .getByRole("listitem")
    .filter({ has: page.getByText(machineName, { exact: true }) });
  await expect(machineRow).toBeVisible();
  await expect(machineRow).toContainText("Active");
  const agentList = machineRow.getByRole("list", {
    name: `${machineName} Agents`,
  });
  const piRow = agentList.getByRole("listitem").filter({ hasText: "Pi" });
  const codexRow = agentList.getByRole("listitem").filter({ hasText: "Codex" });
  await expect(piRow).toContainText("Active · Online");
  await expect(codexRow).toContainText("Active · Online");
  await expect(machineRow).toContainText("Owned by Carry Member");
  const piLastActive = lastActiveValue(
    await piRow.getByText(/Last active/).textContent(),
  );
  const codexLastActive = lastActiveValue(
    await codexRow.getByText(/Last active/).textContent(),
  );

  writeFileSync(offlineRequestFile, "offline", { mode: 0o600 });
  await expect.poll(() => existsSync(offlineReadyFile)).toBe(true);
  await page.reload();
  await openHostsFromSpace(page);
  await expect(piRow).toContainText("Active · Offline");
  await expect(codexRow).toContainText("Active · Offline");
  await expect(piRow.getByText(/Last active/)).toContainText(
    `Last active ${piLastActive}`,
  );
  await expect(codexRow.getByText(/Last active/)).toContainText(
    `Last active ${codexLastActive}`,
  );

  writeFileSync(restartRequestFile, "restart", { mode: 0o600 });
  await expect.poll(() => existsSync(onlineReadyFile)).toBe(true);
  await page.reload();
  await openHostsFromSpace(page);
  await expect(piRow).toContainText("Active · Online");
  await expect(codexRow).toContainText("Active · Online");
  await expect(piRow.getByText(/Last active/)).toBeVisible();
  await expect(codexRow.getByText(/Last active/)).toBeVisible();
  const recoveredPiLastActive = lastActiveValue(
    await piRow.getByText(/Last active/).textContent(),
  );
  const recoveredCodexLastActive = lastActiveValue(
    await codexRow.getByText(/Last active/).textContent(),
  );

  await machineRow
    .getByRole("button", { name: "Revoke Host", exact: true })
    .click();
  await expect(page.getByRole("dialog", { name: "Revoke Host" })).toContainText(
    fingerprint,
  );
  await expect(page.getByRole("dialog", { name: "Revoke Host" })).toContainText(
    "does not prove a process stopped",
  );
  await page
    .getByRole("dialog", { name: "Revoke Host" })
    .getByRole("button", { name: "Revoke Host" })
    .click();
  await expect(
    page.getByText(machineName, { exact: true }).locator(".."),
  ).toContainText("Revoked");
  await expect(piRow).toContainText("Removed · Offline");
  await expect(codexRow).toContainText("Removed · Offline");
  await expect(piRow.getByText(/Last active/)).toContainText(
    `Last active ${recoveredPiLastActive}`,
  );
  await expect(codexRow.getByText(/Last active/)).toContainText(
    `Last active ${recoveredCodexLastActive}`,
  );
});

function lastActiveValue(copy: string | null) {
  const value = copy?.replace(/^.*Last active /, "");
  if (!value) throw new Error("Agent Last active is required");
  return value;
}

async function openHosts(page: import("@playwright/test").Page) {
  await page.goto(webURL);
  await page.getByRole("link", { name: new RegExp(expectedSpaceName) }).click();
  await openHostsFromSpace(page);
}

async function openHostsFromSpace(page: import("@playwright/test").Page) {
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "Hosts" }).click();
  await expect(
    page.getByRole("heading", { name: "Hosts and Agents" }),
  ).toBeVisible();
}
