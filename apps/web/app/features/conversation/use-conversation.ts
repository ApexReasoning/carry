import { useEffect, useState } from "react";

import {
  APIResponseError,
  listConversationMessages,
  MutationOutcomeUnknownError,
  sendConversationMessage,
} from "../../carry-api";
import type { ConversationMessage } from "../../generated/types.gen";
import {
  appendUniqueMessages,
  prependUniqueMessages,
} from "./conversation-page";
import {
  clearPendingConversationRequestID,
  loadPendingConversationRequestID,
  pendingConversationRequestID,
} from "./conversation-pending";

const pageSize = 50;
const pollIntervalMilliseconds = 1_000;

export function useConversation(memberID: string, spaceID: string) {
  const [messages, setMessages] = useState<Array<ConversationMessage>>([]);
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const [loadingEarlier, setLoadingEarlier] = useState(false);
  const [canLoadEarlier, setCanLoadEarlier] = useState(false);
  const [identityBlocked, setIdentityBlocked] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    async function loadInitialPage(): Promise<void> {
      try {
        const loaded = await listConversationMessages(spaceID);
        if (!active) return;
        setMessages(loaded);
        setCanLoadEarlier(loaded.length === pageSize);
        try {
          clearPendingIfAdmitted(memberID, spaceID, loaded);
        } catch (caught) {
          setIdentityBlocked(true);
          setError(errorMessage(caught));
        }
      } catch (caught) {
        if (active) setError(errorMessage(caught));
      } finally {
        if (active) setLoading(false);
      }
    }
    void loadInitialPage();
    return () => {
      active = false;
    };
  }, [memberID, spaceID]);

  const newestMessage = messages.at(-1);
  const awaitingReply = newestMessage?.author === "member";
  const newestMessageID = awaitingReply ? newestMessage.message_id : null;

  useEffect(() => {
    const replyCursor = newestMessageID;
    if (!replyCursor) return;
    let active = true;
    let polling = false;
    async function loadNewReplies(after: string): Promise<void> {
      if (polling) return;
      polling = true;
      try {
        const loaded = await listConversationMessages(spaceID, { after });
        if (!active || loaded.length === 0) return;
        setMessages((current) => appendUniqueMessages(current, loaded));
        setError(null);
      } catch (caught) {
        if (active) setError(errorMessage(caught));
      } finally {
        polling = false;
      }
    }
    const poll = window.setInterval(() => {
      void loadNewReplies(replyCursor);
    }, pollIntervalMilliseconds);
    return () => {
      active = false;
      window.clearInterval(poll);
    };
  }, [memberID, newestMessageID, spaceID]);

  async function send(text: string): Promise<boolean> {
    if (awaitingReply || sending || identityBlocked) {
      return false;
    }
    setSending(true);
    setError(null);
    let requestID: string;
    try {
      requestID = pendingConversationRequestID(memberID, spaceID);
    } catch (caught) {
      setIdentityBlocked(true);
      setError(errorMessage(caught));
      setSending(false);
      return false;
    }

    try {
      const admitted = await sendConversationMessage(spaceID, text, requestID);
      setMessages((current) => appendUniqueMessages(current, [admitted]));
      clearAdmittedIdentity(memberID, spaceID, requestID);
      return true;
    } catch (caught) {
      if (caught instanceof APIResponseError && caught.status === 409) {
        try {
          clearPendingConversationRequestID(memberID, spaceID, requestID);
          const newest = await listConversationMessages(spaceID);
          setMessages((current) => appendUniqueMessages(current, newest));
          setError(
            "This private conversation changed in another browser. Review the latest messages, then send again.",
          );
        } catch (reconciliationError) {
          setIdentityBlocked(true);
          setError(
            `Carry could not replace the stale private request identity: ${errorMessage(reconciliationError)}`,
          );
        }
        return false;
      }
      if (caught instanceof MutationOutcomeUnknownError) {
        try {
          const newest = await listConversationMessages(spaceID);
          const admitted = newest.some(
            (message) => message.request_id === requestID,
          );
          setMessages((current) => appendUniqueMessages(current, newest));
          if (admitted) {
            clearAdmittedIdentity(memberID, spaceID, requestID);
            return true;
          }
        } catch (reconciliationError) {
          setError(
            `${errorMessage(caught)} Reconciliation failed: ${errorMessage(reconciliationError)}`,
          );
          return false;
        }
      }
      setError(errorMessage(caught));
      return false;
    } finally {
      setSending(false);
    }
  }

  async function loadEarlier(): Promise<void> {
    const firstMessage = messages[0];
    if (loadingEarlier || !firstMessage) return;
    setLoadingEarlier(true);
    setError(null);
    try {
      const earlier = await listConversationMessages(spaceID, {
        before: firstMessage.message_id,
      });
      setMessages((current) => prependUniqueMessages(earlier, current));
      setCanLoadEarlier(earlier.length === pageSize);
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setLoadingEarlier(false);
    }
  }

  function clearAdmittedIdentity(
    currentMemberID: string,
    currentSpaceID: string,
    requestID: string,
  ): void {
    try {
      clearPendingConversationRequestID(
        currentMemberID,
        currentSpaceID,
        requestID,
      );
    } catch (caught) {
      setIdentityBlocked(true);
      setError(errorMessage(caught));
    }
  }

  return {
    messages,
    loading,
    sending,
    loadingEarlier,
    canLoadEarlier,
    awaitingReply,
    identityBlocked,
    error,
    send,
    loadEarlier,
  };
}

function clearPendingIfAdmitted(
  memberID: string,
  spaceID: string,
  messages: Array<ConversationMessage>,
): void {
  const requestID = loadPendingConversationRequestID(memberID, spaceID);
  if (
    requestID &&
    messages.some((message) => message.request_id === requestID)
  ) {
    clearPendingConversationRequestID(memberID, spaceID, requestID);
  }
}

function errorMessage(value: unknown): string {
  if (value instanceof Error) return value.message;
  if (
    typeof value === "object" &&
    value !== null &&
    "message" in value &&
    typeof value.message === "string"
  ) {
    return value.message;
  }
  return "Carry could not complete the private request";
}
