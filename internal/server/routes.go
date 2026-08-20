package server

import (
	"errors"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/go-chi/chi/v5"
)

// UserAuthentication holds bearer and Browser Session authentication middleware for User routes.
type UserAuthentication struct {
	authenticator userAuthenticator
}

func NewUserAuthentication(
	tokens UserTokenAuthenticator,
	sessions BrowserSessions,
	credentials identity.Credentials,
) (*UserAuthentication, error) {
	if tokens == nil || sessions == nil {
		return nil, errors.New("User authentication dependencies are required")
	}
	return &UserAuthentication{authenticator: userAuthenticator{
		tokens: tokens, sessions: sessions, credentials: credentials,
	}}, nil
}

// UserIdentityRoutes owns login, Browser Session, and current-User HTTP routes.
type UserIdentityRoutes struct {
	email    emailLoginAPI
	external externalLoginAPI
	sessions browserSessionAPI
	user     userAPI
}

func NewUserIdentityRoutes(
	emailLogin EmailLogin,
	externalLogin ExternalLogin,
	sessions BrowserSessions,
	credentials identity.Credentials,
	externalOrigin ExternalOrigin,
	requestSources RequestSource,
	memberships MembershipReader,
) (*UserIdentityRoutes, error) {
	if emailLogin == nil || externalLogin == nil || sessions == nil || memberships == nil || externalOrigin.value == "" {
		return nil, errors.New("User identity route dependencies are required")
	}
	return &UserIdentityRoutes{
		email: emailLoginAPI{
			login: emailLogin, credentials: credentials, requestSources: requestSources,
		},
		external: externalLoginAPI{
			login: externalLogin, sessions: sessions, credentials: credentials, origin: externalOrigin,
		},
		sessions: browserSessionAPI{sessions: sessions, credentials: credentials},
		user:     userAPI{memberships: memberships},
	}, nil
}

func (routes *UserIdentityRoutes) mountPublic(router chi.Router) {
	router.Post("/auth/email/challenges", routes.email.requestCode)
	router.Post("/auth/email/challenges/{challenge_id}/verify", routes.email.verifyCode)
	router.Post("/auth/google/start", routes.external.startGoogle)
	router.Get("/auth/google/callback", routes.external.callbackGoogle)
	router.Post("/auth/github/start", routes.external.startGitHub)
	router.Get("/auth/github/callback", routes.external.callbackGitHub)
	router.Delete("/browser/sessions/current", routes.sessions.revokeCurrent)
}

func (routes *UserIdentityRoutes) mountUser(router chi.Router) {
	router.Get("/me", routes.user.me)
}

// UserSpaceRoutes owns the Browser-only first-Space HTTP route.
type UserSpaceRoutes struct {
	spaces spaceCreationAPI
}

func NewUserSpaceRoutes(firstSpace FirstSpace) (*UserSpaceRoutes, error) {
	if firstSpace == nil {
		return nil, errors.New("User Space route dependencies are required")
	}
	return &UserSpaceRoutes{spaces: spaceCreationAPI{creator: firstSpace}}, nil
}

func (routes *UserSpaceRoutes) mount(router chi.Router) {
	router.Post("/spaces", routes.spaces.createFirst)
}

// UserMachineRoutes owns User-authorized Machine enrollment and revocation routes.
type UserMachineRoutes struct {
	machines userMachineAPI
}

func NewUserMachineRoutes(
	enrollment MachineEnrollment,
	revocation MachineRevocation,
) (*UserMachineRoutes, error) {
	if enrollment == nil || revocation == nil {
		return nil, errors.New("User Machine route dependencies are required")
	}
	return &UserMachineRoutes{machines: userMachineAPI{
		enrollment: enrollment, revocation: revocation,
	}}, nil
}

func (routes *UserMachineRoutes) mount(router chi.Router) {
	router.Post("/machines/enroll", routes.machines.enroll)
	router.Post("/machines/revoke", routes.machines.revoke)
}

