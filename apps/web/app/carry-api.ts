import { client } from "./generated/client.gen";
import {
  acceptWorkReview as acceptWorkReviewRequest,
  appendWorkMessage as appendWorkMessageRequest,
  createFirstSpace as createFirstSpaceRequest,
  createWork as createWorkRequest,
  listConversationMessages as listConversationMessagesRequest,
  listWorks as listWorksRequest,
  loadCurrentUser,
  loadIdentityMethods as loadIdentityMethodsRequest,
  loadWork as loadWorkRequest,
  requestEmailCode as requestEmailCodeRequest,
  requestEmailLinkCode as requestEmailLinkCodeRequest,
  requestEmailReauthenticationCode as requestEmailReauthenticationCodeRequest,
  retryWork as retryWorkRequest,
  revokeCurrentBrowserSession,
  sendConversationMessage as sendConversationMessageRequest,
  unlinkIdentityMethod as unlinkIdentityMethodRequest,
  verifyEmailCode as verifyEmailCodeRequest,
  verifyEmailLinkCode as verifyEmailLinkCodeRequest,
  verifyEmailReauthenticationCode as verifyEmailReauthenticationCodeRequest,
} from "./generated/sdk.gen";
import type {
  ConversationMessage,
  EmailChallenge,
  IdentityMethods,
  Membership,
  User,
  Work,
  WorkMessage,
  WorkSummary,
} from "./generated/types.gen";

export class MutationOutcomeUnknownError extends Error {}

export class APIResponseError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

export type WorkPage = {
  works: Array<WorkSummary>;
  has_earlier_works: boolean;
};

export type WorkDetails = {
  work: Work;
  messages: Array<WorkMessage>;
  has_earlier_messages: boolean;
};

client.setConfig({
  baseUrl: window.location.origin,
  credentials: "same-origin",
});

const sameOrigin = { credentials: "same-origin" as const };

export async function requestEmailCode(
  email: string,
  challengeID: string,
  idempotencyKey: string,
): Promise<EmailChallenge> {
  const result = await requestEmailCodeRequest({
    ...sameOrigin,
    body: { challenge_id: challengeID, email },
    headers: { "Idempotency-Key": idempotencyKey },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Send email code",
  );
}

export async function verifyEmailCode(
  challengeID: string,
  code: string,
  idempotencyKey: string,
): Promise<void> {
  const result = await verifyEmailCodeRequest({
    ...sameOrigin,
    body: { code },
    headers: { "Idempotency-Key": idempotencyKey },
    path: { challengeID },
  });
  requireMutationSuccess(result.response, result.error, "Verify email code");
}

export async function identityMethods(): Promise<IdentityMethods> {
  const result = await loadIdentityMethodsRequest(sameOrigin);
  return requireData(
    result.data,
    result.response,
    result.error,
    "Load sign-in methods",
  );
}

export async function requestIdentityEmailCode(
  purpose: "reauthenticate" | "link",
  challengeID: string,
  idempotencyKey: string,
  email?: string,
): Promise<EmailChallenge> {
  const request =
    purpose === "link"
      ? requestEmailLinkCodeRequest({
          ...sameOrigin,
          body: { challenge_id: challengeID, email: email ?? "" },
          headers: { "Idempotency-Key": idempotencyKey },
        })
      : requestEmailReauthenticationCodeRequest({
          ...sameOrigin,
          body: { challenge_id: challengeID },
          headers: { "Idempotency-Key": idempotencyKey },
        });
  const result = await request;
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    purpose === "link" ? "Link email" : "Confirm email",
  );
}

export async function verifyIdentityEmailCode(
  purpose: "reauthenticate" | "link",
  challengeID: string,
  code: string,
  idempotencyKey: string,
): Promise<void> {
  const options = {
    ...sameOrigin,
    body: { code },
    headers: { "Idempotency-Key": idempotencyKey },
    path: { challengeID },
  };
  const result =
    purpose === "link"
      ? await verifyEmailLinkCodeRequest(options)
      : await verifyEmailReauthenticationCodeRequest(options);
  requireMutationSuccess(
    result.response,
    result.error,
    purpose === "link" ? "Link email" : "Confirm email",
  );
}

