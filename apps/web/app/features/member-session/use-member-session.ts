import { useEffect, useState } from "react";

import {
  closeBrowserSession,
  currentMember,
  establishBrowserSession,
  MutationOutcomeUnknownError,
} from "../../carry-api";
import type { Member } from "../../generated/types.gen";

const pendingSignOutParameter = "carry-signing-out";
const pendingSignOutStorageKey = "carry.pending-sign-out.v1";

type SessionPhase = "checking" | "login" | "ready" | "failed" | "signing-out";

export function useMemberSession() {
  const [phase, setPhase] = useState<SessionPhase>("checking");
  const [member, setMember] = useState<Member | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [restoreAttempt, setRestoreAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    async function restore() {
      setError(null);
      if (hasPendingSignOut()) {
        setMember(null);
        setPhase("signing-out");
        await finishSignOut(() => active);
        return;
      }
      setPhase("checking");
      try {
        const loaded = await currentMember();
        if (!active) return;
        setMember(loaded);
        setPhase(loaded ? "ready" : "login");
      } catch (caught) {
        if (!active) return;
        setError(errorMessage(caught));
        setPhase("failed");
      }
    }
    void restore();
    return () => {
      active = false;
    };
  }, [restoreAttempt]);

  async function openWithToken(token: string) {
    setBusy(true);
    setError(null);
    try {
      await establishBrowserSession(token);
      const loaded = await currentMember();
      if (!loaded) throw new Error("Carry did not establish a browser session");
      setMember(loaded);
      setPhase("ready");
    } catch (caught) {
      if (caught instanceof MutationOutcomeUnknownError) {
        try {
          const reconciled = await currentMember();
          if (reconciled) {
            setMember(reconciled);
            setPhase("ready");
            return;
          }
        } catch {
          // The original mutation remains Unknown when reconciliation is unavailable.
        }
      }
      setError(errorMessage(caught));
      setPhase("login");
    } finally {
      setBusy(false);
    }
  }

  async function signOut() {
    setMember(null);
    setPhase("signing-out");
    setError(null);
    try {
      // The URL latch survives reload without depending on quota-limited Web
      // Storage. It carries no credential or Work content and fails closed.
      markPendingSignOut();
    } catch (caught) {
      setError(
        `Carry could not establish a reload-safe sign-out latch. Work remains hidden while revocation continues. ${errorMessage(caught)}`,
      );
    }
    await finishSignOut();
  }

  async function finishSignOut(isActive: () => boolean = () => true) {
    setBusy(true);
    try {
      await closeBrowserSession();
      if (!isActive()) return;
      clearPendingSignOut();
      setError(null);
      setPhase("login");
    } catch (caught) {
      if (!isActive()) return;
      try {
        if ((await currentMember()) === null) {
          clearPendingSignOut();
          setError(null);
          setPhase("login");
          return;
        }
      } catch {
        // Keep the URL latch and never restore Work while revocation is unknown.
      }
      setError(
        `Sign-out revocation is not confirmed. Finish signing out before reopening Work. ${errorMessage(caught)}`,
      );
      setPhase("signing-out");
    } finally {
      if (isActive()) setBusy(false);
    }
  }

  return {
    phase,
    member,
    busy,
    error,
    retry: () => setRestoreAttempt((attempt) => attempt + 1),
    openWithToken,
    signOut,
    finishSignOut: () => finishSignOut(),
  };
}

function hasPendingSignOut(): boolean {
  try {
    if (
      new URL(window.location.href).searchParams.has(pendingSignOutParameter)
    ) {
      return true;
    }
  } catch {
    // An unreadable URL latch must never reopen Work under an existing cookie.
    return true;
  }
  try {
    return window.sessionStorage.getItem(pendingSignOutStorageKey) !== null;
  } catch {
    // An unreadable fallback latch is also fail-closed.
    return true;
  }
}

function markPendingSignOut(): void {
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

function clearPendingSignOut(): void {
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

function errorMessage(value: unknown): string {
  return value instanceof Error
    ? value.message
    : "Carry could not complete the request";
}