// ConversationRoutes owns the User-authenticated Conversation HTTP routes.
type ConversationRoutes struct {
	conversations conversationAPI
}

func NewConversationRoutes(
	commands ConversationCommands,
	queries ConversationQueries,
) (*ConversationRoutes, error) {
	if commands == nil || queries == nil {
		return nil, errors.New("Conversation route dependencies are required")
	}
	return &ConversationRoutes{conversations: conversationAPI{commands: commands, queries: queries}}, nil
}

func (routes *ConversationRoutes) mount(router chi.Router) {
	routes.conversations.mount(router)
}

// WorkRoutes owns the User-authenticated Work HTTP routes.
type WorkRoutes struct {
	works workAPI
}

func NewWorkRoutes(commands WorkCommands, queries WorkQueries) (*WorkRoutes, error) {
	if commands == nil || queries == nil {
		return nil, errors.New("Work route dependencies are required")
	}
	return &WorkRoutes{works: workAPI{commands: commands, queries: queries}}, nil
}

func (routes *WorkRoutes) mount(router chi.Router) {
	routes.works.mount(router)
}

// UserRoutes composes the User API without merging User and Machine principals.
type UserRoutes struct {
	authentication *UserAuthentication
	identity       *UserIdentityRoutes
	spaces         *UserSpaceRoutes
	machines       *UserMachineRoutes
	conversations  *ConversationRoutes
	works          *WorkRoutes
}

func NewUserRoutes(
	authentication *UserAuthentication,
	identityRoutes *UserIdentityRoutes,
	spaceRoutes *UserSpaceRoutes,
	machineRoutes *UserMachineRoutes,
	conversationRoutes *ConversationRoutes,
	workRoutes *WorkRoutes,
) (*UserRoutes, error) {
	if authentication == nil || identityRoutes == nil || spaceRoutes == nil || machineRoutes == nil ||
		conversationRoutes == nil || workRoutes == nil {
		return nil, errors.New("User route groups are required")
	}
	return &UserRoutes{
		authentication: authentication,
		identity:       identityRoutes,
		spaces:         spaceRoutes,
		machines:       machineRoutes,
		conversations:  conversationRoutes,
		works:          workRoutes,
	}, nil
}

func (routes *UserRoutes) mount(router chi.Router) {
	router.Group(func(userSurface chi.Router) {
		userSurface.Use(rejectMachinePrincipal)
		routes.identity.mountPublic(userSurface)
		userSurface.Group(func(browser chi.Router) {
			browser.Use(routes.authentication.authenticator.requireBrowserUser)
			routes.spaces.mount(browser)
		})
		userSurface.Group(func(user chi.Router) {
			user.Use(routes.authentication.authenticator.requireUser)
			routes.identity.mountUser(user)
			routes.machines.mount(user)
			routes.conversations.mount(user)
			routes.works.mount(user)
		})
	})
}

// MachineRoutes composes only the mTLS-authenticated Host surface.
type MachineRoutes struct {
	runs          machineAPI
	conversations machineConversationAPI
}

func NewMachineRoutes(runs MachineRuns, conversations MachineConversations) (*MachineRoutes, error) {
	if runs == nil || conversations == nil {
		return nil, errors.New("Machine route dependencies are required")
	}
	return &MachineRoutes{
		runs:          machineAPI{runs: runs},
		conversations: machineConversationAPI{conversations: conversations},
	}, nil
}

func (routes *MachineRoutes) mount(router chi.Router) {
	router.Group(func(machine chi.Router) {
		machine.Use(requireMachine)
		machine.Post("/runs/claim", routes.runs.claimRun)
		machine.Post("/runs/{run_id}/attempts/{attempt_id}/renew", routes.runs.renewRun)
		machine.Post("/runs/{run_id}/attempts/{attempt_id}/understanding", routes.runs.commitUnderstanding)
		machine.Post("/runs/{run_id}/attempts/{attempt_id}/outcome", routes.runs.finishAttempt)
		routes.conversations.mount(machine)
	})
}
