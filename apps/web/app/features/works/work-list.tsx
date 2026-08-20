import type { WorkSummary } from "../../generated/types.gen";
import { formatWorkDate } from "./work-time";

type WorkListProps = {
  works: Array<WorkSummary>;
  hasEarlier: boolean;
  needsYouOnly: boolean;
  selectedWorkID: string | null;
  busy: boolean;
  onSelect: (workID: string) => void;
  onViewChange: (needsYouOnly: boolean) => void;
  onLoadEarlier: () => void;
};

export function WorkList({
  works,
  hasEarlier,
  needsYouOnly,
  selectedWorkID,
  busy,
  onSelect,
  onViewChange,
  onLoadEarlier,
}: WorkListProps) {
  return (
    <section className="work-list" aria-labelledby="work-list-title">
      <div className="section-heading">
        <h2 id="work-list-title">Work</h2>
        <span className="work-count">
          {needsYouOnly
            ? `${works.length}${hasEarlier ? "+" : ""} need you`
            : works.length === 1 && !hasEarlier
              ? "1 open"
              : `${works.length}${hasEarlier ? "+" : ""} open`}
        </span>
      </div>
      <div className="work-view-switch" role="group" aria-label="Work view">
        <button
          type="button"
          className={!needsYouOnly ? "selected" : undefined}
          aria-pressed={!needsYouOnly}
          disabled={busy}
          onClick={() => onViewChange(false)}
        >
          All Work
        </button>
        <button
          type="button"
          className={needsYouOnly ? "selected" : undefined}
          aria-pressed={needsYouOnly}
          disabled={busy}
          onClick={() => onViewChange(true)}
        >
          Needs You
        </button>
      </div>
      {works.length === 0 ? (
        <p className="empty-copy">
          {needsYouOnly
            ? "Nothing needs you right now."
            : "No Work yet. Give Carry one clear responsibility above."}
        </p>
      ) : (
        <ul>
          {works.map((item) => (
            <li key={item.work_id}>
              <button
                type="button"
                className={
                  item.work_id === selectedWorkID
                    ? "work-link selected"
                    : "work-link"
                }
                onClick={() => onSelect(item.work_id)}
                disabled={busy}
                aria-current={
                  item.work_id === selectedWorkID ? "true" : undefined
                }
              >
                <span className="work-goal">{item.goal}</span>
                <span className="work-meta">
                  <span className="status-dot" aria-hidden="true" />
                  {item.needs_review
                    ? "Needs review"
                    : item.needs_retry
                      ? "Try again"
                      : "Open"}
                  <span aria-hidden="true">·</span>
                  {item.owner_display_name}
                  <span aria-hidden="true">·</span>
                  <time dateTime={item.created_at}>
                    {formatWorkDate(item.created_at)}
                  </time>
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {hasEarlier ? (
        <button
          className="ghost-button load-earlier"
          type="button"
          onClick={onLoadEarlier}
          disabled={busy}
        >
          Load earlier Work
        </button>
      ) : null}
    </section>
  );
}
