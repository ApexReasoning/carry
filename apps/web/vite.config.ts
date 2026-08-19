/// <reference types="node" />

import { readFileSync } from "node:fs";

import react from "@vitejs/plugin-react";
import { configDefaults, defineConfig } from "vitest/config";

const certificatePath = process.env.CARRY_WEB_TLS_CERT;
const privateKeyPath = process.env.CARRY_WEB_TLS_KEY;
const apiTarget = process.env.CARRY_API_URL;
const https =
  certificatePath && privateKeyPath
    ? { cert: readFileSync(certificatePath), key: readFileSync(privateKeyPath) }
    : undefined;
const proxy = apiTarget
  ? {
      "/v1": {
        target: apiTarget,
        secure: false,
      },
    }
  : undefined;

export default defineConfig({
  plugins: [react()],
  server: { https, proxy },
  preview: { https, proxy },
  test: {
    environment: "jsdom",
    exclude: [...configDefaults.exclude, "e2e/**"],
    environmentOptions: {
      jsdom: { url: "https://carry.example/" },
    },
    setupFiles: ["./vitest.setup.ts"],
  },
});
