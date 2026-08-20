/// <reference types="node" />

import { rm } from "node:fs/promises";

import { expect, test, type Page, type Route } from "@playwright/test";

import { readLatestEmailCode, signInWithEmail } from "./email-login";

const emailCaptureFile = process.env.CARRY_EMAIL_CAPTURE_FILE;
const loginEmail = process.env.CARRY_LOGIN_EMAIL;

if (!emailCaptureFile || !loginEmail) {
  throw new Error(
    "CARRY_EMAIL_CAPTURE_FILE and CARRY_LOGIN_EMAIL are required",
  );
}

test.beforeEach(async () => {
  await rm(emailCaptureFile, { force: true });
});

test("provider entry keeps methods separate and cleans neutral callback status", async ({
  page,
}) => {
  await page.goto("/?sign_in=unavailable");

  await expect(
    page.getByRole("button", { name: "Continue with Google" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Continue with GitHub" }),
  ).toBeVisible();
  await expect(
    page.getByText(/Google, GitHub, and email accounts are not combined yet/),
  ).toBeVisible();
  await expect(page.getByRole("alert")).toContainText(
    "Carry could not confirm sign-in. Start a fresh sign-in.",
  );
  const forms = await page
    .locator(".provider-entry form")
    .evaluateAll((items) =>
      items.map((item) => ({
        action: item.getAttribute("action"),
        method: item.getAttribute("method"),
      })),
    );
  expect(forms).toEqual([
    { action: "/v1/auth/google/start", method: "post" },
    { action: "/v1/auth/github/start", method: "post" },
  ]);
  expect(new URL(page.url()).search).toBe("");
  expect(
    await page.evaluate(() => [localStorage.length, sessionStorage.length]),
  ).toEqual([0, 0]);
});

test("email response loss replays exact commands and logout remains fail closed", async ({
  context,
  page,
}) => {
  const email = derivedEmail(loginEmail, "unknown");
  const challengeRequests: Array<RecordedMutation> = [];
  let challengeAttempt = 0;
  await page.route("**/v1/auth/email/challenges", async (route) => {
    challengeRequests.push(recordMutation(route));
    challengeAttempt += 1;
    if (challengeAttempt === 1) {
      await route.fetch();
      await route.abort("failed");
      return;
    }
    await route.continue();
  });

  await page.goto("/");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send code" }).click();
  await expect(
    page.getByText(/Carry may have sent the code\. Retry this exact request/),
  ).toBeVisible();
  await page.getByRole("button", { name: "Retry this request" }).click();
  await expect(page.getByLabel("Email code")).toBeVisible();
  expect(challengeRequests).toHaveLength(2);
  expect(challengeRequests[1]).toEqual(challengeRequests[0]);
  const code = await readLatestEmailCode(emailCaptureFile);

  const wrongCode = code === "000000" ? "000001" : "000000";
  await page.getByLabel("Email code").fill(wrongCode);
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "email code is invalid or expired",
  );

  const verificationRequests: Array<RecordedMutation> = [];
  let verificationAttempt = 0;
  await page.route("**/v1/auth/email/challenges/*/verify", async (route) => {
    verificationRequests.push(recordMutation(route));
    verificationAttempt += 1;
    if (verificationAttempt === 1) {
      await route.fetch();
      await route.abort("failed");
      return;
    }
    await route.continue();
  });
  await page.getByLabel("Email code").fill(code);
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(
    page.getByRole("heading", { name: "Create your Space" }),
  ).toBeVisible();
  expect(verificationRequests).toHaveLength(2);
  expect(verificationRequests[1]).toEqual(verificationRequests[0]);

  const spaceRequests: Array<RecordedMutation> = [];
  let spaceAttempt = 0;
  await page.route("**/v1/spaces", async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    spaceRequests.push(recordMutation(route));
    spaceAttempt += 1;
    if (spaceAttempt === 1) {
      await route.fetch();
      await route.abort("failed");
      return;
    }
    await route.continue();
  });
  await page.getByLabel("Your name").fill("Alex Morgan");
  await page.getByLabel("Space name").fill("Research response loss");
  await page.getByRole("button", { name: "Create Space" }).click();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  expect(spaceRequests).toHaveLength(2);
  expect(spaceRequests[1]).toEqual(spaceRequests[0]);

  await page.route("**/v1/browser/sessions/current", async (route) => {
    await route.abort("failed");
  });
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(
    page.getByText("Your Work is hidden on this browser."),
  ).toBeVisible();
  await expect(page.getByRole("alert")).toContainText(
    "Sign-out revocation is not confirmed",
  );
  await page.reload();
  await expect(
    page.getByText("Your Work is hidden on this browser."),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toHaveCount(0);
  await page.unroute("**/v1/browser/sessions/current");
  await page.getByRole("button", { name: "Finish signing out" }).click();
  await expect(page.getByLabel("Email")).toBeVisible();
  expect(
    (await context.cookies()).find(
      (cookie) => cookie.name === "__Host-carry_session",
    ),
  ).toBeUndefined();

  await expectNoBrowserSecrets(page, email, [code, wrongCode]);
});

test("explicit resend makes the older code obsolete and the newest code succeeds", async ({
  page,
}) => {
  test.setTimeout(90_000);
  const email = derivedEmail(loginEmail, "resend");
  await page.goto("/");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send code" }).click();
  const oldCode = await readLatestEmailCode(emailCaptureFile);

  // Production policy is fixed at sixty seconds. This Browser evidence waits
  // for it rather than adding a configurable or test-only security path.
  await page.waitForTimeout(61_000);
  await page.getByRole("button", { name: "Send a new code" }).click();
  const newestCode = await readLatestEmailCode(emailCaptureFile, oldCode);

  await page.getByLabel("Email code").fill(oldCode);
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "email code is invalid or expired",
  );
  await page.getByLabel("Email code").fill(newestCode);
  await page.getByRole("button", { name: "Verify" }).click();
  await expect(
    page.getByRole("heading", { name: "Create your Space" }),
  ).toBeVisible();
  await expectNoBrowserSecrets(page, email, [oldCode, newestCode]);
});

