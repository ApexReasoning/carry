/// <reference types="node" />

import { rm } from "node:fs/promises";

import {
  expect,
  test,
  type Browser,
  type Page,
  type Route,
} from "@playwright/test";

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
    page.getByText(/Carry may have sent the code\. Check your inbox/),
  ).toBeVisible();
  await page.getByRole("button", { name: "Try sending again" }).click();
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
    page.getByRole("heading", { name: "Choose a Space" }),
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
  await page.getByLabel("Space name").fill("Research response loss");
  await page.getByRole("button", { name: "Create Space" }).click();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/s/research-response-loss");
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
    page.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  await expectNoBrowserSecrets(page, email, [oldCode, newestCode]);
});

test("a concurrent Space slug collision requires an explicit new request", async ({
  context,
  page,
}) => {
  const email = derivedEmail(loginEmail, "concurrent");
  await page.goto("/");
  await signInWithEmail(page, emailCaptureFile, email);
  await expect(
    page.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();

  const otherTab = await context.newPage();
  await otherTab.goto("/");
  await expect(
    otherTab.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  const status = await otherTab.evaluate(async () => {
    const response = await fetch("/v1/spaces", {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/json",
        "Idempotency-Key": crypto.randomUUID(),
      },
      body: JSON.stringify({ name: "Concurrent Space" }),
    });
    return response.status;
  });
  expect(status).toBe(201);

  const attemptedCommands: Array<RecordedMutation> = [];
  await page.route("**/v1/spaces", async (route) => {
    if (route.request().method() === "POST") {
      attemptedCommands.push(recordMutation(route));
    }
    await route.continue();
  });

  await page.getByLabel("Space name").fill("Concurrent Space");
  await page.getByRole("button", { name: "Create Space" }).click();
  await expect(
    page.getByText("/s/concurrent-space is already in use.", { exact: false }),
  ).toBeVisible();
  const createElsewhere = (suffix: number) =>
    otherTab.evaluate(async (nextSuffix) => {
      const response = await fetch("/v1/spaces", {
        method: "POST",
        credentials: "same-origin",
        headers: {
          "Content-Type": "application/json",
          "Idempotency-Key": crypto.randomUUID(),
        },
        body: JSON.stringify({ name: "Concurrent Space", suffix: nextSuffix }),
      });
      return response.status;
    }, suffix);

  expect(await createElsewhere(2)).toBe(201);
  await page.getByRole("button", { name: "Try /s/concurrent-space-2" }).click();
  await expect(
    page.getByRole("button", { name: "Try /s/concurrent-space-3" }),
  ).toBeVisible();
  expect(await createElsewhere(3)).toBe(201);
  await page.getByRole("button", { name: "Try /s/concurrent-space-3" }).click();
  await expect(
    page.getByRole("button", { name: "Try /s/concurrent-space-4" }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Try /s/concurrent-space-4" }).click();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  expect(new URL(page.url()).pathname).toBe("/s/concurrent-space-4");
  expect(attemptedCommands.map((command) => command.body)).toEqual([
    JSON.stringify({ name: "Concurrent Space" }),
    JSON.stringify({ name: "Concurrent Space", suffix: 2 }),
    JSON.stringify({ name: "Concurrent Space", suffix: 3 }),
    JSON.stringify({ name: "Concurrent Space", suffix: 4 }),
  ]);
  expect(
    new Set(attemptedCommands.map((command) => command.idempotencyKey)).size,
  ).toBe(4);
  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /\/s\/concurrent-space$/ }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /\/s\/concurrent-space-4$/ }),
  ).toBeVisible();
});

