import { MemberEntry } from "./features/member-session/member-entry";
import { useMemberSession } from "./features/member-session/use-member-session";
import { CreateWorkForm } from "./features/works/create-work-form";
import { useWorkBoard } from "./features/works/use-work-board";
import { WorkDetail } from "./features/works/work-detail";
import { WorkList } from "./features/works/work-list";

export function App() {
  const session = useMemberSession();
  const board = useWorkBoard(session.member);
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
  if (session.phase === "login") {
    return (
      <MemberEntry
        busy={busy}
        error={session.error}
        onOpen={session.openWithToken}
      />
    );
  }
  if (!session.member) {
    return null;
  }

  const currentSpace = session.member.spaces.find(
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
          {session.member.spaces.length > 1 ? (
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
                {session.member.spaces.map((space) => (
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
            onClick={() => void session.signOut()}
            disabled={busy}
          >
            Sign out
          </button>
        </div>
      </header>

      <main className="workspace">
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
            <CreateWorkForm busy={busy} onCreate={board.addWork} />
            <div className="work-grid">
              <WorkList
                works={board.works}
                selectedWorkID={board.details?.work.work_id ?? null}
                busy={busy}
                onSelect={(workID) => void board.selectWork(workID)}
              />
              <WorkDetail
                key={board.details?.work.work_id ?? "no-work-selected"}
                details={board.details}
                busy={busy}
                currentMemberID={session.member.user_id}
                onMessage={board.addMessage}
                onRetry={board.retryCurrentWork}
              />
            </div>
          </>
        ) : session.member.spaces.length > 1 ? (
          <p className="empty-panel">
            Choose a Space before opening shared Work.
          </p>
        ) : (
          <p className="empty-panel">
            This member does not belong to a Space yet. Ask a Space owner to add
            you, then open Carry again.
          </p>
        )}
      </main>
    </div>
  );
}
