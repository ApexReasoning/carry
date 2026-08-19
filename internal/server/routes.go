package server

import (
	"errors"

	"github.com/ApexReasoning/carry/internal/host"
	"github.com/go-chi/chi/v5"
)

// MemberRoutes composes only routes authorized by a member token or opaque
// browser session, plus the one-way token-to-session exchange.
type MemberRoutes struct {
	auth     memberAuthenticator
	sessions browserSessionAPI
	member   memberAPI
	machines memberMachineAPI
	works    workAPI
}

// NewMemberRoutes constructs the member principal surface from narrow
// consumer-owned contracts. A concrete Store may implement several contracts
// without turning them into one server-wide interface.
func NewMemberRoutes(
	tokens UserTokenAuthenticator,
	sessions BrowserSessionStore,
	memberships MembershipReader,
	machines MachineEnrollmentStore,
	workCommands WorkCommands,
	workQueries WorkQueries,
	authority *host.CertificateAuthority,
) (*MemberRoutes, error) {
	if tokens == nil || sessions == nil || memberships == nil || machines == nil ||
		workCommands == nil || workQueries == nil || authority == nil {
		return nil, errors.New("member route dependencies are required")
	}
	return &MemberRoutes{
		auth:     memberAuthenticator{tokens: tokens, sessions: sessions},
		sessions: browserSessionAPI{store: sessions},
		member:   memberAPI{memberships: memberships},
		machines: memberMachineAPI{store: machines, authority: authority},
		works:    workAPI{commands: workCommands, queries: workQueries},
	}, nil
}

func (routes *MemberRoutes) mount(router chi.Router) {
	router.Group(func(memberSurface chi.Router) {
		memberSurface.Use(rejectMachinePrincipal)
		memberSurface.Post("/browser/sessions", routes.sessions.create)
		memberSurface.Delete("/browser/sessions/current", routes.sessions.revokeCurrent)
		memberSurface.Group(func(member chi.Router) {
			member.Use(routes.auth.requireMember)
			member.Get("/me", routes.member.me)
			member.Post("/machines/enroll", routes.machines.enroll)
			member.Post("/machines/revoke", routes.machines.revoke)
			routes.works.mount(member)
		})
	})
}

// MachineRoutes composes only the mTLS-authenticated Host surface.
type MachineRoutes struct {
	machine machineAPI
}

// NewMachineRoutes constructs the Machine principal surface from independent runtime-report and Run-claim contracts.
func NewMachineRoutes(runtimeStore MachineRuntimeStore, runStore MachineRunStore) (*MachineRoutes, error) {
	if runtimeStore == nil || runStore == nil {
		return nil, errors.New("Machine route dependencies are required")
	}
	return &MachineRoutes{machine: machineAPI{runtimeStore: runtimeStore, runStore: runStore}}, nil
}

func (routes *MachineRoutes) mount(router chi.Router) {
	router.Group(func(machine chi.Router) {
		machine.Use(requireMachine)
		machine.Post("/runtime-report", routes.machine.reportRuntimes)
		machine.Get("/status", routes.machine.status)
		machine.Post("/runs/claim", routes.machine.claimRun)
		machine.Post("/runs/{run_id}/attempts/{attempt_id}/renew", routes.machine.renewRun)
	})
}

// AgentRoutes composes only the Attempt-scoped bearer surface.
type AgentRoutes struct {
	agent agentAPI
}

// NewAgentRoutes constructs the Agent principal surface independently of member and Machine auth.
func NewAgentRoutes(store AgentRunStore) (*AgentRoutes, error) {
	if store == nil {
		return nil, errors.New("Agent route dependency is required")
	}
	return &AgentRoutes{agent: agentAPI{store: store}}, nil
}

func (routes *AgentRoutes) mount(router chi.Router) {
	router.Group(func(agent chi.Router) {
		agent.Use(requireAgent)
		agent.Get("/runs/{run_id}/attempts/{attempt_id}/context", routes.agent.loadContext)
		agent.Post("/runs/{run_id}/attempts/{attempt_id}/revision", routes.agent.commitRevision)
		agent.Post("/runs/{run_id}/attempts/{attempt_id}/outcome", routes.agent.finishOutcome)
	})
}
