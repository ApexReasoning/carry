import { createSpace, MutationOutcomeUnknownError } from "../../carry-api";
import type { Membership } from "../../generated/types.gen";

const storageKey = "carry.pending-space-creation.v1";
const digestPattern = /^[0-9a-f]{64}$/;
const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

type PendingIdentities = Record<string, string>;

type CreationIdentity = {
  digest: string;
  idempotencyKey: string;
};

export async function createExactSpace(
  memberID: string,
  name: string,
  suffix?: number,
): Promise<Membership> {
  const identity = await pendingSpaceCreationIdentity(memberID, name, suffix);
  const attempt = async () => {
    const created = await createSpace(name, suffix, identity.idempotencyKey);
    clearPendingSpaceCreation(identity);
    return created;
  };
  try {
    return await attempt();
  } catch (caught) {
    if (!(caught instanceof MutationOutcomeUnknownError)) {
      clearPendingSpaceCreation(identity);
      throw caught;
    }
  }
  try {
    return await attempt();
  } catch (caught) {
    if (!(caught instanceof MutationOutcomeUnknownError)) {
      clearPendingSpaceCreation(identity);
    }
    throw caught;
  }
}

export async function pendingSpaceCreationIdentity(
  memberID: string,
  name: string,
  suffix?: number,
): Promise<CreationIdentity> {
  const encoded = new TextEncoder().encode(
    JSON.stringify({ memberID, name, suffix: suffix ?? null }),
  );
  const digestBytes = new Uint8Array(
    await crypto.subtle.digest("SHA-256", encoded),
  );
  const digest = Array.from(digestBytes, (value) =>
    value.toString(16).padStart(2, "0"),
  ).join("");
  const pending = loadPending();
  const existing = pending[digest];
  if (existing) return { digest, idempotencyKey: existing };

  const idempotencyKey = crypto.randomUUID();
  pending[digest] = idempotencyKey;
  savePending(pending);
  return { digest, idempotencyKey };
}

function clearPendingSpaceCreation(identity: CreationIdentity): void {
  const pending = loadPending();
  if (pending[identity.digest] !== identity.idempotencyKey) return;
  delete pending[identity.digest];
  savePending(pending);
}

function loadPending(): PendingIdentities {
  const encoded = window.sessionStorage.getItem(storageKey);
  if (!encoded) return {};
  let value: unknown;
  try {
    value = JSON.parse(encoded);
  } catch (caught) {
    throw new Error("Pending Space creation identities are invalid", {
      cause: caught,
    });
  }
  if (!isPendingIdentities(value)) {
    throw new Error("Pending Space creation identities are invalid");
  }
  return value;
}

function isPendingIdentities(value: unknown): value is PendingIdentities {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  return Object.entries(value).every(
    ([digest, idempotencyKey]) =>
      digestPattern.test(digest) &&
      typeof idempotencyKey === "string" &&
      uuidPattern.test(idempotencyKey),
  );
}

function savePending(pending: PendingIdentities): void {
  if (Object.keys(pending).length === 0) {
    window.sessionStorage.removeItem(storageKey);
    if (window.sessionStorage.getItem(storageKey) !== null) {
      throw new Error("Pending Space creation identity could not be cleared");
    }
    return;
  }
  const encoded = JSON.stringify(pending);
  window.sessionStorage.setItem(storageKey, encoded);
  if (window.sessionStorage.getItem(storageKey) !== encoded) {
    throw new Error("Pending Space creation identity could not be saved");
  }
}
