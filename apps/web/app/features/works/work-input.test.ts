import { expect, test } from "vitest";

import {
  goalInputError,
  maxGoalBytes,
  maxMessageBytes,
  messageInputError,
} from "./work-input";

test("validates Work inputs by UTF-8 bytes", () => {
  expect(goalInputError("界".repeat(Math.floor(maxGoalBytes / 3)))).toBeNull();
  expect(
    goalInputError("界".repeat(Math.floor(maxGoalBytes / 3) + 1)),
  ).toContain(`${maxGoalBytes} UTF-8 bytes`);

  expect(messageInputError("界".repeat(maxMessageBytes / 3))).toBeNull();
  expect(messageInputError(`${"界".repeat(maxMessageBytes / 3)}a`)).toContain(
    `${maxMessageBytes} UTF-8 bytes`,
  );
});
