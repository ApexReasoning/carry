//go:build integration

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	carrypostgres "github.com/ApexReasoning/carry/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	privateConversationQuestionText        = "What should I check before a customer renewal? Private reference ORCHID-QUESTION-739."
	privateConversationQuestionReply       = "Review the renewal date, notice window, owner, and approval dependencies first."
	privateConversationDelegationText      = "Carry, take responsibility for preparing the renewal brief. Private reference ORCHID-DELEGATION-842."
	privateConversationDelegationReply     = "I’ll keep the renewal brief moving as shared Work."
	privateConversationDelegationGoal      = "Prepare the renewal brief"
	privateConversationQuestionRequestID   = "11111111-1111-4111-8111-111111111111"
	privateConversationDelegationRequestID = "22222222-2222-4222-8222-222222222222"
)

func TestMemberTalksPrivatelyAndDelegatesSharedWork(t *testing.T) {
	databaseURL := os.Getenv("CARRY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("CARRY_TEST_DATABASE_URL is required")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	temporary := t.TempDir()
	carryServer := filepath.Join(temporary, "carry-server")
	carry := filepath.Join(temporary, "carry")
	build(t, root, carryServer, "./cmd/carry-server")
	build(t, root, carry, "./cmd/carry")

	binDirectory := filepath.Join(temporary, "bin")
	if err := os.Mkdir(binDirectory, 0o700); err != nil {
		t.Fatalf("create fake Agent bin directory: %v", err)
	}
	build(t, root, filepath.Join(binDirectory, "pi"), "./e2e/testdata/privateconversationpi")

	pkiDirectory := filepath.Join(temporary, "pki")
	run(t, root, nil, carryServer, "pki", "init", "--dir", pkiDirectory, "--hosts", "localhost,127.0.0.1")
	testMemberOutput := prepareTestMember(t, databaseURL)
	resetProductJourneyFacts(t, databaseURL)
	var member struct {
		UserID  string `json:"user_id"`
		SpaceID string `json:"space_id"`
	}
	if err := json.Unmarshal([]byte(testMemberOutput), &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	attachTestEmailIdentity(t, databaseURL, member.UserID, "conversation@example.com")

	serverAddress := freeAddress(t)
	stopServer, serverLog, emailCaptureFile := startServer(t, root, carryServer, serverAddress, databaseURL, pkiDirectory)
	defer stopServer()
	serverURL := "https://" + serverAddress
	waitForServer(t, serverURL, filepath.Join(pkiDirectory, "ca.pem"), serverLog)

	configDirectory := filepath.Join(temporary, "config")
	clientEnvironment := []string{"CARRY_CONFIG_DIR=" + configDirectory}
	loginCarryCLI(t, root, carry, databaseURL, serverURL, filepath.Join(pkiDirectory, "ca.pem"),
		configDirectory, member.UserID, member.SpaceID, "private-conversation-cli")
	run(t, root, clientEnvironment, carry, "host", "enroll",
		"--space", member.SpaceID, "--name", "private-conversation-host",
	)

	hostCtx, cancelHost := context.WithCancel(t.Context())
	hostLog := &lockedBuffer{}
	hostCommand := exec.CommandContext(hostCtx, carry, "host", "start")
	hostCommand.Dir = root
	hostCommand.Env = append(os.Environ(), clientEnvironment...)
	hostCommand.Env = append(hostCommand.Env, "PATH="+binDirectory)
	hostCommand.Stdout = hostLog
	hostCommand.Stderr = hostLog
	if err := hostCommand.Start(); err != nil {
		cancelHost()
		t.Fatalf("start Carry Host: %v", err)
	}
	defer func() {
		cancelHost()
		if err := hostCommand.Wait(); err != nil && hostCtx.Err() == nil {
			t.Errorf("wait for Carry Host: %v\n%s", err, hostLog.String())
		}
	}()
	waitForHostStart(t, hostLog)

	run(t, root, nil, "pnpm", "--dir", "apps/web", "build")
	webAddress := freeAddress(t)
	stopWeb, webLog := startWeb(t, root, webAddress, serverURL, pkiDirectory)
	defer stopWeb()
	webURL := "https://" + webAddress
	waitForServer(t, webURL, filepath.Join(pkiDirectory, "ca.pem"), webLog)

	playwrightOutput, err := runError(
		root,
		[]string{
			"CARRY_WEB_URL=" + webURL,
			"CARRY_EMAIL_CAPTURE_FILE=" + emailCaptureFile,
		},
		"pnpm", "--dir", "apps/web", "exec", "playwright", "test", "e2e/private-conversation.spec.ts",
	)
	if err != nil {
		t.Fatalf("run private Conversation browser journey: %v\n%s\nHost log:\n%s", err, playwrightOutput, hostLog.String())
	}
	if !strings.Contains(playwrightOutput, "1 passed") {
		t.Fatalf("private Conversation Playwright spec did not execute:\n%s", playwrightOutput)
	}
	t.Logf("public Host: %s", strings.SplitN(hostLog.String(), "\n", 2)[0])
	t.Logf("private Conversation Playwright: %s", strings.TrimSpace(playwrightOutput))

	pool, err := carrypostgres.Open(t.Context(), databaseURL)
	if err != nil {
		t.Fatalf("open private Conversation evidence database: %v", err)
	}
	defer pool.Close()
	assertPrivateConversationEvidence(t, pool, member.SpaceID, member.UserID)
}

func waitForHostStart(t *testing.T, hostLog *lockedBuffer) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(hostLog.String(), "Started Carry Host") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Carry Host did not start before deadline\n%s", hostLog.String())
}

// assertPrivateConversationEvidence reads only committed PostgreSQL facts after
// the public browser/Host journey. It does not participate in product writes.
func assertPrivateConversationEvidence(t *testing.T, pool *pgxpool.Pool, spaceID string, userID string) {
	t.Helper()
	ctx := t.Context()

	rows, err := pool.Query(ctx, `
		select message.message_seq, message.author, message.text,
		       coalesce(message.member_request_id, ''),
		       coalesce(message.reply_to_member_message_id::text, '')
		from conversations as conversation
		join conversation_messages as message
		  on message.conversation_id = conversation.conversation_id
		where conversation.space_id = $1 and conversation.member_user_id = $2
		order by message.message_seq
	`, spaceID, userID)
	if err != nil {
		t.Fatalf("load private journey messages: %v", err)
	}
	defer rows.Close()
	type storedMessage struct {
		sequence  int64
		author    string
		text      string
		requestID string
		replyTo   string
	}
	var messages []storedMessage
	for rows.Next() {
		var message storedMessage
		if err := rows.Scan(&message.sequence, &message.author, &message.text, &message.requestID, &message.replyTo); err != nil {
			t.Fatalf("scan private journey message: %v", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate private journey messages: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("private journey messages = %d, want two member messages and two Carry replies: %+v", len(messages), messages)
	}
	expected := []struct {
		author    string
		text      string
		requestID string
	}{
		{author: "member", text: privateConversationQuestionText, requestID: privateConversationQuestionRequestID},
		{author: "carry", text: privateConversationQuestionReply},
		{author: "member", text: privateConversationDelegationText, requestID: privateConversationDelegationRequestID},
		{author: "carry", text: privateConversationDelegationReply},
	}
	for index, want := range expected {
		got := messages[index]
		if got.sequence != int64(index+1) || got.author != want.author || got.text != want.text || got.requestID != want.requestID {
			t.Fatalf("private message %d = %+v, want %+v", index+1, got, want)
		}
		if want.author == "carry" && got.replyTo == "" {
			t.Fatalf("Carry reply %d has no source member message", index+1)
		}
	}

	var committedClaims int
	var ordinaryWorkID string
	var delegatedWorkID string
	if err := pool.QueryRow(ctx, `
		select count(*) filter (where claim.committed_reply_message_id is not null),
		       coalesce(max(claim.created_work_id::text) filter (
		           where source.member_request_id = $3
		       ), ''),
		       coalesce(max(claim.created_work_id::text) filter (
		           where source.member_request_id = $4
		       ), '')
		from conversations as conversation
		join conversation_messages as source
		  on source.conversation_id = conversation.conversation_id
		join conversation_reply_claims as claim
		  on claim.source_message_id = source.message_id
		where conversation.space_id = $1
		  and conversation.member_user_id = $2
		  and source.member_request_id in ($3, $4)
	`, spaceID, userID, privateConversationQuestionRequestID, privateConversationDelegationRequestID).Scan(
		&committedClaims, &ordinaryWorkID, &delegatedWorkID,
	); err != nil {
		t.Fatalf("load private reply consequences: %v", err)
	}
	if committedClaims != 2 || ordinaryWorkID != "" || delegatedWorkID == "" {
		t.Fatalf("private reply consequences = committed %d ordinary Work %q delegated Work %q", committedClaims, ordinaryWorkID, delegatedWorkID)
	}

	var goal string
	var ownerUserID string
	var creatorUserID string
	var understanding string
	var nextStep string
	if err := pool.QueryRow(ctx, `
		select goal, owner_user_id, creator_user_id,
		       coalesce(understanding, ''), coalesce(next_step, '')
		from works
		where work_id = $1
	`, delegatedWorkID).Scan(&goal, &ownerUserID, &creatorUserID, &understanding, &nextStep); err != nil {
		t.Fatalf("load delegated Work: %v", err)
	}
	if goal != privateConversationDelegationGoal || ownerUserID != userID || creatorUserID != userID {
		t.Fatalf("delegated Work = goal %q owner %s creator %s", goal, ownerUserID, creatorUserID)
	}
	if understanding != "Carry owns the renewal brief." || nextStep != "Draft the renewal brief." {
		t.Fatalf("delegated Work native update = understanding %q next step %q", understanding, nextStep)
	}

	var matchingGoals int
	if err := pool.QueryRow(ctx, `
		select count(*) from works where space_id = $1 and goal = $2
	`, spaceID, privateConversationDelegationGoal).Scan(&matchingGoals); err != nil {
		t.Fatalf("count exact delegated Works: %v", err)
	}
	if matchingGoals != 1 {
		t.Fatalf("exact delegated Work count = %d, want 1", matchingGoals)
	}
	var workMessages int
	if err := pool.QueryRow(ctx, `
		select count(*) from work_messages where work_id = $1
	`, delegatedWorkID).Scan(&workMessages); err != nil {
		t.Fatalf("count delegated Work messages: %v", err)
	}
	if workMessages != 0 {
		t.Fatalf("delegated Work messages = %d, want 0 private source copies", workMessages)
	}
	var runCount int
	var runStates string
	if err := pool.QueryRow(ctx, `
		select count(*), coalesce(string_agg(state, ',' order by created_at), '')
		from runs where work_id = $1
	`, delegatedWorkID).Scan(&runCount, &runStates); err != nil {
		t.Fatalf("load delegated Work Runs: %v", err)
	}
	if runCount != 1 || runStates != "succeeded" {
		t.Fatalf("delegated Work Runs = %d %q, want one succeeded native Run", runCount, runStates)
	}

	combinedWorkText := strings.Join([]string{goal, understanding, nextStep}, "\n")
	for _, privateValue := range []string{
		privateConversationQuestionText,
		privateConversationDelegationText,
		"ORCHID-QUESTION-739",
		"ORCHID-DELEGATION-842",
	} {
		if strings.Contains(combinedWorkText, privateValue) {
			t.Fatalf("delegated Work leaked private source %q", privateValue)
		}
	}
	var forbiddenColumns string
	if err := pool.QueryRow(ctx, `
		select coalesce(string_agg(table_name || '.' || column_name, ',' order by table_name, column_name), '')
		from information_schema.columns
		where table_schema = 'public'
		  and table_name in ('works', 'work_messages', 'runs')
		  and (
		    column_name like '%conversation%'
		    or column_name like '%source_message%'
		    or column_name like '%private%'
		  )
	`).Scan(&forbiddenColumns); err != nil {
		t.Fatalf("inspect shared Work/Run source columns: %v", err)
	}
	if forbiddenColumns != "" {
		t.Fatalf("shared Work/Run tables expose private source relation columns: %s", forbiddenColumns)
	}

	t.Logf(
		"observed four ordered private messages, two committed claims, ordinary no-Work, delegated Work %s, and one native succeeded Run",
		delegatedWorkID,
	)
}
