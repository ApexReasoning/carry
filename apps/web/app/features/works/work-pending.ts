const storageKey = "carry.pending-work-mutations.v1";
const digestPattern = /^[0-9a-f]{64}$/;
const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

type PendingMutations = Record<string, string>;

type MutationCommand = {
  operation: "create" | "message" | "retry" | "accept-review";
  memberID: string;
  spaceID: string;
  workID?: string;
  reviewID?: string;
};

type MutationIdentity = {
  digest: string;
  idempotencyKey: string;
};

export async function pendingCreateIdentity(
  memberID: string,
  spaceID: string,
  goal: string,
): Promise<MutationIdentity> {
  return pendingIdentity({ operation: "create", memberID, spaceID }, goal);
}

export async function pendingMessageIdentity(
  memberID: string,
  spaceID: string,
  workID: string,
  text: string,
): Promise<MutationIdentity> {
  return pendingIdentity(
    { operation: "message", memberID, spaceID, workID },
    text,
  );
}

export async function pendingRetryIdentity(
  memberID: string,
  spaceID: string,
  workID: string,
): Promise<MutationIdentity> {
  return pendingIdentity({ operation: "retry", memberID, spaceID, workID }, "");
}

export async function pendingReviewIdentity(
  memberID: string,
  spaceID: string,
  workID: string,
  reviewID: string,
): Promise<MutationIdentity> {
  return pendingIdentity(
    { operation: "accept-review", memberID, spaceID, workID, reviewID },
    "",
  );
}

export function clearPendingIdentity(identity: MutationIdentity): void {
  const pending = loadPending();
  if (pending[identity.digest] !== identity.idempotencyKey) return;
  delete pending[identity.digest];
  savePending(pending);
}

async function pendingIdentity(
  command: MutationCommand,
  content: string,
): Promise<MutationIdentity> {
  const encoded = new TextEncoder().encode(
    JSON.stringify({ ...command, content }),
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

function loadPending(): PendingMutations {
  const encoded = window.sessionStorage.getItem(storageKey);
  if (!encoded) return {};
  let value: unknown;
  try {
    value = JSON.parse(encoded);
  } catch (caught) {
    throw new Error("Pending Work identities are invalid", { cause: caught });
  }
  if (!isPendingMutations(value)) {
    throw new Error("Pending Work identities are invalid");
  }
  return value;
}

function isPendingMutations(value: unknown): value is PendingMutations {
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

function savePending(pending: PendingMutations): void {
  if (Object.keys(pending).length === 0) {
    window.sessionStorage.removeItem(storageKey);
    if (window.sessionStorage.getItem(storageKey) !== null) {
      throw new Error("Pending Work identity could not be cleared");
    }
    return;
  }
  const encoded = JSON.stringify(pending);
  window.sessionStorage.setItem(storageKey, encoded);
  if (window.sessionStorage.getItem(storageKey) !== encoded) {
    throw new Error("Pending Work identity could not be saved");
  }
}
