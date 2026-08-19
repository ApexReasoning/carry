/// <reference types="node" />

import { expect, test } from "@playwright/test";

test("member creates and continues durable Work without browser bearer storage", async ({
  page,
}) => {
  const memberToken = process.env.CARRY_MEMBER_TOKEN;
  if (!memberToken) {
    throw new Error("CARRY_MEMBER_TOKEN is required");
  }

  await page.goto("/");
  await page.getByLabel("Member token").fill(memberToken);
  await page.getByRole("button", { name: "Open Carry" }).click();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
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
});
