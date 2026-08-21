import { useState } from "react";

import { ConversationPanel } from "./features/conversation/conversation-panel";
import { FirstSpace } from "./features/user-session/first-space";
import { IdentityMethodSettings } from "./features/user-session/identity-methods";
import { InvitationInboxView } from "./features/user-session/invitation-inbox";
import { MemberSettings } from "./features/user-session/member-settings";
import { useUserSession } from "./features/user-session/use-user-session";
import { UserEntry } from "./features/user-session/user-entry";
import { CreateWorkForm } from "./features/works/create-work-form";
import { useWorkBoard } from "./features/works/use-work-board";
import { WorkDetail } from "./features/works/work-detail";
import { WorkList } from "./features/works/work-list";

export function App() {
  const [settingsPanel, setSettingsPanel] = useState<
    "identity" | "members" | null
  >(hasIdentityChangeStatus() ? "identity" : null);
  const session = useUserSession();
  const board = useWorkBoard(session.user);
  const busy = session.busy || board.busy;

  if (session.phase === "checking") {
    return (
      <main className="center-state" aria-live="polite">
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
        <p className="center-state-copy">Opening Carry…</p>
      </main>
    );
  }
  if (session.phase === "failed") {
    return (
      <main className="center-state">
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
        <p className="alert" role="alert">
          {session.error}
        </p>
        <button className="ghost-button" type="button" onClick={session.retry}>
          Try again
        </button>
      </main>
    );
  }
  if (session.phase === "signing-out") {
    return (
      <main className="center-state">
        <p className="brand-mark">
          Carry<span className="brand-dot">.</span>
        </p>
        <p className="center-state-copy">
          Your private conversation is hidden on this browser.
        </p>
        <p className="center-state-copy">
          Your Work is hidden on this browser.
        </p>
        {session.error ? (
          <p className="alert" role="alert">
            {session.error}
          </p>
        ) : null}
        <button
          className="ghost-button"
          type="button"
          onClick={() => void session.finishSignOut()}
          disabled={session.busy}
        >
          {session.busy ? "Signing out…" : "Finish signing out"}
        </button>
      </main>
    );
  }
  if (session.phase === "email" || session.phase === "code") {
    return (
      <UserEntry
        key={session.challengeID || "email-entry"}
        step={session.phase}
        email={session.email}
        busy={busy}
        error={session.error}
        canRetryCodeRequest={session.canRetryCodeRequest}
        onSendCode={session.sendCode}
        onRetryCodeRequest={session.retryCodeRequest}
        onVerifyCode={session.verifyCode}
        onResendCode={session.resendCode}
        onBack={session.backToEmail}
      />
    );
  }
  if (session.phase === "invitations" && session.user && session.inbox) {
    return (
      <InvitationInboxView
        user={session.user}
        initialInbox={session.inbox}
        onChanged={session.refresh}
        onSkip={session.skipInvitations}
      />
    );
  }
  if (session.phase === "first-space") {
    return (
      <FirstSpace
        busy={busy}
        error={session.error}
        initialName={session.user?.display_name ?? ""}
        onCreate={session.createSpace}
      />
    );
  }
  if (!session.user) {
    return null;
  }

  const currentSpace = session.user.spaces.find(
    (space) => space.space_id === board.spaceID,
  );
  return (
    <div className="app-shell">
      <header className="app-header">
        <a className="brand" href="/" aria-label="Carry home">
          Carry
          <span className="brand-dot" aria-hidden="true">
            .
          </span>
        </a>
        <div className="header-actions">
          <span className="member-name">{session.user.display_name}</span>
          {session.user.spaces.length > 1 ? (
            <label className="space-picker">
              <span className="space-picker-label">Space</span>
              <select
                value={board.spaceID ?? ""}
                onChange={(event) => void board.selectSpace(event.target.value)}
                disabled={busy}
              >
                <option value="" disabled>
                  Choose a Space
                </option>
                {session.user.spaces.map((space) => (
                  <option key={space.space_id} value={space.space_id}>
                    {space.name}
                  </option>
                ))}
              </select>
            </label>
          ) : (
            <span className="space-name">
              {currentSpace?.name ?? "No Space"}
            </span>
          )}
          <button
            className="ghost-button"
            type="button"
            onClick={() =>
              setSettingsPanel((panel) => (panel ? null : "identity"))
            }
            disabled={busy}
          >
            Settings
          </button>
          <button
            className="ghost-button"
            type="button"
            onClick={() => void session.signOut()}
            disabled={busy}
          >
            Sign out
          </button>
        </div>
      </header>

      <main className="workspace">
        {settingsPanel ? (
          <>
            <nav
              className="identity-method-actions"
              aria-label="Settings sections"
            >
              <button
                className="ghost-button"
                type="button"
                onClick={() => setSettingsPanel("identity")}
              >
                Sign-in methods
              </button>
              {currentSpace ? (
                <button
                  className="ghost-button"
                  type="button"
                  onClick={() => setSettingsPanel("members")}
                >
                  Members
                </button>
              ) : null}
            </nav>
            {settingsPanel === "identity" ? (
              <IdentityMethodSettings onClose={() => setSettingsPanel(null)} />
            ) : null}
            {settingsPanel === "members" && currentSpace ? (
              <MemberSettings
                key={currentSpace.space_id}
                spaceID={currentSpace.space_id}
                spaceName={currentSpace.name}
                currentUserID={session.user.user_id}
                canManage={currentSpace.can_manage_members}
                canEnroll={currentSpace.can_enroll_machines}
                onClose={() => setSettingsPanel(null)}
                onRemoved={(removedSelf) => {
                  if (removedSelf) setSettingsPanel(null);
                  session.refresh();
                }}
              />
            ) : null}
          </>
        ) : null}
        <div className="workspace-intro">
          <div>
            <p className="eyebrow">Shared responsibility</p>
            <h1>What should Carry keep moving?</h1>
          </div>
          <p className="workspace-lede">
            Give Carry one clear goal. It keeps the Work moving, remembers what
            happened, and comes back when your judgement is needed.
          </p>
        </div>
        {session.error || board.error ? (
          <p className="alert global-alert" role="alert">
            {session.error ?? board.error}
          </p>
        ) : null}
        {board.spaceID ? (
          <>
            <ConversationPanel
              key={`${session.user.user_id}:${board.spaceID}`}
              memberID={session.user.user_id}
              spaceID={board.spaceID}
              workBusy={board.busy}
              onOpenWork={board.selectWork}
            />
            <section
              className="shared-work"
              aria-labelledby="shared-work-title"
            >
              <div className="shared-work-heading">
                <p className="eyebrow">Visible to the Space</p>
                <h2 id="shared-work-title">Shared Work</h2>
              </div>
              <CreateWorkForm busy={busy} onCreate={board.addWork} />
              <div className="work-grid">
                <WorkList
                  works={board.works}
                  hasEarlier={board.hasEarlierWorks}
                  needsYouOnly={board.needsYouOnly}
                  selectedWorkID={board.details?.work.work_id ?? null}
                  busy={busy}
                  onSelect={(workID) => void board.selectWork(workID)}
                  onViewChange={(value) => void board.showNeedsYou(value)}
                  onLoadEarlier={() => void board.loadEarlierWorks()}
                />
                <WorkDetail
                  key={board.details?.work.work_id ?? "no-work-selected"}
                  details={board.details}
                  busy={busy}
                  currentMemberID={session.user.user_id}
                  onMessage={board.addMessage}
                  onRetry={board.retryCurrentWork}
                  onAcceptReview={board.acceptCurrentReview}
                  onLoadEarlierMessages={() => void board.loadEarlierMessages()}
                />
              </div>
            </section>
          </>
        ) : session.user.spaces.length > 1 ? (
          <p className="empty-panel">
            Choose a Space before talking to Carry or opening shared Work.
          </p>
        ) : null}
      </main>
    </div>
  );
}

function hasIdentityChangeStatus(): boolean {
  return new URL(window.location.href).searchParams.has("identity_change");
}
