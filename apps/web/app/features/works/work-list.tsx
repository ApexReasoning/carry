import type { WorkSummary } from "../../generated/types.gen";
import { formatWorkDate } from "./work-time";

type WorkListProps = {
  works: Array<WorkSummary>;
  hasEarlier: boolean;
  selectedWorkID: string | null;
  busy: boolean;
  onSelect: (workID: string) => void;
  onLoadEarlier: () => void;
};

export function WorkList({
  works,
  hasEarlier,
  selectedWorkID,
  busy,
  onSelect,
  onLoadEarlier,
}: WorkListProps) {
  return (
    <section className="work-list" aria-labelledby="work-list-title">
      <div className="section-heading">
        <h2 id="work-list-title">Work</h2>
        <span className="work-count">
          {works.length === 1 && !hasEarlier
            ? "1 open"
            : `${works.length}${hasEarlier ? "+" : ""} open`}
        </span>
      </div>
      {works.length === 0 ? (
        <p className="empty-copy">
          No Work yet. Give Carry one clear responsibility above.
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
                  Open
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