export async function unlinkIdentityMethod(
  method: "email" | "google" | "github",
  idempotencyKey: string,
): Promise<void> {
  const result = await unlinkIdentityMethodRequest({
    ...sameOrigin,
    headers: { "Idempotency-Key": idempotencyKey },
    path: { method },
  });
  requireMutationSuccess(
    result.response,
    result.error,
    "Remove sign-in method",
  );
}

export async function createFirstSpace(
  displayName: string,
  name: string,
  idempotencyKey: string,
): Promise<Membership> {
  const result = await createFirstSpaceRequest({
    ...sameOrigin,
    body: { display_name: displayName, name },
    headers: { "Idempotency-Key": idempotencyKey },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Create first Space",
  );
}

export async function closeBrowserSession(): Promise<void> {
  const result = await revokeCurrentBrowserSession(sameOrigin);
  requireMutationSuccess(
    result.response,
    result.error,
    "Close browser session",
  );
}

export async function currentUser(): Promise<User | null> {
  const result = await loadCurrentUser(sameOrigin);
  if (result.response?.status === 401) {
    return null;
  }
  return requireData(result.data, result.response, result.error, "Load User");
}

export async function listConversationMessages(
  spaceID: string,
  cursor?: { before: string } | { after: string },
): Promise<Array<ConversationMessage>> {
  const result = await listConversationMessagesRequest({
    ...sameOrigin,
    path: { spaceID },
    query: cursor,
  });
  return requireData(
    result.data,
    result.response,
    result.error,
    "Load private messages",
  ).messages;
}

export async function sendConversationMessage(
  spaceID: string,
  text: string,
  idempotencyKey: string,
): Promise<ConversationMessage> {
  const result = await sendConversationMessageRequest({
    ...sameOrigin,
    body: { text },
    headers: { "Idempotency-Key": idempotencyKey },
    path: { spaceID },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Send private message",
  );
}

export async function listWorks(
  spaceID: string,
  before?: string,
  needsYou = false,
): Promise<WorkPage> {
  const result = await listWorksRequest({
    ...sameOrigin,
    path: { spaceID },
    query:
      before || needsYou
        ? { before, needs_you: needsYou || undefined }
        : undefined,
  });
  return requireData(result.data, result.response, result.error, "List Work");
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
  beforeMessage?: string,
): Promise<WorkDetails> {
  const result = await loadWorkRequest({
    ...sameOrigin,
    path: { spaceID, workID },
    query: beforeMessage ? { before: beforeMessage } : undefined,
  });
  return requireData(result.data, result.response, result.error, "Load Work");
}

export async function acceptWorkReview(
  spaceID: string,
  workID: string,
  reviewID: string,
  idempotencyKey: string,
): Promise<void> {
  const result = await acceptWorkReviewRequest({
    ...sameOrigin,
    headers: { "Idempotency-Key": idempotencyKey },
    path: { spaceID, workID, reviewID },
  });
  requireMutationSuccess(result.response, result.error, "Accept Work result");
}

export async function retryWork(
  spaceID: string,
  workID: string,
  idempotencyKey: string,
): Promise<void> {
  const result = await retryWorkRequest({
    ...sameOrigin,
    headers: { "Idempotency-Key": idempotencyKey },
    path: { spaceID, workID },
  });
  requireMutationSuccess(result.response, result.error, "Retry Work");
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
    throw new MutationOutcomeUnknownError(
      `${action} outcome is unknown; retry the same command to reconcile${detail}`,
    );
  }
  return requireData(data, response, error, action);
}

function requireMutationSuccess(
  response: Response | undefined,
  error: unknown,
  action: string,
): void {
  if (!response) {
    const detail = error instanceof Error ? `: ${error.message}` : "";
    throw new MutationOutcomeUnknownError(
      `${action} outcome is unknown${detail}`,
    );
  }
  requireSuccess(response, error, action);
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
  if (isAPIError(error) && response) {
    throw new APIResponseError(error.error, response.status);
  }
  if (error instanceof Error) {
    throw new Error(`${action} failed: ${error.message}`);
  }
  if (response) {
    throw new APIResponseError(
      `${action} failed (${response.status})`,
      response.status,
    );
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
