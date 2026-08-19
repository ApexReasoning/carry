import { useEffect, useState } from "react";

import {
  appendWorkMessage,
  createWork,
  listWorks,
  loadWork,
  retryWork,
  type WorkDetails,
} from "../../carry-api";
import type { Member, Work } from "../../generated/types.gen";
import {
  clearPendingIdentity,
  pendingCreateIdentity,
  pendingMessageIdentity,
  pendingRetryIdentity,
} from "./work-pending";

export function useWorkBoard(member: Member | null) {
  const [spaceID, setSpaceID] = useState<string | null>(null);
  const [works, setWorks] = useState<Array<Work>>([]);
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
        const loadedWorks = initialSpaceID
          ? await listWorks(initialSpaceID)
          : [];
        if (!active) return;
        setSpaceID(initialSpaceID);
        setWorks(loadedWorks);
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
      setWorks(await listWorks(selectedSpaceID));
      setSpaceID(selectedSpaceID);
      setDetails(null);
    });
  }

  async function selectWork(workID: string) {
    if (!spaceID) return;
    await run(async () => setDetails(await loadWork(spaceID, workID)));
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
    setDetails(reloaded);
    setWorks((current) => [
      reloaded.work,
      ...current.filter((item) => item.work_id !== reloaded.work.work_id),
    ]);
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
    details,
    busy,
    error,
    selectSpace,
    selectWork,
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
