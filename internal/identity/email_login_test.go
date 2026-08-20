package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestCanonicalEmailTrimsAndLowercasesWithoutAliasRewriting(t *testing.T) {
	t.Parallel()

	canonical, err := CanonicalEmail("  Zane+Carry@Example.COM  ")
	if err != nil {
		t.Fatalf("canonicalize email: %v", err)
	}
	if canonical != "zane+carry@example.com" {
		t.Fatalf("canonical email = %q", canonical)
	}
	for _, invalid := range []string{"", "Zane <zane@example.com>", "not-an-email", "a@example.com\nBcc:x@example.com"} {
		if _, err := CanonicalEmail(invalid); err == nil {
			t.Fatalf("invalid email %q was accepted", invalid)
		}
	}
}

func TestEmailCodeIsStableSixDigitsAndBoundToChallenge(t *testing.T) {
	t.Parallel()

	credentials, err := NewCredentials(bytes.Repeat([]byte{9}, IdentityRootBytes))
	if err != nil {
		t.Fatalf("create Identity credentials: %v", err)
	}
	first, err := credentials.EmailCode("11111111-1111-4111-8111-111111111111", "person@example.com")
	if err != nil {
		t.Fatalf("create email code: %v", err)
	}
	replayed, _ := credentials.EmailCode("11111111-1111-4111-8111-111111111111", "person@example.com")
	other, _ := credentials.EmailCode("22222222-2222-4222-8222-222222222222", "person@example.com")
	if len(first) != EmailCodeDigits || first != replayed || first == other {
		t.Fatalf("email codes first=%q replayed=%q other=%q", first, replayed, other)
	}
	for _, character := range first {
		if character < '0' || character > '9' {
			t.Fatalf("email code contains %q", character)
		}
	}
}

func TestEmailLoginOwnsCanonicalizationDerivationAndSubmission(t *testing.T) {
	t.Parallel()
	credentials, err := NewCredentials(bytes.Repeat([]byte{4}, IdentityRootBytes))
	if err != nil {
		t.Fatalf("create credentials: %v", err)
	}
	persistence := &recordingEmailPersistence{prepared: EmailChallenge{
		ChallengeID: "11111111-1111-4111-8111-111111111111", CanonicalEmail: "person@example.com",
		ExpiresAt: time.Now().Add(EmailCodeLifetime), SubmissionState: EmailSubmissionPrepared, CanSubmit: true,
	}}
	submitter := &recordingEmailSubmitter{}
	login, err := NewEmailLogin(persistence, submitter, credentials)
	if err != nil {
		t.Fatalf("compose email login: %v", err)
	}
	_, err = login.RequestCode(context.Background(), RequestEmailCodeCommand{
		ChallengeID: persistence.prepared.ChallengeID, Email: " Person@Example.COM ", Source: "203.0.113.7",
		IdempotencyKey: "request-code",
	})
	if err != nil {
		t.Fatalf("request code: %v", err)
	}
	expectedRequestDigest := credentials.RequestDigest(
		"request-email-code", string(LoginPurpose), persistence.prepared.ChallengeID,
		"person@example.com", "", "",
	)
	if persistence.prepare.CanonicalEmail != "person@example.com" ||
		persistence.prepare.RequestDigest != expectedRequestDigest || submitter.calls != 1 ||
		submitter.message.Recipient != "person@example.com" || len(submitter.message.Code) != EmailCodeDigits ||
		submitter.message.IdempotencyKey != "carry-email-"+persistence.prepared.ChallengeID {
		t.Fatalf("prepare = %#v, message = %#v, calls = %d", persistence.prepare, submitter.message, submitter.calls)
	}
}

