import { useEffect, useState } from "react";

import {
  issueInvitation,
  MutationOutcomeUnknownError,
  managedInvitations,
  removeMember,
  resendInvitation,
  revokeInvitation,
  spaceMembers,
} from "../../carry-api";
import type { ManagedInvitation, SpaceMember } from "../../generated/types.gen";

type PendingMutation =
  | {
      type: "issue";
      key: string;
      email: string;
      manage: boolean;
      enroll: boolean;
    }
  | { type: "resend"; key: string; invitation: ManagedInvitation }
  | { type: "revoke"; key: string; invitation: ManagedInvitation }
  | {
      type: "remove";
      key: string;
      target: SpaceMember;
      successor: string | undefined;
    };

export function MemberSettings({
  spaceID,
  spaceName,
  currentUserID,
  canManage,
  canEnroll,
  onClose,
  onRemoved,
}: {
  spaceID: string;
  spaceName: string;
  currentUserID: string;
  canManage: boolean;
  canEnroll: boolean;
  onClose: () => void;
  onRemoved: (removedSelf: boolean) => void;
}) {
  const [members, setMembers] = useState<Array<SpaceMember>>([]);
  const [nextMemberCursor, setNextMemberCursor] = useState<string | null>(null);
  const [pending, setPending] = useState<Array<ManagedInvitation>>([]);
  const [email, setEmail] = useState("");
  const [grantManage, setGrantManage] = useState(false);
  const [grantEnroll, setGrantEnroll] = useState(false);
  const [removalTarget, setRemovalTarget] = useState<SpaceMember | null>(null);
  const [successor, setSuccessor] = useState("");
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [pendingMutation, setPendingMutation] =
    useState<PendingMutation | null>(null);

  useEffect(() => {
    let active = true;
    void Promise.all([
      spaceMembers(spaceID),
      canManage ? managedInvitations(spaceID) : Promise.resolve([]),
    ])
      .then(([memberPage, loadedPending]) => {
        if (active) {
          setMembers(memberPage.members);
          setNextMemberCursor(memberPage.next_cursor);
          setPending(loadedPending);
        }
      })
      .catch((caught: unknown) => {
        if (active) setError(message(caught));
      })
      .finally(() => {
        if (active) setBusy(false);
      });
    return () => {
      active = false;
    };
  }, [spaceID, canManage]);

  async function loadMoreMembers() {
    if (!nextMemberCursor) return;
    setBusy(true);
    setError(null);
    try {
      const page = await spaceMembers(spaceID, nextMemberCursor);
      setMembers((current) => [
        ...current,
        ...page.members.filter(
          (member) => !current.some((item) => item.user_id === member.user_id),
        ),
      ]);
      setNextMemberCursor(page.next_cursor);
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function issue(
    event: React.FormEvent | null,
    retry?: Extract<PendingMutation, { type: "issue" }>,
  ) {
    event?.preventDefault();
    const command = retry ?? {
      type: "issue" as const,
      key: crypto.randomUUID(),
      email,
      manage: grantManage,
      enroll: grantEnroll,
    };
    setPendingMutation(command);
    setBusy(true);
    setError(null);
    try {
      const created = await issueInvitation(
        spaceID,
        command.email,
        command.manage,
        command.enroll,
        command.key,
      );
      setPending((items) => [
        ...items.filter((item) => item.invitation_id !== created.invitation_id),
        created,
      ]);
      setEmail("");
      setGrantManage(false);
      setGrantEnroll(false);
      setPendingMutation(null);
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError))
        setPendingMutation(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function resend(
    item: ManagedInvitation,
    retry?: Extract<PendingMutation, { type: "resend" }>,
  ) {
    const command = retry ?? {
      type: "resend" as const,
      key: crypto.randomUUID(),
      invitation: item,
    };
    setPendingMutation(command);
    setBusy(true);
    setError(null);
    try {
      const updated = await resendInvitation(
        spaceID,
        command.invitation.invitation_id,
        command.key,
      );
      setPending((items) =>
        items.map((value) =>
          value.invitation_id === updated.invitation_id ? updated : value,
        ),
      );
      setPendingMutation(null);
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError))
        setPendingMutation(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function revoke(
    item: ManagedInvitation,
    retry?: Extract<PendingMutation, { type: "revoke" }>,
  ) {
    const command = retry ?? {
      type: "revoke" as const,
      key: crypto.randomUUID(),
      invitation: item,
    };
    setPendingMutation(command);
    setBusy(true);
    setError(null);
    try {
      await revokeInvitation(
        spaceID,
        command.invitation.invitation_id,
        command.key,
      );
      setPending((items) =>
        items.filter(
          (value) => value.invitation_id !== command.invitation.invitation_id,
        ),
      );
      setPendingMutation(null);
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError))
        setPendingMutation(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function remove(
    target: SpaceMember,
    retry?: Extract<PendingMutation, { type: "remove" }>,
  ) {
    const selectedSuccessor =
      target.open_work_count > 0 ? successor : undefined;
    const command = retry ?? {
      type: "remove" as const,
      key: crypto.randomUUID(),
      target,
      successor: selectedSuccessor,
    };
    setPendingMutation(command);
    setBusy(true);
    setError(null);
    try {
      await removeMember(
        spaceID,
        command.target.user_id,
        command.successor,
        command.key,
      );
      setMembers((items) =>
        items.filter((item) => item.user_id !== command.target.user_id),
      );
      setRemovalTarget(null);
      setSuccessor("");
      setPendingMutation(null);
      onRemoved(command.target.user_id === currentUserID);
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError))
        setPendingMutation(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section
      className="identity-settings"
      aria-labelledby="member-settings-title"
    >
      <div className="identity-settings-heading">
        <div>
          <p className="eyebrow">Settings</p>
          <h2 id="member-settings-title">Members</h2>
        </div>
        <button className="ghost-button" type="button" onClick={onClose}>
          Close
        </button>
      </div>
      {error ? (
        <p className="alert" role="alert">
          {error}
        </p>
      ) : null}
      <ul className="identity-method-list">
        {members.map((member) => (
          <li key={member.user_id}>
            <div>
              <strong>{member.display_name}</strong>
              <span>
                {grants(member.can_manage_members, member.can_enroll_machines)}
              </span>
              <span>
                {member.open_work_count === 1
                  ? "1 Open Work"
                  : `${member.open_work_count} Open Work`}
              </span>
            </div>
            {canManage ? (
              <button
                className="ghost-button"
                type="button"
                disabled={busy}
                onClick={() => {
                  setRemovalTarget(member);
                  setSuccessor("");
                  setError(null);
                }}
              >
                Remove from Space
              </button>
            ) : null}
          </li>
        ))}
      </ul>
      {nextMemberCursor ? (
        <button
          className="ghost-button"
          type="button"
          disabled={busy}
          onClick={() => void loadMoreMembers()}
        >
          Load more members
        </button>
      ) : null}
      {removalTarget ? (
        <section aria-labelledby="remove-member-title">
          <h3 id="remove-member-title">
            Remove {removalTarget.display_name} from {spaceName}?
          </h3>
          <p>
            Future access to {spaceName} ends immediately. Authored history
            stays, and private Conversation rows remain private and retained.
            User credentials and Space Machines are not automatically revoked.
            Pending invitations remain. Data already copied outside Carry is not
            deleted.
          </p>
          {removalTarget.open_work_count > 0 ? (
            <label>
              Transfer all {removalTarget.open_work_count} Open Work to
              <select
                value={successor}
                onChange={(event) => setSuccessor(event.target.value)}
                disabled={busy}
                required
              >
                <option value="">Choose one active successor</option>
                {members
                  .filter((member) => member.user_id !== removalTarget.user_id)
                  .map((member) => (
                    <option key={member.user_id} value={member.user_id}>
                      {member.display_name}
                    </option>
                  ))}
              </select>
            </label>
          ) : null}
          <div className="identity-method-actions">
            <button
              className="primary-button"
              type="button"
              disabled={
                busy ||
                (removalTarget.open_work_count > 0 && successor.length === 0)
              }
              onClick={() => void remove(removalTarget)}
            >
              Remove {removalTarget.display_name}
            </button>
            <button
              className="ghost-button"
              type="button"
              disabled={busy}
              onClick={() => {
                setRemovalTarget(null);
                setSuccessor("");
              }}
            >
              Cancel
            </button>
          </div>
        </section>
      ) : null}
      {canManage ? (
        <>
          <form
            className="identity-code-form"
            onSubmit={(event) => void issue(event)}
          >
            <label htmlFor="invite-email">Invite one exact Email</label>
            <input
              id="invite-email"
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              required
            />
            <label>
              <input
                type="checkbox"
                checked={grantManage}
                onChange={(event) => setGrantManage(event.target.checked)}
              />{" "}
              Can manage members
            </label>
            <label>
              <input
                type="checkbox"
                checked={grantEnroll}
                disabled={!canEnroll}
                onChange={(event) => setGrantEnroll(event.target.checked)}
              />{" "}
              Can enroll Machines
            </label>
            <button className="primary-button" disabled={busy || !email.trim()}>
              Create invitation
            </button>
          </form>
          {pendingMutation ? (
            <button
              className="ghost-button"
              type="button"
              disabled={busy}
              onClick={() => {
                if (pendingMutation.type === "issue")
                  void issue(null, pendingMutation);
                else if (pendingMutation.type === "resend")
                  void resend(pendingMutation.invitation, pendingMutation);
                else if (pendingMutation.type === "revoke")
                  void revoke(pendingMutation.invitation, pendingMutation);
                else void remove(pendingMutation.target, pendingMutation);
              }}
            >
              Retry exact change
            </button>
          ) : null}
          <h3>Pending invitations</h3>
          <ul className="identity-method-list">
            {pending.map((item) => (
              <li key={item.invitation_id}>
                <div>
                  <strong>{item.recipient_email}</strong>
                  <span>
                    {grants(item.can_manage_members, item.can_enroll_machines)}
                  </span>
                  <span>{submissionCopy(item.submission.state)}</span>
                </div>
                <div className="identity-method-actions">
                  <button
                    className="secondary-button"
                    type="button"
                    disabled={busy}
                    onClick={() => void resend(item)}
                  >
                    Resend
                  </button>
                  <button
                    className="ghost-button"
                    type="button"
                    disabled={busy}
                    onClick={() => void revoke(item)}
                  >
                    Revoke
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </>
      ) : (
        <p>
          Only a current member manager can view or create pending invitations.
        </p>
      )}
    </section>
  );
}

function grants(manage: boolean, enroll: boolean) {
  return [
    manage ? "Can manage members" : "Cannot manage members",
    enroll ? "Can enroll Machines" : "Cannot enroll Machines",
  ].join(" · ");
}
function submissionCopy(state: ManagedInvitation["submission"]["state"]) {
  switch (state) {
    case "accepted":
      return "The email provider accepted this submission. Delivery or reading is not confirmed.";
    case "rejected":
      return "The invitation exists, but the provider rejected this submission.";
    case "unknown":
      return "Carry cannot confirm whether the provider accepted this submission. Resend is a new explicit attempt.";
    default:
      return "Carry recorded the submission intent but has not confirmed an external outcome.";
  }
}
function message(value: unknown) {
  return value instanceof Error
    ? value.message
    : "Carry could not manage members";
}
