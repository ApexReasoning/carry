import { closeBrowserSession, currentUser } from "../../carry-api";

const pendingSignOutParameter = "carry-signing-out";
const pendingSignOutStorageKey = "carry.pending-sign-out.v1";
const deferredInvitationsStorageKey = "carry.deferred-invitations.v1";
const uuidPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function hasPendingSignOut(): boolean {
  try {
    if (
      new URL(window.location.href).searchParams.has(pendingSignOutParameter)
    ) {
      return true;
    }
  } catch {
    return true;
  }
  try {
    return window.sessionStorage.getItem(pendingSignOutStorageKey) !== null;
  } catch {
    return true;
  }
}

export function markPendingSignOut(): void {
  try {
    const url = new URL(window.location.href);
    url.searchParams.set(pendingSignOutParameter, "1");
    window.history.replaceState(window.history.state, "", url);
    return;
  } catch (urlError) {
    try {
      window.sessionStorage.setItem(pendingSignOutStorageKey, "1");
      return;
    } catch (storageError) {
      throw new AggregateError(
        [urlError, storageError],
        "sign-out privacy latch is unavailable",
        { cause: storageError },
      );
    }
  }
}

export function deferInvitations(userID: string): void {
  window.sessionStorage.setItem(deferredInvitationsStorageKey, userID);
  if (window.sessionStorage.getItem(deferredInvitationsStorageKey) !== userID) {
    clearDeferredInvitations();
    throw new Error("invitation deferral could not be confirmed");
  }
}

export function invitationsDeferredFor(userID: string): boolean {
  try {
    const deferredUserID = window.sessionStorage.getItem(
      deferredInvitationsStorageKey,
    );
    if (deferredUserID === null) return false;
    if (!uuidPattern.test(deferredUserID)) {
      window.sessionStorage.removeItem(deferredInvitationsStorageKey);
      return false;
    }
    return deferredUserID === userID;
  } catch {
    return false;
  }
}

export function clearDeferredInvitations(): void {
  try {
    window.sessionStorage.removeItem(deferredInvitationsStorageKey);
  } catch {
    // A retained marker cannot grant authority and explicit invitation routes ignore it.
  }
}

export async function finishBrowserSignOut(): Promise<void> {
  try {
    await closeBrowserSession();
  } catch (caught) {
    // The backend expires the exact cookie on committed revocation. A 401 is
    // therefore sufficient read evidence when that response was lost.
    try {
      if ((await currentUser()) === null) return;
    } catch {
      // Keep the durable latch while revocation remains unknown.
    }
    throw caught;
  }
}

export function clearPendingSignOut(): void {
  try {
    const url = new URL(window.location.href);
    url.searchParams.delete(pendingSignOutParameter);
    window.history.replaceState(window.history.state, "", url);
  } catch {
    // Retaining the URL latch after cookie revocation is fail-closed on reload.
  }
  try {
    window.sessionStorage.removeItem(pendingSignOutStorageKey);
  } catch {
    // Retaining the fallback latch is also fail-closed.
  }
}
