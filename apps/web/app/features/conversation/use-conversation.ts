import { useEffect, useState } from "react";

import {
  listConversationMessages,
  MutationOutcomeUnknownError,
  sendConversationMessage,
} from "../../carry-api";
import type { ConversationMessage } from "../../generated/types.gen";
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
    void listConversationMessages(spaceID)
      .then((loaded) => {
        if (!active) return;
        setMessages(loaded);
        setCanLoadEarlier(loaded.length === pageSize);
        try {
          clearPendingIfAdmitted(memberID, spaceID, loaded);
        } catch (caught) {
          setIdentityBlocked(true);
          setError(errorMessage(caught));
        }
      })
      .catch((caught: unknown) => {
        if (active) setError(errorMessage(caught));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [memberID, spaceID]);

  const newestMessage = messages.at(-1);
  const awaitingReply = newestMessage?.author === "member";
  const newestMessageID = awaitingReply ? newestMessage.message_id : null;

  useEffect(() => {
    if (!newestMessageID) return;
    let active = true;
    let polling = false;
    const poll = window.setInterval(() => {
      if (polling) return;
      polling = true;
      void listConversationMessages(spaceID, { after: newestMessageID })
        .then((loaded) => {
          if (!active || loaded.length === 0) return;
          setMessages((current) => appendUnique(current, loaded));
          setError(null);
        })
        .catch((caught: unknown) => {
          if (active) setError(errorMessage(caught));
        })
        .finally(() => {
          polling = false;
        });
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
      setMessages((current) => appendUnique(current, [admitted]));
      clearAdmittedIdentity(memberID, spaceID, requestID);
      return true;
    } catch (caught) {
      if (caught instanceof MutationOutcomeUnknownError) {
        try {
          const newest = await listConversationMessages(spaceID);
          const admitted = newest.some(
            (message) => message.request_id === requestID,
          );
          setMessages((current) => appendUnique(current, newest));
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
    if (loadingEarlier || messages.length === 0) return;
    setLoadingEarlier(true);
    setError(null);
    try {
      const earlier = await listConversationMessages(spaceID, {
        before: messages[0]!.message_id,
      });
      setMessages((current) => prependUnique(earlier, current));
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

function appendUnique(
  current: Array<ConversationMessage>,
  incoming: Array<ConversationMessage>,
): Array<ConversationMessage> {
  const known = new Set(current.map((message) => message.message_id));
  return [
    ...current,
    ...incoming.filter((message) => !known.has(message.message_id)),
  ];
}

function prependUnique(
  earlier: Array<ConversationMessage>,
  current: Array<ConversationMessage>,
): Array<ConversationMessage> {
  const known = new Set(current.map((message) => message.message_id));
  return [
    ...earlier.filter((message) => !known.has(message.message_id)),
    ...current,
  ];
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
