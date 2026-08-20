import type { WorkDetails, WorkPage } from "../../carry-api";
import type { Work, WorkMessage, WorkSummary } from "../../generated/types.gen";

export function emptyWorkPage(): WorkPage {
  return { works: [], has_earlier_works: false };
}

export function summaryFromWork(value: Work): WorkSummary {
  return {
    work_id: value.work_id,
    space_id: value.space_id,
    goal: value.goal,
    lifecycle: value.lifecycle,
    owner_user_id: value.owner_user_id,
    owner_display_name: value.owner_display_name,
    creator_user_id: value.creator_user_id,
    creator_display_name: value.creator_display_name,
    has_unapplied_input: value.has_unapplied_input,
    needs_retry: value.needs_retry,
    needs_review: value.needs_review,
    created_at: value.created_at,
  };
}

export function upsertSummary(
  current: Array<WorkSummary>,
  updated: WorkSummary,
): Array<WorkSummary> {
  return [
    updated,
    ...current.filter((item) => item.work_id !== updated.work_id),
  ];
}

export function mergeDetails(
  current: WorkDetails | null,
  reloaded: WorkDetails,
): WorkDetails {
  if (!current || current.work.work_id !== reloaded.work.work_id) {
    return reloaded;
  }
  return {
    work: reloaded.work,
    messages: mergeByID(current.messages, reloaded.messages, "message_id"),
    // Reloading the newest page cannot create an older message than the
    // earliest page already visible, so preserve that cursor fact.
    has_earlier_messages: current.has_earlier_messages,
  };
}

export function mergeByID<T extends WorkSummary | WorkMessage>(
  first: Array<T>,
  second: Array<T>,
  key: "work_id" | "message_id",
): Array<T> {
  const seen = new Set<string>();
  return [...first, ...second].filter((item) => {
    const id = item[key as keyof T];
    if (typeof id !== "string" || seen.has(id)) return false;
    seen.add(id);
    return true;
  });
}
