import { useState } from "react";

import {
  acceptInvitation,
  invitationInbox,
  MutationOutcomeUnknownError,
  requestIdentityEmailCode,
  verifyIdentityEmailCode,
} from "../../carry-api";
import type { InvitationInbox } from "../../generated/types.gen";
import { IdentityMethodSettings } from "./identity-methods";
import type { TargetedInvitationState } from "./use-user-session";

type InvitationEmailChallenge = {
  id: string;
  requestKey: string;
  verifyKey: string;
};

function useInvitationEmailProof(onVerified: () => void | Promise<void>) {
  const [challenge, setChallenge] = useState<InvitationEmailChallenge | null>(
    null,
  );
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function request(retry?: InvitationEmailChallenge) {
    const next = retry ?? {
      id: crypto.randomUUID(),
      requestKey: crypto.randomUUID(),
      verifyKey: crypto.randomUUID(),
    };
    setChallenge(next);
    setBusy(true);
    setError(null);
    try {
      await requestIdentityEmailCode({
        purpose: "reauthenticate",
        challengeID: next.id,
        idempotencyKey: next.requestKey,
      });
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  async function retry() {
    if (challenge) await request(challenge);
  }

  async function verify(event: React.FormEvent) {
    event.preventDefault();
    if (!challenge || !/^\d{6}$/.test(code)) return;
    setBusy(true);
    setError(null);
    try {
      await verifyIdentityEmailCode(
        "reauthenticate",
        challenge.id,
        code,
        challenge.verifyKey,
      );
      setChallenge(null);
      setCode("");
      await onVerified();
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  return {
    busy,
    challenge,
    code,
    error,
    request,
    retry,
    setCode,
    verify,
  };
}

export function InvitationInboxView({
  initialInbox,
  onSkip,
}: {
  initialInbox: InvitationInbox;
  onSkip: () => void;
}) {
  const [inbox, setInbox] = useState(initialInbox);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pendingAccept, setPendingAccept] = useState<{
    invitationID: string;
    key: string;
  } | null>(null);
  const proof = useInvitationEmailProof(refresh);

  async function refresh() {
    setInbox(await invitationInbox());
  }

  async function accept(
    invitationID: string,
    retry?: { invitationID: string; key: string },
  ) {
    const command = retry ?? {
      invitationID,
      key: crypto.randomUUID(),
    };
    setPendingAccept(command);
    setBusy(true);
    setError(null);
    try {
      await acceptInvitation(command.invitationID, command.key);
      setPendingAccept(null);
      window.location.assign("/");
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError))
        setPendingAccept(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="center-state invitation-inbox">
      <p className="brand-mark">
        Carry<span className="brand-dot">.</span>
      </p>
      <h1>Space invitations</h1>
      {(error ?? proof.error) ? (
        <p className="alert" role="alert">
          {error ?? proof.error}
        </p>
      ) : null}
      {inbox.invitations.length === 0 ? (
        <>
          <p>No pending Space invitations.</p>
          <button className="ghost-button" type="button" onClick={onSkip}>
            Back to Carry
          </button>
        </>
      ) : (
        <>
          {inbox.reauthentication_required ? (
            <section className="identity-confirmation">
              <p>
                Confirm the invited Email before joining. Authentication alone
                does not accept an invitation.
              </p>
              {!proof.challenge ? (
                <button
                  className="secondary-button"
                  type="button"
                  disabled={busy || proof.busy}
                  onClick={() => void proof.request()}
                >
                  Confirm with Email
                </button>
              ) : (
                <form onSubmit={(event) => void proof.verify(event)}>
                  <label htmlFor="invitation-email-code">
                    Newest six-digit Email code
                  </label>
                  <input
                    id="invitation-email-code"
                    inputMode="numeric"
                    value={proof.code}
                    onChange={(event) =>
                      proof.setCode(
                        event.target.value.replace(/\D/g, "").slice(0, 6),
                      )
                    }
                  />
                  <button
                    className="primary-button"
                    disabled={busy || proof.busy || !/^\d{6}$/.test(proof.code)}
                  >
                    Confirm Email
                  </button>
                  <button
                    className="ghost-button"
                    type="button"
                    disabled={busy || proof.busy}
                    onClick={() => void proof.retry()}
                  >
                    Retry exact code request
                  </button>
                  <button
                    className="ghost-button"
                    type="button"
                    disabled={busy || proof.busy}
                    onClick={() => void proof.request()}
                  >
                    Send a new code
                  </button>
                </form>
              )}
            </section>
          ) : null}
          <ul className="identity-method-list">
            {inbox.invitations.map((item) => (
              <li key={item.invitation_id}>
                <div>
                  <strong>{item.space_name}</strong>
                  <span>Invited by {item.inviter_display_name}</span>
                  <span>
                    {grants(item.can_manage_members, item.can_enroll_machines)}
                  </span>
                </div>
                <button
                  className="primary-button"
                  type="button"
                  disabled={busy || inbox.reauthentication_required}
                  onClick={() => void accept(item.invitation_id)}
                >
                  Accept and join
                </button>
              </li>
            ))}
          </ul>
          {pendingAccept ? (
            <button
              className="ghost-button"
              type="button"
              disabled={busy}
              onClick={() =>
                void accept(pendingAccept.invitationID, pendingAccept)
              }
            >
              Retry exact acceptance
            </button>
          ) : null}
          <button className="ghost-button" type="button" onClick={onSkip}>
            Not now
          </button>
        </>
      )}
    </main>
  );
}

export function TargetedInvitationView({
  state,
  onReload,
  onSkip,
  onSignOut,
}: {
  state: TargetedInvitationState;
  onReload: () => void | Promise<void>;
  onSkip: () => void;
  onSignOut: () => void;
}) {
  const [showMethods, setShowMethods] = useState(false);
  const [acceptKey, setAcceptKey] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const proof = useInvitationEmailProof(onReload);
  const invitation = state.status === "owner" ? state.invitation : null;

  async function accept() {
    if (!invitation) return;
    const key = acceptKey ?? crypto.randomUUID();
    setAcceptKey(key);
    setBusy(true);
    setError(null);
    try {
      await acceptInvitation(invitation.invitation_id, key);
      window.location.assign("/");
    } catch (caught) {
      if (caught instanceof MutationOutcomeUnknownError) {
        setError(
          "Carry cannot confirm whether acceptance completed. Reload to reconcile the invitation before choosing again.",
        );
      } else {
        setAcceptKey(null);
        setError(message(caught));
      }
    } finally {
      setBusy(false);
    }
  }

  if (showMethods)
    return (
      <IdentityMethodSettings
        onClose={() => {
          setShowMethods(false);
          onReload();
        }}
      />
    );

  return (
    <main className="center-state invitation-inbox">
      <p className="brand-mark">
        Carry<span className="brand-dot">.</span>
      </p>
      <h1>Space invitation</h1>
      {(error ?? proof.error) ? (
        <p className="alert" role="alert">
          {error ?? proof.error}
        </p>
      ) : null}
      {state.status === "loading" ? (
        <p>Loading invitation…</p>
      ) : state.status === "error" ? (
        <>
          <p className="alert" role="alert">
            {state.message}
          </p>
          <button className="secondary-button" type="button" onClick={onReload}>
            Reload invitation
          </button>
        </>
      ) : state.status === "unavailable" ? (
        state.hasEmail ? (
          <>
            <p>This signed-in account cannot review this invitation.</p>
            <p>
              Sign out, then sign in with the email address that received this
              invitation.
            </p>
            <button
              className="secondary-button"
              type="button"
              onClick={onSignOut}
            >
              Sign out
            </button>
          </>
        ) : (
          <>
            <p>Confirm your Email to review this invitation.</p>
            <button
              className="secondary-button"
              type="button"
              onClick={() => setShowMethods(true)}
            >
              Confirm Email
            </button>
          </>
        )
      ) : invitation ? (
        <>
          <strong>{invitation.space_name}</strong>
          <p>Invited by {invitation.inviter_display_name}</p>
          <p>
            {grants(
              invitation.can_manage_members,
              invitation.can_enroll_machines,
            )}
          </p>
          {invitation.state === "revoked" ? (
            <p>This invitation was revoked before acceptance.</p>
          ) : null}
          {invitation.state === "expired" ? (
            <p>This invitation expired before acceptance.</p>
          ) : null}
          {invitation.state === "accepted" ? (
            <>
              <p>
                {invitation.accept_result === "already_member"
                  ? "This invitation was accepted while you were already a member."
                  : "This invitation was accepted and joined the Space."}
              </p>
              <p>
                {invitation.current_member
                  ? "Your Membership is current."
                  : "You are no longer a current member of this Space."}
              </p>
            </>
          ) : null}
          {invitation.state === "pending" ? (
            invitation.reauthentication_required ? (
              <section className="identity-confirmation">
                <p>Confirm the invited Email before accepting.</p>
                {!proof.challenge ? (
                  <button
                    className="secondary-button"
                    type="button"
                    disabled={busy || proof.busy}
                    onClick={() => void proof.request()}
                  >
                    Confirm with Email
                  </button>
                ) : (
                  <form onSubmit={(event) => void proof.verify(event)}>
                    <label htmlFor="targeted-invitation-code">
                      Newest six-digit Email code
                    </label>
                    <input
                      id="targeted-invitation-code"
                      inputMode="numeric"
                      value={proof.code}
                      onChange={(event) =>
                        proof.setCode(
                          event.target.value.replace(/\D/g, "").slice(0, 6),
                        )
                      }
                    />
                    <button
                      className="primary-button"
                      disabled={
                        busy || proof.busy || !/^\d{6}$/.test(proof.code)
                      }
                    >
                      Confirm Email
                    </button>
                    <button
                      className="ghost-button"
                      type="button"
                      disabled={busy || proof.busy}
                      onClick={() => void proof.retry()}
                    >
                      Retry exact code request
                    </button>
                    <button
                      className="ghost-button"
                      type="button"
                      disabled={busy || proof.busy}
                      onClick={() => void proof.request()}
                    >
                      Send a new code
                    </button>
                  </form>
                )}
              </section>
            ) : (
              <button
                className="primary-button"
                type="button"
                disabled={busy}
                onClick={() => void accept()}
              >
                Accept and join
              </button>
            )
          ) : null}
        </>
      ) : null}
      {acceptKey ? (
        <button
          className="ghost-button"
          type="button"
          disabled={busy}
          onClick={onReload}
        >
          Reload invitation status
        </button>
      ) : null}
      <button className="ghost-button" type="button" onClick={onSkip}>
        Not now
      </button>
    </main>
  );
}

function grants(manage: boolean, enroll: boolean) {
  const values = [
    manage ? "Can manage members" : "Cannot manage members",
    enroll ? "Can connect Hosts" : "Cannot connect Hosts",
  ];
  return values.join(" · ");
}
function message(value: unknown) {
  return value instanceof Error
    ? value.message
    : "Carry could not complete the invitation";
}