test("a concurrent other first Space routes only from actual User state", async ({
  context,
  page,
}) => {
  const email = derivedEmail(loginEmail, "concurrent");
  await page.goto("/");
  await signInWithEmail(page, emailCaptureFile, email);
  await expect(
    page.getByRole("heading", { name: "Create your Space" }),
  ).toBeVisible();

  const otherTab = await context.newPage();
  await otherTab.goto("/");
  await expect(
    otherTab.getByRole("heading", { name: "Create your Space" }),
  ).toBeVisible();

  const attemptedCommands: Array<RecordedMutation> = [];
  let attempt = 0;
  await page.route("**/v1/spaces", async (route) => {
    if (route.request().method() !== "POST") {
      await route.continue();
      return;
    }
    attemptedCommands.push(recordMutation(route));
    attempt += 1;
    if (attempt === 1) {
      const status = await otherTab.evaluate(async () => {
        const response = await fetch("/v1/spaces", {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            "Idempotency-Key": crypto.randomUUID(),
          },
          body: JSON.stringify({
            display_name: "Alex Morgan",
            name: "Operations won elsewhere",
          }),
        });
        return response.status;
      });
      expect(status).toBe(201);
      await route.abort("failed");
      return;
    }
    await route.continue();
  });

  await page.getByLabel("Your name").fill("Alex Morgan");
  await page.getByLabel("Space name").fill("Attempted Research");
  await page.getByRole("button", { name: "Create Space" }).click();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  await expect(page.getByText("Operations won elsewhere")).toBeVisible();
  await expect(page.getByText("Attempted Research")).toHaveCount(0);
  expect(attemptedCommands).toHaveLength(2);
  expect(attemptedCommands[1]).toEqual(attemptedCommands[0]);
});

test("Settings presents only fixed sign-in method labels and protects the final method", async ({
  page,
}) => {
  const email = derivedEmail(loginEmail, "identity-settings");
  await page.goto("/");
  await signInWithEmail(page, emailCaptureFile, email);
  await page.getByLabel("Your name").fill("Identity User");
  await page.getByLabel("Space name").fill("Identity Settings");
  await page.getByRole("button", { name: "Create Space" }).click();
  await page.getByRole("button", { name: "Settings" }).click();

  await expect(
    page.getByRole("heading", { name: "Sign-in methods" }),
  ).toBeVisible();
  await expect(page.getByText("Email", { exact: true })).toBeVisible();
  await expect(page.getByText("Google", { exact: true })).toBeVisible();
  await expect(page.getByText("GitHub", { exact: true })).toBeVisible();
  await expect(page.getByText(email)).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Remove" })).toBeDisabled();
  const providerForms = await page
    .locator(".identity-method-list form")
    .evaluateAll((forms) =>
      forms.map((form) => ({
        action: form.getAttribute("action"),
        method: form.getAttribute("method"),
      })),
    );
  expect(providerForms).toEqual([
    { action: "/v1/identity/methods/google/start", method: "post" },
    { action: "/v1/identity/methods/github/start", method: "post" },
  ]);
});

type RecordedMutation = {
  body: string | null;
  idempotencyKey: string | undefined;
  url: string;
};

function recordMutation(route: Route): RecordedMutation {
  const request = route.request();
  return {
    body: request.postData(),
    idempotencyKey: request.headers()["idempotency-key"],
    url: new URL(request.url()).pathname,
  };
}

function derivedEmail(email: string, journey: string): string {
  const separator = email.lastIndexOf("@");
  return `${email.slice(0, separator)}-${journey}@${email.slice(separator + 1)}`;
}

async function expectNoBrowserSecrets(
  page: Page,
  email: string,
  codes: Array<string>,
) {
  const url = page.url();
  const localValues = JSON.stringify(
    await page.evaluate(() => Object.entries(localStorage)),
  );
  const sessionValues = JSON.stringify(
    await page.evaluate(() => Object.entries(sessionStorage)),
  );
  for (const secret of [email, ...codes]) {
    expect(url).not.toContain(secret);
    expect(localValues).not.toContain(secret);
    expect(sessionValues).not.toContain(secret);
  }
}
