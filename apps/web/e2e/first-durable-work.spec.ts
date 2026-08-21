/// <reference types="node" />

import { expect, test } from "@playwright/test";

import { signInWithEmail } from "./email-login";

test("new User creates a Space and continues durable Work without browser credential storage", async ({
  context,
  page,
}) => {
  const emailCaptureFile = process.env.CARRY_EMAIL_CAPTURE_FILE;
  const loginEmail = process.env.CARRY_LOGIN_EMAIL;
  if (!emailCaptureFile || !loginEmail) {
    throw new Error(
      "CARRY_EMAIL_CAPTURE_FILE and CARRY_LOGIN_EMAIL are required",
    );
  }

  await page.goto("/");
  await signInWithEmail(page, emailCaptureFile, loginEmail);
  await page.getByLabel("Space name").fill("Research");
  await page.getByRole("button", { name: "Create Space" }).click();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  const browserSession = (await context.cookies()).find(
    (cookie) => cookie.name === "__Host-carry_session",
  );
  expect(browserSession?.httpOnly).toBe(true);
  expect(browserSession?.secure).toBe(true);
  expect(browserSession?.sameSite).toBe("Strict");
  await expect.poll(() => page.evaluate(() => localStorage.length)).toBe(0);
  await expect.poll(() => page.evaluate(() => sessionStorage.length)).toBe(0);

  await page
    .getByLabel("What should Carry take responsibility for?")
    .fill("Prepare the quarterly customer research brief");
  await page.getByRole("button", { name: "Create Work" }).click();
  await expect(
    page.getByRole("heading", {
      name: "Prepare the quarterly customer research brief",
    }),
  ).toBeVisible();

  await page
    .getByLabel("Add information for Carry")
    .fill("Include themes from the support queue");
  await page.getByRole("button", { name: "Add message" }).click();
  await expect(
    page.getByText("Include themes from the support queue"),
  ).toBeVisible();

  await page.reload();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  await page
    .getByRole("button", {
      name: "Prepare the quarterly customer research brief",
    })
    .click();
  await expect(
    page.getByText("Include themes from the support queue"),
  ).toBeVisible();
  expect(await page.evaluate(() => Object.entries(localStorage))).toEqual([]);
  const sessionValues = JSON.stringify(
    await page.evaluate(() => Object.entries(sessionStorage)),
  );
  expect(sessionValues).not.toContain(loginEmail);
  expect(sessionValues).not.toContain("__Host-carry_session");
  expect(sessionValues).not.toMatch(/\b\d{6}\b/);
});
