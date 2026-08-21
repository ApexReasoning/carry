import { useEffect, useState } from "react";

import {
  currentUser,
  invitationInbox,
  MutationOutcomeUnknownError,
} from "../../carry-api";
import type { InvitationInbox, User } from "../../generated/types.gen";
import {
  clearPendingSignOut,
  finishBrowserSignOut,
  hasPendingSignOut,
  markPendingSignOut,
} from "./browser-session";
import {
  type EmailChallengeCommand,
  newEmailChallenge,
  requestExactEmailChallenge,
} from "./email-challenge";
import {
  newEmailVerification,
  verifyExactEmailChallenge,
} from "./email-verification";

type SessionPhase =
  | "checking"
  | "email"
  | "code"
  | "invitations"
  | "ready"
  | "failed"
  | "signing-out";

export function useUserSession() {
  const [phase, setPhase] = useState<SessionPhase>("checking");
  const [user, setUser] = useState<User | null>(null);
  const [inbox, setInbox] = useState<InvitationInbox | null>(null);
  const [challenge, setChallenge] = useState<EmailChallengeCommand | null>(
    null,
  );
  const [requestRetryPending, setRequestRetryPending] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    async function restore() {
      setError(takeExternalSignInStatus());
      if (hasPendingSignOut()) {
        setUser(null);
        setPhase("signing-out");
        await finishSignOut(() => active);
        return;
      }
      setPhase("checking");
      try {
        const loaded = await currentUser();
        if (active) await routeUser(loaded);
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
  }, []);

  async function routeUser(loaded: User | null) {
    setUser(loaded);
    setInbox(null);
    if (!loaded) {
      setPhase("email");
      return;
    }
    const invitations = await invitationInbox();
    if (
      invitations.invitations.length > 0 ||
      window.location.pathname === "/invitations"
    ) {
      setInbox(invitations);
      setPhase("invitations");
      return;
    }
    setPhase("ready");
  }

  async function refresh() {
    try {
      const loaded = await currentUser();
      await routeUser(loaded);
    } catch (caught) {
      setError(errorMessage(caught));
    }
  }

  async function retryRestore() {
    setPhase("checking");
    setError(null);
    try {
      const loaded = await currentUser();
      await routeUser(loaded);
    } catch (caught) {
      setError(errorMessage(caught));
      setPhase("failed");
    }
  }

  async function sendCode(address: string) {
    const command = newEmailChallenge(address);
    setChallenge(command);
    setRequestRetryPending(false);
    await submitChallenge(command, false);
  }

  async function retryCodeRequest() {
    if (!challenge) return;
    await submitChallenge(challenge, true);
  }

  async function resendCode() {
    if (!challenge) return;
    const command = newEmailChallenge(challenge.email);
    setChallenge(command);
    setRequestRetryPending(false);
    await submitChallenge(command, false);
  }

  async function submitChallenge(
    command: EmailChallengeCommand,
    isExactRetry: boolean,
  ) {
    setBusy(true);
    setError(null);
    try {
      await requestExactEmailChallenge(command);
      setRequestRetryPending(false);
      setPhase("code");
    } catch (caught) {
      if (caught instanceof MutationOutcomeUnknownError) {
        setRequestRetryPending(true);
        setPhase("code");
        setError(
          isExactRetry
            ? "Carry still cannot confirm whether this exact code request was submitted."
            : "Carry may have sent the code. Retry this exact request, enter it if it arrives, or send a new code.",
        );
      } else {
        setRequestRetryPending(false);
        setError(errorMessage(caught));
        setPhase(isExactRetry ? "code" : "email");
      }
    } finally {
      setBusy(false);
    }
  }

  async function verifyCode(code: string) {
    if (!challenge) return;
    setBusy(true);
    setError(null);
    try {
      const loaded = await verifyExactEmailChallenge(
        newEmailVerification(challenge.challengeID, code),
      );
      await routeUser(loaded);
    } catch (caught) {
      setError(errorMessage(caught));
      setPhase("code");
    } finally {
      setBusy(false);
    }
  }

  async function signOut() {
    setUser(null);
    setPhase("signing-out");
    setError(null);
    try {
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
      await finishBrowserSignOut();
      if (!isActive()) return;
      clearPendingSignOut();
      setError(null);
      setPhase("email");
    } catch (caught) {
      if (!isActive()) return;
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
    user,
    inbox,
    email: challenge?.email ?? "",
    challengeID: challenge?.challengeID ?? "",
    canRetryCodeRequest: requestRetryPending,
    busy,
    error,
    retry: () => void retryRestore(),
    refresh: () => void refresh(),
    skipInvitations: () => {
      window.history.replaceState(null, "", "/");
      setPhase("ready");
    },
    sendCode,
    retryCodeRequest,
    verifyCode,
    resendCode,
    backToEmail: () => {
      setError(null);
      setChallenge(null);
      setRequestRetryPending(false);
      setPhase("email");
    },
    signOut,
    finishSignOut: () => finishSignOut(),
  };
}

function takeExternalSignInStatus(): string | null {
  const parameters = new URLSearchParams(window.location.search);
  const status = parameters.get("sign_in");
  if (status === null) return null;
  parameters.delete("sign_in");
  const query = parameters.toString();
  window.history.replaceState(
    null,
    "",
    `${window.location.pathname}${query ? `?${query}` : ""}${window.location.hash}`,
  );
  switch (status) {
    case "cancelled":
      return "Sign-in was cancelled. Start again when you’re ready.";
    case "unavailable":
      return "Carry could not confirm sign-in. Start a fresh sign-in.";
    case "invalid":
      return "This sign-in link is invalid or expired. Start again.";
    default:
      return "Carry could not confirm sign-in. Start again.";
  }
}

function errorMessage(value: unknown): string {
  return value instanceof Error
    ? value.message
    : "Carry could not complete the request";
}
