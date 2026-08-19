package run

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateUnderstandingUpdateTrimsAndRejectsInvalidFields(t *testing.T) {
	understanding, nextStep, err := ValidateUnderstandingUpdate("  Current facts  ", "  Ask the owner  ")
	if err != nil {
		t.Fatalf("validate update: %v", err)
	}
	if understanding != "Current facts" || nextStep != "Ask the owner" {
		t.Fatalf("normalized update = %q, %q", understanding, nextStep)
	}
	if _, _, err := ValidateUnderstandingUpdate("", "Continue"); !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("missing understanding error = %v", err)
	}
	if _, _, err := ValidateUnderstandingUpdate("Known", strings.Repeat("n", MaxNextStepBytes+1)); !errors.Is(err, ErrInvalidUpdate) {
		t.Fatalf("oversized next step error = %v", err)
	}
}

func TestValidateUnresolvedOutcomeAcceptsOnlyFailedAndUnknown(t *testing.T) {
	for _, outcome := range []State{StateFailed, StateUnknown} {
		if err := ValidateUnresolvedOutcome(outcome); err != nil {
			t.Fatalf("validate %s: %v", outcome, err)
		}
	}
	for _, outcome := range []State{"", StateActive, StateSucceeded} {
		if err := ValidateUnresolvedOutcome(outcome); !errors.Is(err, ErrInvalidOutcome) {
			t.Fatalf("invalid outcome %q error = %v", outcome, err)
		}
	}
}
