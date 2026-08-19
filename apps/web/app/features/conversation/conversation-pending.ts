const storagePrefix = "carry.pending-conversation.v1";
const requestIDPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

export function loadPendingConversationRequestID(
  memberID: string,
  spaceID: string,
): string | null {
  const requestID = window.sessionStorage.getItem(
    pendingConversationStorageKey(memberID, spaceID),
  );
  if (requestID !== null && !requestIDPattern.test(requestID)) {
    throw new Error("Pending private message identity is invalid");
  }
  return requestID;
}

export function pendingConversationRequestID(
  memberID: string,
  spaceID: string,
): string {
  const existing = loadPendingConversationRequestID(memberID, spaceID);
  if (existing) return existing;

  const requestID = crypto.randomUUID();
  const storageKey = pendingConversationStorageKey(memberID, spaceID);
  window.sessionStorage.setItem(storageKey, requestID);
  if (window.sessionStorage.getItem(storageKey) !== requestID) {
    throw new Error("Pending private message identity could not be saved");
  }
  return requestID;
}

export function clearPendingConversationRequestID(
  memberID: string,
  spaceID: string,
  requestID: string,
): void {
  const storageKey = pendingConversationStorageKey(memberID, spaceID);
  if (window.sessionStorage.getItem(storageKey) !== requestID) return;
  window.sessionStorage.removeItem(storageKey);
  if (window.sessionStorage.getItem(storageKey) === requestID) {
    throw new Error("Pending private message identity could not be cleared");
  }
}

function pendingConversationStorageKey(
  memberID: string,
  spaceID: string,
): string {
  return `${storagePrefix}:${encodeURIComponent(memberID)}:${encodeURIComponent(spaceID)}`;
}
