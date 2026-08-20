import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { WorkSummary } from "../../generated/types.gen";
import { WorkList } from "./work-list";

const reviewable: WorkSummary = {
  work_id: "22222222-2222-4222-8222-222222222222",
  space_id: "11111111-1111-4111-8111-111111111111",
  goal: "Review the renewal recommendation",
  lifecycle: "open",
  owner_user_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  owner_display_name: "Alex Morgan",
  creator_user_id: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
  creator_display_name: "Alex Morgan",
  has_unapplied_input: false,
  needs_retry: false,
  needs_review: true,
  created_at: "2026-08-20T12:00:00Z",
};

test("exposes Needs You as a Work query rather than a new object", () => {
  const onViewChange = vi.fn();
  const { rerender } = render(
    <WorkList
      works={[reviewable]}
      hasEarlier={false}
      needsYouOnly={false}
      selectedWorkID={null}
      busy={false}
      onSelect={vi.fn()}
      onViewChange={onViewChange}
      onLoadEarlier={vi.fn()}
    />,
  );

  expect(screen.getByRole("button", { name: /Needs review/ })).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Needs You" }));
  expect(onViewChange).toHaveBeenCalledWith(true);

  rerender(
    <WorkList
      works={[]}
      hasEarlier={false}
      needsYouOnly
      selectedWorkID={null}
      busy={false}
      onSelect={vi.fn()}
      onViewChange={onViewChange}
      onLoadEarlier={vi.fn()}
    />,
  );
  expect(screen.getByText("Nothing needs you right now.")).toBeVisible();
});
