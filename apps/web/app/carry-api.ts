import { client } from "./generated/client.gen";
import {
  acceptWorkReview as acceptWorkReviewRequest,
  appendWorkMessage as appendWorkMessageRequest,
  acceptSpaceInvitation as acceptSpaceInvitationRequest,
  approveCliLogin as approveCliLoginRequest,
  approveMachineConnection as approveMachineConnectionRequest,
  createSpace as createSpaceRequest,
  createWork as createWorkRequest,
  denyCliLogin as denyCliLoginRequest,
  denyMachineConnection as denyMachineConnectionRequest,
  issueSpaceInvitation as issueSpaceInvitationRequest,
  listCliCredentials as listCliCredentialsRequest,
  listConversationMessages as listConversationMessagesRequest,
  listInvitationInbox as listInvitationInboxRequest,
  loadInvitation as loadInvitationRequest,
  listManagedInvitations as listManagedInvitationsRequest,
  listMachines as listMachinesRequest,
  listSpaceMembers as listSpaceMembersRequest,
  listWorks as listWorksRequest,
  loadCurrentUser,
  lookupCliLogin as lookupCliLoginRequest,
  lookupMachineConnection as lookupMachineConnectionRequest,
  loadIdentityMethods as loadIdentityMethodsRequest,
  loadWork as loadWorkRequest,
  requestEmailCode as requestEmailCodeRequest,
  requestEmailLinkCode as requestEmailLinkCodeRequest,
  requestEmailReauthenticationCode as requestEmailReauthenticationCodeRequest,
  removeSpaceMember as removeSpaceMemberRequest,
  resendSpaceInvitation as resendSpaceInvitationRequest,
  retryWork as retryWorkRequest,
  revokeCliCredential as revokeCliCredentialRequest,
  revokeMachine as revokeMachineRequest,
  revokeSpaceInvitation as revokeSpaceInvitationRequest,
  revokeCurrentBrowserSession,
  sendConversationMessage as sendConversationMessageRequest,
  unlinkIdentityMethod as unlinkIdentityMethodRequest,
  verifyEmailCode as verifyEmailCodeRequest,
  verifyEmailLinkCode as verifyEmailLinkCodeRequest,
  verifyEmailReauthenticationCode as verifyEmailReauthenticationCodeRequest,
} from "./generated/sdk.gen";
import type {
  AcceptedInvitation,
  CliCredential,
  CliLoginPreview,
  ConversationMessage,
  EmailChallenge,
  IdentityMethods,
  InvitationInbox,
  ManagedInvitation,
  MachineConnectionPreview,
  MachinePage,
  MachineRecord,
  Membership,
  SpaceCreationConflict,
  SpaceMember,
  TargetedInvitation,
  User,
  Work,
  WorkMessage,
  WorkSummary,
} from "./generated/types.gen";

export class MutationOutcomeUnknownError extends Error {}

export class SpaceSlugConflictError extends Error {
  constructor(
    readonly slug: string,
    readonly suggestedSlug: string | undefined,
    readonly suggestedSuffix: number | undefined,
  ) {
    super("Space URL is already in use");
  }
}

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

export async function lookupMachineConnection(
  userCode: string,
): Promise<MachineConnectionPreview> {
  const result = await lookupMachineConnectionRequest({
    ...sameOrigin,
    body: { user_code: userCode },
  });
  return requireData(
    result.data,
    result.response,
    result.error,
    "Machine connection lookup",
  );
}

export async function approveMachineConnection(
  preview: MachineConnectionPreview,
  spaceID: string,
  key: string,
): Promise<void> {
  const result = await approveMachineConnectionRequest({
    ...sameOrigin,
    path: { requestID: preview.request_id },
    headers: { "Idempotency-Key": key },
    body: {
      request_id: preview.request_id,
      user_code: preview.user_code,
      space_id: spaceID,
    },
  });
  requireMutationSuccess(
    result.response,
    result.error,
    "Machine connection approval",
  );
}

export async function denyMachineConnection(
  preview: MachineConnectionPreview,
  key: string,
): Promise<void> {
  const result = await denyMachineConnectionRequest({
    ...sameOrigin,
    path: { requestID: preview.request_id },
    headers: { "Idempotency-Key": key },
    body: { request_id: preview.request_id, user_code: preview.user_code },
  });
  requireMutationSuccess(
    result.response,
    result.error,
    "Machine connection denial",
  );
}

