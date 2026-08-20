/// <reference types="node" />

import { readFile, rm } from "node:fs/promises";

import { expect, type Page } from "@playwright/test";

export async function signInWithEmail(
  page: Page,
  emailCaptureFile: string,
  email: string,
) {
  await rm(emailCaptureFile, { force: true });
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Send code" }).click();

  const code = await readLatestEmailCode(emailCaptureFile);
  await page.getByLabel("Email code").fill(code);
  await page.getByRole("button", { name: "Verify" }).click();
}

export async function readLatestEmailCode(
  emailCaptureFile: string,
  previousCode = "",
): Promise<string> {
  let code = "";
  await expect
    .poll(
      async () => {
        try {
          code = (await readFile(emailCaptureFile, "utf8")).trim();
        } catch {
          code = "";
        }
        return code !== previousCode ? code : "";
      },
      { message: "wait for the local Resend fixture to capture the code" },
    )
    .toMatch(/^\d{6}$/);
  return code;
}
