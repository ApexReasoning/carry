import {
  currentUser,
  MutationOutcomeUnknownError,
  verifyEmailCode,
} from "../../carry-api";
import type { User } from "../../generated/types.gen";

export type EmailVerificationCommand = {
  challengeID: string;
  code: string;
  requestKey: string;
};

export function newEmailVerification(
  challengeID: string,
  code: string,
): EmailVerificationCommand {
  return { challengeID, code, requestKey: crypto.randomUUID() };
}

export async function verifyExactEmailChallenge(
  command: EmailVerificationCommand,
): Promise<User> {
  try {
    await verifyEmailCode(
      command.challengeID,
      command.code,
      command.requestKey,
    );
  } catch (caught) {
    if (!(caught instanceof MutationOutcomeUnknownError)) throw caught;

    // A current Session could belong to another tab. Replay the exact mutation
    // before loading User state so it never stands in as proof of this command.
    try {
      await verifyEmailCode(
        command.challengeID,
        command.code,
        command.requestKey,
      );
    } catch {
      // The following read routes from actual state without claiming this
      // verification succeeded.
    }
    const loaded = await currentUser();
    if (loaded) return loaded;
    throw caught;
  }

  const loaded = await currentUser();
  if (!loaded) throw new Error("Carry did not establish a browser session");
  return loaded;
}
