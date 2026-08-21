/// <reference types="node" />

import { expect, test } from "@playwright/test";

import { signInWithEmail } from "./email-login";

test("responsible User reviews an exact result after response loss", async ({
  page,
}) => {
  const emailCaptureFile = process.env.CARRY_EMAIL_CAPTURE_FILE;
  const workGoal = process.env.CARRY_REVIEW_WORK_GOAL;
  if (!emailCaptureFile || !workGoal) {
    throw new Error(
      "CARRY_EMAIL_CAPTURE_FILE and CARRY_REVIEW_WORK_GOAL are required",
    );
  }

  await page.goto("/");
  await signInWithEmail(page, emailCaptureFile, "reviewer@example.com");
  await page.getByRole("link", { name: /Carry Space/ }).click();
  await page.getByRole("button", { name: "Needs You" }).click();
  await page.getByRole("button", { name: workGoal }).click();

  await expect(
    page.getByRole("heading", { name: "Review this result" }),
  ).toBeVisible();
  await expect(
    page.getByText("Finance approved a twelve month term."),
  ).toBeVisible();
  await expect(
    page.getByText("Prepare the renewal recommendation."),
  ).toBeVisible();

  await page.evaluate(() => {
    const originalFetch = globalThis.fetch;
    globalThis.fetch = async (...arguments_) => {
      const request =
        arguments_[0] instanceof Request
          ? arguments_[0]
          : new Request(arguments_[0], arguments_[1]);
      const response = await originalFetch(...arguments_);
      if (
        request.method === "POST" &&
        new URL(request.url).pathname.includes("/reviews/")
      ) {
        globalThis.fetch = originalFetch;
        throw new TypeError("acceptance response lost after commit");
      }
      return response;
    };
  });

  await page.getByRole("button", { name: "Accept this result" }).click();
  await expect(
    page.getByRole("heading", { name: "Review this result" }),
  ).toHaveCount(0);
  await expect(page.getByText("Open Work", { exact: true })).toBeVisible();

  await page.reload();
  await page.getByRole("button", { name: "Needs You" }).click();
  await expect(page.getByText("Nothing needs you right now.")).toBeVisible();
  await expect.poll(() => page.evaluate(() => localStorage.length)).toBe(0);
  await expect.poll(() => page.evaluate(() => sessionStorage.length)).toBe(0);
});