test("Unicode Space URLs remain explicit and invitation entry bypasses the chooser", async ({
  browser,
  page,
}: {
  browser: Browser;
  page: Page;
}) => {
  test.setTimeout(120_000);
  const ownerEmail = derivedEmail(loginEmail, "unicode-owner");
  await page.goto("/");
  const origin = new URL(page.url()).origin;
  await signInWithEmail(page, emailCaptureFile, ownerEmail);

  const overlongName = "研".repeat(33);
  await page.getByLabel("Space name").fill(overlongName);
  await page.getByRole("button", { name: "Create Space" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Space URL must be at most 32 characters",
  );
  expect(new URL(page.url()).pathname).toBe("/");

  const maximumName = "研".repeat(32);
  await page.getByLabel("Space name").fill(maximumName);
  await page.getByRole("button", { name: "Create Space" }).click();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  expect(decodeURIComponent(new URL(page.url()).pathname)).toBe(
    `/s/${maximumName}`,
  );
  await page.reload();
  await expect(
    page.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();

  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: new RegExp(maximumName) }),
  ).toBeVisible();

  const otherContext = await browser.newContext({ ignoreHTTPSErrors: true });
  const otherPage = await otherContext.newPage();
  await otherPage.goto(`${origin}/`);
  await signInWithEmail(
    otherPage,
    emailCaptureFile,
    derivedEmail(loginEmail, "other-space-owner"),
  );
  await otherPage.getByLabel("Space name").fill("Other Private Space");
  await otherPage.getByRole("button", { name: "Create Space" }).click();
  await expect(
    otherPage.getByRole("heading", { name: "What should Carry keep moving?" }),
  ).toBeVisible();
  await otherContext.close();

  await page.goto("/s/no-such-space");
  await expect(
    page.getByRole("heading", { name: "Space unavailable" }),
  ).toBeVisible();
  const unknownCopy = await page.locator("main.center-state").innerText();
  await page.goto("/s/other-private-space");
  await expect(
    page.getByRole("heading", { name: "Space unavailable" }),
  ).toBeVisible();
  expect(await page.locator("main.center-state").innerText()).toBe(unknownCopy);

  const recipientEmail = derivedEmail(loginEmail, "invited-member");
  await page.goto(`/s/${encodeURIComponent(maximumName)}`);
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "Members" }).click();
  await page.getByLabel("Invite one exact Email").fill(recipientEmail);
  await page.getByRole("button", { name: "Create invitation" }).click();
  const invitationRow = page
    .getByRole("listitem")
    .filter({ has: page.getByText(recipientEmail, { exact: true }) });
  const invitationLink = invitationRow.getByRole("link");
  const invitationHref = await invitationLink.getAttribute("href");
  expect(invitationHref).toMatch(/^\/invitations\/[0-9a-f-]{36}$/);
  const exactPath = invitationHref as string;
  const invitationID = exactPath.split("/").at(-1) as string;
  await page.context().grantPermissions(["clipboard-read", "clipboard-write"], {
    origin,
  });
  await invitationRow.getByRole("button", { name: "Copy link" }).click();
  await expect(invitationRow.getByText("Link copied")).toBeVisible();

  const invitedContext = await browser.newContext({ ignoreHTTPSErrors: true });
  const invitedPage = await invitedContext.newPage();
  await invitedPage.goto(`${origin}${exactPath}`);
  await expect(
    invitedPage.getByRole("heading", {
      name: "Sign in to review this invitation",
    }),
  ).toBeVisible();
  await expect(invitedPage.getByText(maximumName)).toHaveCount(0);
  await expect(invitedPage.locator('meta[name="referrer"]')).toHaveAttribute(
    "content",
    "no-referrer",
  );
  await signInWithEmail(invitedPage, emailCaptureFile, recipientEmail);
  await expect(invitedPage).toHaveURL(new RegExp(`${exactPath}$`));
  await expect(
    invitedPage.getByRole("heading", { name: "Space invitation" }),
  ).toBeVisible();
  await expect(invitedPage.getByLabel("Your name")).toHaveCount(0);
  await invitedPage.getByRole("button", { name: "Not now" }).click();
  await expect(
    invitedPage.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  await invitedPage.getByRole("link", { name: "Invitations" }).click();
  await expect(
    invitedPage.getByRole("heading", { name: "Space invitations" }),
  ).toBeVisible();
  await expect(invitedPage.getByText(maximumName)).toBeVisible();
  await invitedPage.getByRole("button", { name: "Not now" }).click();
  await expect(
    invitedPage.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  await invitedPage.getByLabel("Space name").fill("Recipient Existing Space");
  await invitedPage.getByRole("button", { name: "Create Space" }).click();
  await expect(
    invitedPage.getByRole("heading", {
      name: "What should Carry keep moving?",
    }),
  ).toBeVisible();
  await invitedPage.getByRole("link", { name: "Invitations" }).click();
  await expect(
    invitedPage.getByRole("heading", { name: "Space invitations" }),
  ).toBeVisible();
  await expect(invitedPage.getByText(maximumName)).toBeVisible();
  await invitedPage.goto(`${origin}${exactPath}`);
  await expect(
    invitedPage.getByRole("heading", { name: "Space invitation" }),
  ).toBeVisible();
  await invitedPage.route(
    `**/v1/invitations/${invitationID}/accept`,
    async (route) => {
      const response = await route.fetch();
      expect(response.status()).toBe(200);
      await route.abort("failed");
    },
    { times: 1 },
  );
  await invitedPage.getByRole("button", { name: "Accept and join" }).click();
  await expect(
    invitedPage.getByText(/cannot confirm whether you joined the Space/),
  ).toBeVisible();
  await expect(
    invitedPage.getByRole("button", { name: "Try accepting again" }),
  ).toBeVisible();
  await invitedPage
    .getByRole("button", { name: "Reload invitation status" })
    .click();
  await expect(
    invitedPage.getByText("This invitation was accepted and joined the Space."),
  ).toBeVisible();
  await expect(
    invitedPage.getByText("Your Membership is current."),
  ).toBeVisible();
  await expect(
    invitedPage.getByRole("button", { name: "Accept and join" }),
  ).toHaveCount(0);
  await invitedPage.getByRole("button", { name: "Not now" }).click();
  await expect(
    invitedPage.getByRole("heading", { name: "Choose a Space" }),
  ).toBeVisible();
  await expect(
    invitedPage.getByRole("link", { name: new RegExp(maximumName) }),
  ).toBeVisible();

  await invitedPage.goto(`${origin}${exactPath}`);
  await expect(
    invitedPage.getByText("This invitation was accepted and joined the Space."),
  ).toBeVisible();

  const wrongContext = await browser.newContext({ ignoreHTTPSErrors: true });
  const wrongPage = await wrongContext.newPage();
  await wrongPage.goto(`${origin}${exactPath}`);
  await signInWithEmail(
    wrongPage,
    emailCaptureFile,
    derivedEmail(loginEmail, "wrong-invitation-owner"),
  );
  await expect(
    wrongPage.getByText(
      "This signed-in account cannot review this invitation.",
    ),
  ).toBeVisible();
  await expect(
    wrongPage.getByText(/email address that received this invitation/),
  ).toBeVisible();
  await expect(wrongPage.getByText(maximumName)).toHaveCount(0);
  await wrongContext.close();
  await invitedContext.close();
});

test("Settings presents only fixed sign-in method labels and protects the final method", async ({
  page,
}) => {
  const email = derivedEmail(loginEmail, "identity-settings");
  await page.goto("/");
  await signInWithEmail(page, emailCaptureFile, email);
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
