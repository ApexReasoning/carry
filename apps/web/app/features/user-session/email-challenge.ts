import { requestEmailCode } from "../../carry-api";

export type EmailChallengeCommand = {
  challengeID: string;
  email: string;
  requestKey: string;
};

export function newEmailChallenge(email: string): EmailChallengeCommand {
  return {
    challengeID: crypto.randomUUID(),
    email: email.trim(),
    requestKey: crypto.randomUUID(),
  };
}

export async function requestExactEmailChallenge(
  command: EmailChallengeCommand,
): Promise<void> {
  await requestEmailCode(
    command.email,
    command.challengeID,
    command.requestKey,
  );
}