func TestEmailLoginUnknownReplayResubmitsExactPayloadWithinValidWindow(t *testing.T) {
	t.Parallel()
	credentials, _ := NewCredentials(bytes.Repeat([]byte{6}, IdentityRootBytes))
	persistence := &recordingEmailPersistence{prepared: EmailChallenge{
		ChallengeID: "33333333-3333-4333-8333-333333333333", CanonicalEmail: "person@example.com",
		ExpiresAt: time.Now().Add(time.Minute), SubmissionState: EmailSubmissionUnknown, CanSubmit: true,
	}}
	submitter := &recordingEmailSubmitter{submission: EmailSubmission{State: EmailSubmissionUnknown}}
	login, err := NewEmailLogin(persistence, submitter, credentials)
	if err != nil {
		t.Fatalf("compose email login: %v", err)
	}
	command := RequestEmailCodeCommand{
		ChallengeID: persistence.prepared.ChallengeID, Email: "person@example.com", Source: "203.0.113.9",
		IdempotencyKey: "unknown-replay",
	}
	for range 2 {
		if _, err := login.RequestCode(context.Background(), command); err != nil {
			t.Fatalf("replay unknown submission: %v", err)
		}
	}
	if submitter.calls != 2 || submitter.messages[0] != submitter.messages[1] ||
		submitter.expectedDigests[0] != submitter.expectedDigests[1] {
		t.Fatalf("messages = %#v, expected digests = %#v", submitter.messages, submitter.expectedDigests)
	}
}

func TestEmailLoginAcceptedAndRejectedSubmissionsAreTerminal(t *testing.T) {
	t.Parallel()
	credentials, _ := NewCredentials(bytes.Repeat([]byte{7}, IdentityRootBytes))
	for _, test := range []struct {
		name    string
		state   EmailSubmissionState
		wantErr error
	}{
		{name: "accepted", state: EmailSubmissionAccepted},
		{name: "rejected", state: EmailSubmissionRejected, wantErr: ErrEmailSubmissionRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			persistence := &recordingEmailPersistence{prepared: EmailChallenge{
				ChallengeID: "44444444-4444-4444-8444-444444444444", CanonicalEmail: "person@example.com",
				ExpiresAt: time.Now().Add(time.Minute), SubmissionState: test.state, CanSubmit: true,
			}}
			submitter := &recordingEmailSubmitter{}
			login, err := NewEmailLogin(persistence, submitter, credentials)
			if err != nil {
				t.Fatalf("compose email login: %v", err)
			}
			_, err = login.RequestCode(context.Background(), RequestEmailCodeCommand{
				ChallengeID: persistence.prepared.ChallengeID, Email: "person@example.com", Source: "203.0.113.10",
				IdempotencyKey: "terminal-" + test.name,
			})
			if !errors.Is(err, test.wantErr) || submitter.calls != 0 {
				t.Fatalf("terminal request error = %v, submit calls = %d", err, submitter.calls)
			}
		})
	}
}

