const storageKey = "carry.pending-work-mutations.v1";

type PendingMutation = {
  operation: "create" | "message" | "retry";
  memberID: string;
  spaceID: string;
  workID?: string;
  digest: string;
  idempotencyKey: string;
};

type PendingMutations = Record<string, PendingMutation>;

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

export function clearPendingIdentity(identity: MutationIdentity): void {
  const pending = loadPending();
  if (pending[identity.digest]?.idempotencyKey !== identity.idempotencyKey) {
    return;
  }
  delete pending[identity.digest];
  savePending(pending);
}

async function pendingIdentity(
  command: Omit<PendingMutation, "digest" | "idempotencyKey">,
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
  if (existing) {
    return { digest, idempotencyKey: existing.idempotencyKey };
  }
  const created: PendingMutation = {
    ...command,
    digest,
    idempotencyKey: crypto.randomUUID(),
  };
  pending[digest] = created;
  savePending(pending);
  return { digest, idempotencyKey: created.idempotencyKey };
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
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new Error("Pending Work identities are invalid");
  }
  return value as PendingMutations;
}

function savePending(pending: PendingMutations): void {
  if (Object.keys(pending).length === 0) {
    window.sessionStorage.removeItem(storageKey);
    return;
  }
  window.sessionStorage.setItem(storageKey, JSON.stringify(pending));
}
