package run

import (
	"errors"
	"strings"
	"testing"
)

func TestAgentCredentialIsOpaqueAndDigestVerifiesOnlyExactSecret(t *testing.T) {
	credential, err := NewAgentCredential()
	if err != nil {
		t.Fatalf("create Agent credential: %v", err)
	}
	if !strings.HasPrefix(credential.Secret, agentCredentialPrefix) {
		t.Fatalf("credential prefix = %q", credential.Secret)
	}
	if got := DigestAgentCredential(credential.Secret); got != credential.Digest {
		t.Fatal("credential digest does not match its secret")
	}
	if got := DigestAgentCredential(credential.Secret + "changed"); got == credential.Digest {
		t.Fatal("changed credential retained the same digest")
	}
}

func TestValidateDraftTrimsContentAndRejectsMissingFields(t *testing.T) {
	understanding, nextStep, err := ValidateDraft("  Current evidence  ", "  Ask the owner  ")
	if err != nil {
		t.Fatalf("validate draft: %v", err)
	}
	if understanding != "Current evidence" || nextStep != "Ask the owner" {
		t.Fatalf("normalized draft = %q / %q", understanding, nextStep)
	}
	if _, _, err := ValidateDraft("", "Continue"); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("missing understanding error = %v", err)
	}
	if _, _, err := ValidateDraft("Known", strings.Repeat("n", MaxNextStepBytes+1)); !errors.Is(err, ErrInvalidDraft) {
		t.Fatalf("oversized next step error = %v", err)
	}
}
