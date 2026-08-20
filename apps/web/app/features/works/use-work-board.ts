import { useEffect, useState } from "react";

import {
  appendWorkMessage,
  createWork,
  listWorks,
  loadWork,
  retryWork,
  type WorkDetails,
} from "../../carry-api";
import type { Member, WorkSummary } from "../../generated/types.gen";
import {
  emptyWorkPage,
  mergeByID,
  mergeDetails,
  summaryFromWork,
  upsertSummary,
} from "./work-page";
import {
  clearPendingIdentity,
  pendingCreateIdentity,
  pendingMessageIdentity,
  pendingRetryIdentity,
} from "./work-pending";

export function useWorkBoard(member: Member | null) {
  const [spaceID, setSpaceID] = useState<string | null>(null);
  const [works, setWorks] = useState<Array<WorkSummary>>([]);
  const [hasEarlierWorks, setHasEarlierWorks] = useState(false);
  const [details, setDetails] = useState<WorkDetails | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    async function loadInitialSpace() {
      const initialSpaceID =
        member?.spaces.length === 1
          ? (member.spaces[0]?.space_id ?? null)
          : null;
      try {
        const page = initialSpaceID
          ? await listWorks(initialSpaceID)
          : emptyWorkPage();
        if (!active) return;
        setSpaceID(initialSpaceID);
        setWorks(page.works);
        setHasEarlierWorks(page.has_earlier_works);
        setDetails(null);
        setError(null);
      } catch (caught) {
        if (active) setError(errorMessage(caught));
      }
    }
    void loadInitialSpace();
    return () => {
      active = false;
    };
  }, [member]);

  async function selectSpace(selectedSpaceID: string) {
    if (!selectedSpaceID) return;
    await run(async () => {
      const page = await listWorks(selectedSpaceID);
      setSpaceID(selectedSpaceID);
      setWorks(page.works);
      setHasEarlierWorks(page.has_earlier_works);
      setDetails(null);
    });
  }

  async function loadEarlierWorks(): Promise<void> {
    if (!spaceID || !hasEarlierWorks || works.length === 0) return;
    const cursor = works.at(-1)?.work_id;
    if (!cursor) return;
    await run(async () => {
      const page = await listWorks(spaceID, cursor);
      setWorks((current) => mergeByID(current, page.works, "work_id"));
      setHasEarlierWorks(page.has_earlier_works);
    });
  }

  async function selectWork(workID: string) {
    if (!spaceID) return;
    await run(async () => updateDetails(await loadWork(spaceID, workID)));
  }

  async function loadEarlierMessages(): Promise<void> {
    if (!spaceID || !details?.has_earlier_messages) return;
    const cursor = details.messages[0]?.message_id;
    if (!cursor) return;
    const workID = details.work.work_id;
    await run(async () => {
      const earlier = await loadWork(spaceID, workID, cursor);
      setDetails((current) => {
        if (!current || current.work.work_id !== workID) return current;
        return {
          work: earlier.work,
          messages: mergeByID(earlier.messages, current.messages, "message_id"),
          has_earlier_messages: earlier.has_earlier_messages,
        };
      });
      setWorks((current) =>
        upsertSummary(current, summaryFromWork(earlier.work)),
      );
    });
  }

  async function addWork(goal: string): Promise<boolean> {
    if (!spaceID || !member) return false;
    const identity = await pendingCreateIdentity(
      member.user_id,
      spaceID,
      goal,
    ).catch((caught: unknown) => {
      setError(errorMessage(caught));
      return null;
    });
    if (!identity) return false;

    const failure = await run(async () => {
      const created = await createWork(spaceID, goal, identity.idempotencyKey);
      const reloaded = await loadWork(spaceID, created.work_id);
      updateDetails(reloaded);
      clearPendingIdentity(identity);
    });
    return failure === null;
  }

  async function addMessage(text: string): Promise<boolean> {
    if (!spaceID || !details || !member) return false;
    const workID = details.work.work_id;
    const identity = await pendingMessageIdentity(
      member.user_id,
      spaceID,
      workID,
      text,
    ).catch((caught: unknown) => {
      setError(errorMessage(caught));
      return null;
    });
    if (!identity) return false;

    const failure = await run(async () => {
      await appendWorkMessage(spaceID, workID, text, identity.idempotencyKey);
      const reloaded = await loadWork(spaceID, workID);
      updateDetails(reloaded);
      clearPendingIdentity(identity);
    });
    return failure === null;
  }

  async function retryCurrentWork(): Promise<void> {
    if (!spaceID || !details || !member) return;
    const workID = details.work.work_id;
    const identity = await pendingRetryIdentity(
      member.user_id,
      spaceID,
      workID,
    ).catch((caught: unknown) => {
      setError(errorMessage(caught));
      return null;
    });
    if (!identity) return;

    await run(async () => {
      try {
        await retryWork(spaceID, workID, identity.idempotencyKey);
      } catch (caught) {
        const reloaded = await loadWork(spaceID, workID);
        updateDetails(reloaded);
        if (reloaded.work.needs_retry) throw caught;
        clearPendingIdentity(identity);
        return;
      }

      const reloaded = await loadWork(spaceID, workID);
      updateDetails(reloaded);
      clearPendingIdentity(identity);
      if (reloaded.work.needs_retry) {
        throw new Error(
          "The previous retry was reconciled, but this Work needs a new choice. Choose Try again once more.",
        );
      }
    });
  }

  function updateDetails(reloaded: WorkDetails) {
    setDetails((current) => mergeDetails(current, reloaded));
    setWorks((current) =>
      upsertSummary(current, summaryFromWork(reloaded.work)),
    );
  }

  async function run(operation: () => Promise<void>): Promise<unknown | null> {
    setBusy(true);
    setError(null);
    try {
      await operation();
      return null;
    } catch (caught) {
      setError(errorMessage(caught));
      return caught;
    } finally {
      setBusy(false);
    }
  }

  return {
    spaceID,
    works,
    hasEarlierWorks,
    details,
    busy,
    error,
    selectSpace,
    loadEarlierWorks,
    selectWork,
    loadEarlierMessages,
    addWork,
    addMessage,
    retryCurrentWork,
  };
}

function errorMessage(value: unknown): string {
  return value instanceof Error
    ? value.message
    : "Carry could not complete the request";
}
