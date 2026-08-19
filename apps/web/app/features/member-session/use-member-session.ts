import { useEffect, useState } from "react";

import {
  closeBrowserSession,
  currentMember,
  establishBrowserSession,
} from "../../carry-api";
import type { Member } from "../../generated/types.gen";

type SessionPhase = "checking" | "login" | "ready" | "failed";

export function useMemberSession() {
  const [phase, setPhase] = useState<SessionPhase>("checking");
  const [member, setMember] = useState<Member | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [restoreAttempt, setRestoreAttempt] = useState(0);

  useEffect(() => {
    let active = true;
    async function restore() {
      setPhase("checking");
      setError(null);
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
      setError(errorMessage(caught));
      setPhase("login");
    } finally {
      setBusy(false);
    }
  }

  async function signOut() {
    setBusy(true);
    setError(null);
    try {
      await closeBrowserSession();
      setMember(null);
      setPhase("login");
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBusy(false);
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
  };
}

function errorMessage(value: unknown): string {
  return value instanceof Error
    ? value.message
    : "Carry could not complete the request";
}