func TestEmailMethodProofsFixPurposeTargetAndReauthenticationAddress(t *testing.T) {
	t.Parallel()
	credentials, _ := NewCredentials(bytes.Repeat([]byte{8}, IdentityRootBytes))
	userID := "11111111-1111-4111-8111-111111111111"
	sessionID := "22222222-2222-4222-8222-222222222222"

	reauthentication := &recordingEmailPersistence{prepared: EmailChallenge{
		ChallengeID: "33333333-3333-4333-8333-333333333333", CanonicalEmail: "linked@example.com",
		ExpiresAt: time.Now().Add(time.Minute), SubmissionState: EmailSubmissionPrepared, CanSubmit: true,
	}}
	login, err := NewEmailLogin(reauthentication, &recordingEmailSubmitter{}, credentials)
	if err != nil {
		t.Fatalf("compose reauthentication: %v", err)
	}
	_, err = login.RequestReauthenticationCode(context.Background(), RequestEmailMethodCodeCommand{
		ChallengeID: reauthentication.prepared.ChallengeID, Email: "attacker@example.com", Source: "203.0.113.20",
		IdempotencyKey: "reauthenticate-email", UserID: userID, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("request reauthentication code: %v", err)
	}
	if reauthentication.prepare.CanonicalEmail != "person@example.com" {
		t.Fatalf("reauthentication candidate = %#v", reauthentication.prepare)
	}
	if reauthentication.prepare.Purpose != ReauthenticatePurpose || reauthentication.prepare.TargetUserID != userID ||
		reauthentication.prepare.InitiatingSessionID != sessionID {
		t.Fatalf("reauthentication command = %#v", reauthentication.prepare)
	}

	link := &recordingEmailPersistence{prepared: EmailChallenge{
		ChallengeID: "44444444-4444-4444-8444-444444444444", CanonicalEmail: "candidate@example.com",
		ExpiresAt: time.Now().Add(time.Minute), SubmissionState: EmailSubmissionPrepared, CanSubmit: true,
	}}
	login, _ = NewEmailLogin(link, &recordingEmailSubmitter{}, credentials)
	_, err = login.RequestLinkCode(context.Background(), RequestEmailMethodCodeCommand{
		ChallengeID: link.prepared.ChallengeID, Email: " Candidate@Example.COM ", Source: "203.0.113.21",
		IdempotencyKey: "link-email", UserID: userID, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("request link code: %v", err)
	}
	if link.prepare.CanonicalEmail != "candidate@example.com" || link.prepare.Purpose != LinkPurpose ||
		link.prepare.TargetUserID != userID || link.prepare.InitiatingSessionID != sessionID {
		t.Fatalf("link command = %#v", link.prepare)
	}
}

func TestEmailLoginDoesNotSubmitExpiredPreparedReplay(t *testing.T) {
	t.Parallel()
	credentials, _ := NewCredentials(bytes.Repeat([]byte{5}, IdentityRootBytes))
	persistence := &recordingEmailPersistence{prepared: EmailChallenge{
		ChallengeID: "22222222-2222-4222-8222-222222222222", CanonicalEmail: "person@example.com",
		ExpiresAt: time.Now().Add(-time.Minute), SubmissionState: EmailSubmissionUnknown, CanSubmit: false,
	}}
	submitter := &recordingEmailSubmitter{}
	login, err := NewEmailLogin(persistence, submitter, credentials)
	if err != nil {
		t.Fatalf("compose email login: %v", err)
	}
	if _, err := login.RequestCode(context.Background(), RequestEmailCodeCommand{
		ChallengeID: persistence.prepared.ChallengeID, Email: "person@example.com", Source: "203.0.113.8",
		IdempotencyKey: "expired-replay",
	}); err != nil {
		t.Fatalf("request expired replay: %v", err)
	}
	if submitter.calls != 0 {
		t.Fatalf("expired replay submitted %d emails", submitter.calls)
	}
}

type recordingEmailPersistence struct {
	prepare  PrepareEmailChallengeCommand
	prepared EmailChallenge
}

func (persistence *recordingEmailPersistence) PrepareEmailChallenge(_ context.Context, command PrepareEmailChallengeCommand) (EmailChallenge, error) {
	persistence.prepare = command
	if persistence.prepared.PayloadDigest != ([sha256.Size]byte{}) && persistence.prepared.PayloadDigest != command.PayloadDigest {
		return EmailChallenge{}, ErrEmailPayloadChanged
	}
	persistence.prepared.PayloadDigest = command.PayloadDigest
	return persistence.prepared, nil
}

func (persistence *recordingEmailPersistence) RecordEmailSubmission(
	_ context.Context,
	_ string,
	payloadDigest [sha256.Size]byte,
	submission EmailSubmission,
) (EmailChallenge, error) {
	if persistence.prepared.PayloadDigest != payloadDigest {
		return EmailChallenge{}, ErrEmailPayloadChanged
	}
	persistence.prepared.SubmissionState = submission.State
	persistence.prepared.ProviderMessageID = submission.ProviderMessageID
	return persistence.prepared, nil
}

func (*recordingEmailPersistence) VerifyEmailChallenge(context.Context, VerifyEmailChallengeCommand) (BrowserSession, error) {
	return BrowserSession{}, nil
}

func (*recordingEmailPersistence) EmailForReauthentication(context.Context, string, string) (string, error) {
	return "person@example.com", nil
}

type recordingEmailSubmitter struct {
	calls           int
	message         EmailCodeMessage
	messages        []EmailCodeMessage
	expectedDigests [][sha256.Size]byte
	submission      EmailSubmission
}

func (*recordingEmailSubmitter) PayloadDigest(message EmailCodeMessage) ([sha256.Size]byte, error) {
	return sha256.Sum256([]byte(message.Recipient + "\x00" + message.Code + "\x00" + message.IdempotencyKey)), nil
}

func (submitter *recordingEmailSubmitter) SubmitEmailCode(
	_ context.Context,
	message EmailCodeMessage,
	expectedDigest [sha256.Size]byte,
) EmailSubmission {
	submitter.calls++
	submitter.message = message
	submitter.messages = append(submitter.messages, message)
	submitter.expectedDigests = append(submitter.expectedDigests, expectedDigest)
	if submitter.submission.State != "" {
		return submitter.submission
	}
	return EmailSubmission{State: EmailSubmissionAccepted, ProviderMessageID: "resend-message"}
}
