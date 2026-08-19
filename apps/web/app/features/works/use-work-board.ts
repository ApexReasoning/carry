import { useEffect, useRef, useState } from "react";

import {
  appendWorkMessage,
  createWork,
  listWorks,
  loadWork,
  type WorkDetails,
} from "../../carry-api";
import type { Member, Work } from "../../generated/types.gen";

export function useWorkBoard(member: Member | null) {
  const [spaceID, setSpaceID] = useState<string | null>(null);
  const [works, setWorks] = useState<Array<Work>>([]);
  const [details, setDetails] = useState<WorkDetails | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pendingCreate = useRef<{
    spaceID: string;
    goal: string;
    idempotencyKey: string;
  } | null>(null);
  const pendingMessage = useRef<{
    spaceID: string;
    workID: string;
    text: string;
    idempotencyKey: string;
  } | null>(null);

  useEffect(() => {
    let active = true;
    async function loadInitialSpace() {
      const initialSpaceID = member?.spaces[0]?.space_id ?? null;
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
    if (!spaceID) return false;
    const previous = pendingCreate.current;
    const command =
      previous?.spaceID === spaceID && previous.goal === goal
        ? previous
        : { spaceID, goal, idempotencyKey: crypto.randomUUID() };
    pendingCreate.current = command;

    const failure = await run(async () => {
      const created = await createWork(
        command.spaceID,
        command.goal,
        command.idempotencyKey,
      );
      setWorks((current) => [
        created,
        ...current.filter((item) => item.work_id !== created.work_id),
      ]);
      setDetails({ work: created, messages: [] });
    });
    if (failure) return false;
    pendingCreate.current = null;
    return true;
  }

  async function addMessage(text: string): Promise<boolean> {
    if (!spaceID || !details) return false;
    const workID = details.work.work_id;
    const previous = pendingMessage.current;
    const command =
      previous?.spaceID === spaceID &&
      previous.workID === workID &&
      previous.text === text
        ? previous
        : {
            spaceID,
            workID,
            text,
            idempotencyKey: crypto.randomUUID(),
          };
    pendingMessage.current = command;

    const failure = await run(async () => {
      await appendWorkMessage(
        command.spaceID,
        command.workID,
        command.text,
        command.idempotencyKey,
      );
      const reloaded = await loadWork(command.spaceID, command.workID);
      setDetails(reloaded);
      setWorks((current) =>
        current.map((item) =>
          item.work_id === reloaded.work.work_id ? reloaded.work : item,
        ),
      );
    });
    if (failure) return false;
    pendingMessage.current = null;
    return true;
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
  };
}

function errorMessage(value: unknown): string {
  return value instanceof Error
    ? value.message
    : "Carry could not complete the request";
}
