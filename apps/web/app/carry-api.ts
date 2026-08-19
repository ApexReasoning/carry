import { client } from "./generated/client.gen";
import {
  appendWorkMessage as appendWorkMessageRequest,
  createBrowserSession,
  createWork as createWorkRequest,
  listWorks as listWorksRequest,
  loadCurrentMember,
  loadWork as loadWorkRequest,
  revokeCurrentBrowserSession,
} from "./generated/sdk.gen";
import type { Member, Work, WorkMessage } from "./generated/types.gen";

export type WorkDetails = {
  work: Work;
  messages: Array<WorkMessage>;
};

client.setConfig({
  baseUrl: window.location.origin,
  credentials: "same-origin",
});

const sameOrigin = { credentials: "same-origin" as const };

export async function establishBrowserSession(token: string): Promise<void> {
  const result = await createBrowserSession({
    ...sameOrigin,
    auth: token,
  });
  requireSuccess(result.response, result.error, "Open Carry");
}

export async function closeBrowserSession(): Promise<void> {
  const result = await revokeCurrentBrowserSession(sameOrigin);
  requireSuccess(result.response, result.error, "Close browser session");
}

export async function currentMember(): Promise<Member | null> {
  const result = await loadCurrentMember(sameOrigin);
  if (result.response?.status === 401) {
    return null;
  }
  return requireData(result.data, result.response, result.error, "Load member");
}

export async function listWorks(spaceID: string): Promise<Array<Work>> {
  const result = await listWorksRequest({
    ...sameOrigin,
    path: { spaceID },
  });
  return requireData(result.data, result.response, result.error, "List Work")
    .works;
}

export async function createWork(
  spaceID: string,
  goal: string,
  idempotencyKey: string,
): Promise<Work> {
  const result = await createWorkRequest({
    ...sameOrigin,
    body: { goal },
    headers: { "Idempotency-Key": idempotencyKey },
    path: { spaceID },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Create Work",
  );
}

export async function loadWork(
  spaceID: string,
  workID: string,
): Promise<WorkDetails> {
  const result = await loadWorkRequest({
    ...sameOrigin,
    path: { spaceID, workID },
  });
  return requireData(result.data, result.response, result.error, "Load Work");
}

export async function appendWorkMessage(
  spaceID: string,
  workID: string,
  text: string,
  idempotencyKey: string,
): Promise<WorkMessage> {
  const result = await appendWorkMessageRequest({
    ...sameOrigin,
    body: { text },
    headers: { "Idempotency-Key": idempotencyKey },
    path: { spaceID, workID },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Add Work message",
  );
}

function requireMutationData<T>(
  data: T | undefined,
  response: Response | undefined,
  error: unknown,
  action: string,
): T {
  if (!response && error) {
    const detail = error instanceof Error ? `: ${error.message}` : "";
    throw new Error(
      `${action} outcome is unknown; retry the same command to reconcile${detail}`,
    );
  }
  return requireData(data, response, error, action);
}

function requireData<T>(
  data: T | undefined,
  response: Response | undefined,
  error: unknown,
  action: string,
): T {
  requireSuccess(response, error, action);
  if (data === undefined) {
    throw new Error(`${action} returned no data`);
  }
  return data;
}

function requireSuccess(
  response: Response | undefined,
  error: unknown,
  action: string,
): void {
  if (response?.ok) {
    return;
  }
  if (isAPIError(error)) {
    throw new Error(error.error);
  }
  if (error instanceof Error) {
    throw new Error(`${action} failed: ${error.message}`);
  }
  if (response) {
    throw new Error(`${action} failed (${response.status})`);
  }
  throw new Error(`${action} failed before receiving a response`);
}

function isAPIError(value: unknown): value is { error: string } {
  return (
    typeof value === "object" &&
    value !== null &&
    "error" in value &&
    typeof value.error === "string"
  );
}
