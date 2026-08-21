import { useState } from "react";

import {
  acceptInvitation,
  invitationInbox,
  MutationOutcomeUnknownError,
  requestIdentityEmailCode,
  verifyIdentityEmailCode,
} from "../../carry-api";
import type { InvitationInbox, User } from "../../generated/types.gen";
import { IdentityMethodSettings } from "./identity-methods";

export function InvitationInboxView({
  user,
  initialInbox,
  onChanged,
  onSkip,
}: {
  user: User;
  initialInbox: InvitationInbox;
  onChanged: () => void;
  onSkip: () => void;
}) {
  const [inbox, setInbox] = useState(initialInbox);
  const [showMethods, setShowMethods] = useState(false);
  const [challenge, setChallenge] = useState<{
    id: string;
    requestKey: string;
    verifyKey: string;
  } | null>(null);
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pendingAccept, setPendingAccept] = useState<{
    invitationID: string;
    key: string;
  } | null>(null);

  async function refresh() {
    const loaded = await invitationInbox();
    setInbox(loaded);
    if (!loaded.reauthentication_required) setChallenge(null);
  }

  async function confirmEmail(retry?: {
    id: string;
    requestKey: string;
    verifyKey: string;
  }) {
    const next = retry ?? {
      id: crypto.randomUUID(),
      requestKey: crypto.randomUUID(),
      verifyKey: crypto.randomUUID(),
    };
    setChallenge(next);
    setBusy(true);
    setError(null);
    try {
      await requestIdentityEmailCode(
        "reauthenticate",
        next.id,
        next.requestKey,
      );
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
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
      await refresh();
    } catch (caught) {
      setError(message(caught));
    } finally {
      setBusy(false);
    }
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
      window.history.replaceState(null, "", "/");
      setPendingAccept(null);
      onChanged();
    } catch (caught) {
      if (!(caught instanceof MutationOutcomeUnknownError))
        setPendingAccept(null);
      setError(message(caught));
    } finally {
      setBusy(false);
    }
  }

  if (showMethods)
    return (
      <IdentityMethodSettings
        onClose={() => {
          setShowMethods(false);
          void refresh();
        }}
      />
    );
  return (
    <main className="center-state invitation-inbox">
      <p className="brand-mark">
        Carry<span className="brand-dot">.</span>
      </p>
      <h1>Space invitations</h1>
      {error ? (
        <p className="alert" role="alert">
          {error}
        </p>
      ) : null}
      {inbox.invitations.length === 0 ? (
        <>
          <p>No invitation matches this Carry User’s linked Email.</p>
          <p>
            Google or GitHub profile email is not used. Link the exact invited
            Email explicitly.
          </p>
          <button
            className="secondary-button"
            type="button"
            onClick={() => setShowMethods(true)}
          >
            Manage sign-in methods
          </button>
          <button className="ghost-button" type="button" onClick={onSkip}>
            {user.spaces.length === 0
              ? "Create a new Space instead"
              : "Back to Carry"}
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
              {!challenge ? (
                <button
                  className="secondary-button"
                  type="button"
                  disabled={busy}
                  onClick={() => void confirmEmail()}
                >
                  Confirm with Email
                </button>
              ) : (
                <form onSubmit={(event) => void verify(event)}>
                  <label htmlFor="invitation-email-code">
                    Newest six-digit Email code
                  </label>
                  <input
                    id="invitation-email-code"
                    inputMode="numeric"
                    value={code}
                    onChange={(event) =>
                      setCode(event.target.value.replace(/\D/g, "").slice(0, 6))
                    }
                  />
                  <button
                    className="primary-button"
                    disabled={busy || !/^\d{6}$/.test(code)}
                  >
                    Confirm Email
                  </button>
                  <button
                    className="ghost-button"
                    type="button"
                    disabled={busy}
                    onClick={() => void confirmEmail(challenge)}
                  >
                    Retry exact code request
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

function grants(manage: boolean, enroll: boolean) {
  const values = [
    manage ? "Can manage members" : "Cannot manage members",
    enroll ? "Can enroll Machines" : "Cannot enroll Machines",
  ];
  return values.join(" · ");
}
function message(value: unknown) {
  return value instanceof Error
    ? value.message
    : "Carry could not complete the invitation";
}