export async function machines(
  spaceID: string,
  after?: string,
): Promise<MachinePage> {
  const result = await listMachinesRequest({
    ...sameOrigin,
    path: { spaceID },
    query: after ? { after } : undefined,
  });
  return requireData(
    result.data,
    result.response,
    result.error,
    "Machine inventory",
  );
}

export async function revokeMachine(
  spaceID: string,
  machineID: string,
  key: string,
): Promise<MachineRecord> {
  const result = await revokeMachineRequest({
    ...sameOrigin,
    path: { spaceID, machineID },
    headers: { "Idempotency-Key": key },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Machine revocation",
  );
}

export async function lookupCliLogin(
  userCode: string,
): Promise<CliLoginPreview> {
  const result = await lookupCliLoginRequest({
    ...sameOrigin,
    body: { user_code: userCode },
  });
  return requireData(
    result.data,
    result.response,
    result.error,
    "Find CLI login",
  );
}

export async function approveCliLogin(
  preview: CliLoginPreview,
  spaceID: string,
  replacementCredentialID: string | undefined,
  key: string,
): Promise<void> {
  const result = await approveCliLoginRequest({
    ...sameOrigin,
    headers: { "Idempotency-Key": key },
    body: {
      request_id: preview.request_id,
      user_code: preview.user_code,
      space_id: spaceID,
      replacement_credential_id: replacementCredentialID,
    },
  });
  requireMutationSuccess(result.response, result.error, "Approve CLI login");
}

export async function denyCliLogin(
  preview: CliLoginPreview,
  key: string,
): Promise<void> {
  const result = await denyCliLoginRequest({
    ...sameOrigin,
    headers: { "Idempotency-Key": key },
    body: { request_id: preview.request_id, user_code: preview.user_code },
  });
  requireMutationSuccess(result.response, result.error, "Deny CLI login");
}

export async function cliCredentials(): Promise<Array<CliCredential>> {
  const result = await listCliCredentialsRequest(sameOrigin);
  return requireData(
    result.data,
    result.response,
    result.error,
    "Load CLI access",
  ).credentials;
}

export async function revokeCliCredential(
  credentialID: string,
  key: string,
): Promise<void> {
  const result = await revokeCliCredentialRequest({
    ...sameOrigin,
    headers: { "Idempotency-Key": key },
    path: { credentialID },
  });
  requireMutationSuccess(result.response, result.error, "Revoke CLI access");
}

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

type IdentityEmailCodeRequest =
  | {
      purpose: "reauthenticate";
      challengeID: string;
      idempotencyKey: string;
    }
  | {
      purpose: "link";
      challengeID: string;
      idempotencyKey: string;
      email: string;
    };

export async function requestIdentityEmailCode(
  command: IdentityEmailCodeRequest,
): Promise<EmailChallenge> {
  const request =
    command.purpose === "link"
      ? requestEmailLinkCodeRequest({
          ...sameOrigin,
          body: { challenge_id: command.challengeID, email: command.email },
          headers: { "Idempotency-Key": command.idempotencyKey },
        })
      : requestEmailReauthenticationCodeRequest({
          ...sameOrigin,
          body: { challenge_id: command.challengeID },
          headers: { "Idempotency-Key": command.idempotencyKey },
        });
  const result = await request;
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    command.purpose === "link" ? "Link email" : "Confirm email",
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

export async function createSpace(
  name: string,
  suffix: number | undefined,
  idempotencyKey: string,
): Promise<Membership> {
  const result = await createSpaceRequest({
    ...sameOrigin,
    body: suffix ? { name, suffix } : { name },
    headers: { "Idempotency-Key": idempotencyKey },
  });
  if (
    result.response?.status === 409 &&
    isSpaceCreationConflict(result.error)
  ) {
    throw new SpaceSlugConflictError(
      result.error.slug ?? "",
      result.error.suggested_slug,
      result.error.suggested_suffix,
    );
  }
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Create Space",
  );
}

export type SpaceMemberPage = {
  members: Array<SpaceMember>;
  next_cursor: string | null;
};

export async function spaceMembers(
  spaceID: string,
  after?: string,
): Promise<SpaceMemberPage> {
  const result = await listSpaceMembersRequest({
    ...sameOrigin,
    path: { spaceID },
    query: after ? { after } : undefined,
  });
  return requireData(
    result.data,
    result.response,
    result.error,
    "Load Space members",
  );
}

export async function removeMember(
  spaceID: string,
  userID: string,
  successorUserID: string | undefined,
  key: string,
): Promise<void> {
  const result = await removeSpaceMemberRequest({
    ...sameOrigin,
    path: { spaceID, userID },
    headers: { "Idempotency-Key": key },
    body: successorUserID
      ? { open_work_new_owner_user_id: successorUserID }
      : {},
  });
  requireMutationSuccess(result.response, result.error, "Remove Space member");
}

export async function managedInvitations(
  spaceID: string,
): Promise<Array<ManagedInvitation>> {
  const result = await listManagedInvitationsRequest({
    ...sameOrigin,
    path: { spaceID },
  });
  return requireData(
    result.data,
    result.response,
    result.error,
    "Load pending invitations",
  ).invitations;
}

export async function issueInvitation(
  spaceID: string,
  email: string,
  canManageMembers: boolean,
  canEnrollMachines: boolean,
  key: string,
): Promise<ManagedInvitation> {
  const result = await issueSpaceInvitationRequest({
    ...sameOrigin,
    path: { spaceID },
    headers: { "Idempotency-Key": key },
    body: {
      email,
      can_manage_members: canManageMembers,
      can_enroll_machines: canEnrollMachines,
    },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Create invitation",
  );
}

export async function resendInvitation(
  spaceID: string,
  invitationID: string,
  key: string,
): Promise<ManagedInvitation> {
  const result = await resendSpaceInvitationRequest({
    ...sameOrigin,
    path: { spaceID, invitationID },
    headers: { "Idempotency-Key": key },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Resend invitation",
  );
}

export async function revokeInvitation(
  spaceID: string,
  invitationID: string,
  key: string,
): Promise<void> {
  const result = await revokeSpaceInvitationRequest({
    ...sameOrigin,
    path: { spaceID, invitationID },
    headers: { "Idempotency-Key": key },
  });
  requireMutationSuccess(result.response, result.error, "Revoke invitation");
}

export async function invitation(
  invitationID: string,
): Promise<TargetedInvitation> {
  const result = await loadInvitationRequest({
    ...sameOrigin,
    path: { invitationID },
  });
  return requireData(
    result.data,
    result.response,
    result.error,
    "Load invitation",
  );
}

export async function invitationInbox(): Promise<InvitationInbox> {
  const result = await listInvitationInboxRequest(sameOrigin);
  return requireData(
    result.data,
    result.response,
    result.error,
    "Load invitations",
  );
}

export async function acceptInvitation(
  invitationID: string,
  key: string,
): Promise<AcceptedInvitation> {
  const result = await acceptSpaceInvitationRequest({
    ...sameOrigin,
    path: { invitationID },
    headers: { "Idempotency-Key": key },
  });
  return requireMutationData(
    result.data,
    result.response,
    result.error,
    "Accept invitation",
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

function mutationOutcomeUnknown(response: Response | undefined): boolean {
  return (
    !response ||
    response.status === 500 ||
    response.status === 502 ||
    response.status === 504
  );
}

function unknownMutation(action: string): MutationOutcomeUnknownError {
  return new MutationOutcomeUnknownError(
    `${action} may have finished, but Carry could not confirm it. Check the current page before trying again.`,
  );
}

function requireMutationData<T>(
  data: T | undefined,
  response: Response | undefined,
  error: unknown,
  action: string,
): T {
  if (mutationOutcomeUnknown(response)) throw unknownMutation(action);
  return requireData(data, response, error, action);
}

function requireMutationSuccess(
  response: Response | undefined,
  error: unknown,
  action: string,
): void {
  if (mutationOutcomeUnknown(response)) throw unknownMutation(action);
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
    throw new Error(
      `Carry received an incomplete response for ${action}. Try again.`,
    );
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
  if (response) {
    throw new APIResponseError(
      `Carry could not complete ${action}. Try again.`,
      response.status,
    );
  }
  throw new Error(`Carry could not complete ${action}. Try again.`);
}

function isSpaceCreationConflict(
  value: unknown,
): value is SpaceCreationConflict & { slug: string } {
  return isAPIError(value) && "slug" in value && typeof value.slug === "string";
}

function isAPIError(value: unknown): value is { error: string } {
  return (
    typeof value === "object" &&
    value !== null &&
    "error" in value &&
    typeof value.error === "string"
  );
}
