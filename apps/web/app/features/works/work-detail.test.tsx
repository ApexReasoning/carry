import { fireEvent, render, screen } from "@testing-library/react";
import { expect, test, vi } from "vitest";

import type { WorkDetails } from "../../carry-api";
import { WorkDetail } from "./work-detail";

const memberID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa";
const reviewID = "77777777-7777-4777-8777-777777777777";

function reviewableDetails(ownerUserID = memberID): WorkDetails {
  return {
    work: {
      work_id: "22222222-2222-4222-8222-222222222222",
      space_id: "11111111-1111-4111-8111-111111111111",
      goal: "Prepare the renewal recommendation",
      lifecycle: "open",
      owner_user_id: ownerUserID,
      owner_display_name: "Alex Morgan",
      creator_user_id: memberID,
      creator_display_name: "Alex Morgan",
      understanding: "The recommendation is ready for review.",
      next_step: "The responsible member should inspect this stage result.",
      has_unapplied_input: false,
      needs_retry: false,
      needs_review: true,
      review_id: reviewID,
      created_at: "2026-08-20T12:00:00Z",
    },
    messages: [],
    has_earlier_messages: false,
  };
}

test("lets only the responsible member accept the exact current result", () => {
  const onAcceptReview = vi.fn();
  const { rerender } = render(
    <WorkDetail
      details={reviewableDetails()}
      busy={false}
      currentMemberID={memberID}
      onMessage={vi.fn()}
      onRetry={vi.fn()}
      onAcceptReview={onAcceptReview}
      onLoadEarlierMessages={vi.fn()}
    />,
  );

  expect(
    screen.getByRole("heading", { name: "Review this result" }),
  ).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Accept this result" }));
  expect(onAcceptReview).toHaveBeenCalledOnce();

  rerender(
    <WorkDetail
      details={reviewableDetails("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")}
      busy={false}
      currentMemberID={memberID}
      onMessage={vi.fn()}
      onRetry={vi.fn()}
      onAcceptReview={onAcceptReview}
      onLoadEarlierMessages={vi.fn()}
    />,
  );
  expect(
    screen.queryByRole("button", { name: "Accept this result" }),
  ).toBeNull();
  expect(
    screen.getByText(
      "Waiting for the responsible member to review this result.",
    ),
  ).toBeVisible();
});
