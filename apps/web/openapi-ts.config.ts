/// <reference types="node" />

import { defineConfig } from "@hey-api/openapi-ts";

export default defineConfig({
  input: "../../protocol/user/v1/openapi.yaml",
  output: {
    path: process.env.CARRY_WEB_GENERATED_OUTPUT ?? "./app/generated",
    postProcess: ["prettier"],
  },
  plugins: [
    "@hey-api/typescript",
    "@hey-api/client-fetch",
    {
      name: "@hey-api/sdk",
      validator: {
        request: false,
        response: true,
      },
    },
    {
      name: "zod",
      compatibilityVersion: 4,
      dates: { offset: true },
    },
  ],
});
