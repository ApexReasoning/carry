import {
  createFirstSpace,
  currentUser,
  MutationOutcomeUnknownError,
} from "../../carry-api";
import type { User } from "../../generated/types.gen";

export type FirstSpaceCommand = {
  displayName: string;
  name: string;
  requestKey: string;
};

export function newFirstSpace(
  displayName: string,
  name: string,
): FirstSpaceCommand {
  return { displayName, name, requestKey: crypto.randomUUID() };
}

export async function createExactFirstSpace(
  command: FirstSpaceCommand,
): Promise<User> {
  try {
    await createFirstSpace(
      command.displayName,
      command.name,
      command.requestKey,
    );
  } catch (caught) {
    if (!(caught instanceof MutationOutcomeUnknownError)) throw caught;

    // Another tab's Space is state, not proof that this command succeeded.
    // Replay this exact mutation first, then route from the current User.
    try {
      await createFirstSpace(
        command.displayName,
        command.name,
        command.requestKey,
      );
    } catch {
      // A concurrent first-Space winner is expected to reject this command.
    }
    const loaded = await currentUser();
    if (loaded?.spaces.length) return loaded;
    throw caught;
  }

  const loaded = await currentUser();
  if (!loaded?.spaces.length) throw new Error("Carry did not create the Space");
  return loaded;
}
