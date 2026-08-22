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
	cli CLICredentialAuthenticator,
	sessions BrowserSessions,
	credentials identity.Credentials,
	origin ExternalOrigin,
) (*UserAuthentication, error) {
	if cli == nil || sessions == nil || origin.value == "" {
		return nil, errors.New("User authentication dependencies are required")
	}
	return &UserAuthentication{authenticator: userAuthenticator{
		cli: cli, sessions: sessions, credentials: credentials, origin: origin,
	}}, nil
}

// UserIdentityRoutes owns login, Browser Session, CLI access, and current-User HTTP routes.
type UserIdentityRoutes struct {
	email    emailLoginAPI
	external externalLoginAPI
	methods  identityMethodsAPI
	sessions browserSessionAPI
	cli      cliLoginAPI
	user     userAPI
}

func NewUserIdentityRoutes(
	emailLogin EmailLogin,
	externalLogin ExternalLogin,
	methods IdentityMethods,
	sessions BrowserSessions,
	cliLogins CLILogins,
	credentials identity.Credentials,
	externalOrigin ExternalOrigin,
	requestSources RequestSource,
	memberships MembershipReader,
) (*UserIdentityRoutes, error) {
	if emailLogin == nil || externalLogin == nil || methods == nil || sessions == nil || cliLogins == nil ||
		memberships == nil || externalOrigin.value == "" {
		return nil, errors.New("User Identity route dependencies are required")
	}
	return &UserIdentityRoutes{
		email: emailLoginAPI{
			login: emailLogin, credentials: credentials, requestSources: requestSources, origin: externalOrigin,
		},
		external: externalLoginAPI{
			login:          externalLogin,
			sessions:       sessions,
			credentials:    credentials,
			origin:         externalOrigin,
			requestSources: requestSources,
		},
		methods:  identityMethodsAPI{methods: methods, credentials: credentials, origin: externalOrigin},
		sessions: browserSessionAPI{sessions: sessions, credentials: credentials},
		cli: cliLoginAPI{
			logins: cliLogins, credentials: credentials, origin: externalOrigin, requestSources: requestSources,
		},
		user: userAPI{memberships: memberships},
	}, nil
}

func (routes *UserIdentityRoutes) mountPublic(router chi.Router) {
	router.Post("/auth/email/challenges", routes.email.requestCode)
	router.Post("/auth/email/challenges/{challenge_id}/verify", routes.email.verifyCode)
	router.Post("/auth/google/start", routes.external.startGoogle)
	router.Get("/auth/google/callback", routes.external.callbackGoogle)
	router.Post("/auth/github/start", routes.external.startGitHub)
	router.Get("/auth/github/callback", routes.external.callbackGitHub)
	router.Post("/identity/reauthentication/email/challenges/{challenge_id}/verify", routes.email.verifyReauthenticationCode)
	router.Post("/identity/methods/email/challenges/{challenge_id}/verify", routes.email.verifyLinkCode)
	router.Delete("/identity/methods/{method}", routes.methods.unlink)
	router.Delete("/browser/sessions/current", routes.sessions.revokeCurrent)
	router.Post("/cli-logins", routes.cli.begin)
	router.Post("/cli-logins/poll", routes.cli.poll)
	router.Post("/cli-logins/cancel", routes.cli.cancel)
	router.Post("/cli-credentials/current/revoke", routes.cli.revokeCurrent)
}

func (routes *UserIdentityRoutes) mountBrowser(router chi.Router) {
	router.Get("/identity/methods", routes.methods.list)
	router.Post("/identity/reauthentication/email/challenges", routes.email.requestReauthenticationCode)
	router.Post("/identity/reauthentication/google/start", routes.external.startGoogleReauthentication)
	router.Post("/identity/reauthentication/github/start", routes.external.startGitHubReauthentication)
	router.Post("/identity/methods/email/challenges", routes.email.requestLinkCode)
	router.Post("/identity/methods/google/start", routes.external.startGoogleLink)
	router.Post("/identity/methods/github/start", routes.external.startGitHubLink)
	router.Post("/cli-logins/lookup", routes.cli.lookup)
	router.Post("/cli-logins/approve", routes.cli.approve)
	router.Post("/cli-logins/deny", routes.cli.deny)
	router.Get("/identity/cli-credentials", routes.cli.listCredentials)
	router.Post("/identity/cli-credentials/{credential_id}/revoke", routes.cli.revokeFromBrowser)
}

func (routes *UserIdentityRoutes) mountUser(router chi.Router) {
	router.Get("/me", routes.user.me)
}

// UserSpaceRoutes owns Browser-authenticated Space and Membership routes.
type UserSpaceRoutes struct {
	spaces      spaceCreationAPI
	invitations *spaceInvitationAPI
}

func NewUserSpaceRoutes(creation SpaceCreation) (*UserSpaceRoutes, error) {
	if creation == nil {
		return nil, errors.New("User Space route dependencies are required")
	}
	return &UserSpaceRoutes{spaces: spaceCreationAPI{creation: creation}}, nil
}

func NewUserSpaceRoutesWithInvitations(
	creation SpaceCreation,
	invitations SpaceInvitationCommands,
	invitationQueries SpaceInvitationQueries,
	members SpaceMembers,
	credentials identity.Credentials,
	origin ExternalOrigin,
) (*UserSpaceRoutes, error) {
	routes, err := NewUserSpaceRoutes(creation)
	if err != nil || invitations == nil || invitationQueries == nil || members == nil || origin.value == "" {
		return nil, errors.New("User Space member route dependencies are required")
	}
	api := &spaceInvitationAPI{
		invitations:       invitations,
		invitationQueries: invitationQueries,
		members:           members,
		credentials:       credentials,
		origin:            origin,
	}
	routes.invitations = api
	return routes, nil
}

func (routes *UserSpaceRoutes) mount(router chi.Router) {
	router.Post("/spaces", routes.spaces.create)
	if routes.invitations == nil {
		return
	}
	router.Get("/invitations", routes.invitations.inbox)
	router.Post("/invitations/{invitation_id}/accept", routes.invitations.accept)
	router.Get("/invitations/{invitation_id}", routes.invitations.targeted)
	router.Get("/spaces/{space_id}/members", routes.invitations.listMembers)
	router.Post("/spaces/{space_id}/members/{user_id}/remove", routes.invitations.removeMember)
	router.Get("/spaces/{space_id}/invitations", routes.invitations.listManaged)
	router.Post("/spaces/{space_id}/invitations", routes.invitations.issue)
	router.Post("/spaces/{space_id}/invitations/{invitation_id}/resend", routes.invitations.resend)
	router.Post("/spaces/{space_id}/invitations/{invitation_id}/revoke", routes.invitations.revoke)
}

// UserMachineRoutes owns the public terminal ceremony and Browser-only
// Machine approval, inventory, and remote revocation surface.
type UserMachineRoutes struct {
	machines machineConnectionAPI
}

func NewUserMachineRoutes(
	connections MachineConnections,
	credentials identity.Credentials,
	origin ExternalOrigin,
	requestSources RequestSource,
) (*UserMachineRoutes, error) {
	if connections == nil || origin.value == "" {
		return nil, errors.New("User Machine route dependencies are required")
	}
	return &UserMachineRoutes{machines: machineConnectionAPI{
		connections: connections, credentials: credentials, origin: origin, requestSources: requestSources,
	}}, nil
}

func (routes *UserMachineRoutes) mountPublic(router chi.Router) {
	router.Post("/machine-connections", routes.machines.begin)
	router.Post("/machine-connections/status", routes.machines.poll)
	router.Post("/machine-connections/cancel", routes.machines.cancel)
}

func (routes *UserMachineRoutes) mountBrowser(router chi.Router) {
	router.Post("/machine-connections/lookup", routes.machines.lookup)
	router.Post("/machine-connections/{request_id}/approve", routes.machines.approve)
	router.Post("/machine-connections/{request_id}/deny", routes.machines.deny)
	router.Get("/spaces/{space_id}/machines", routes.machines.list)
	router.Post("/spaces/{space_id}/machines/{machine_id}/revoke", routes.machines.revokeFromBrowser)
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
		routes.machines.mountPublic(userSurface)
		userSurface.Group(func(browser chi.Router) {
			browser.Use(routes.authentication.authenticator.requireBrowserUser)
			routes.identity.mountBrowser(browser)
			routes.spaces.mount(browser)
			routes.machines.mountBrowser(browser)
		})
		userSurface.Group(func(user chi.Router) {
			user.Use(routes.authentication.authenticator.requireUser)
			routes.identity.mountUser(user)
			routes.conversations.mount(user)
			routes.works.mount(user)
		})
	})
}

// MachineRoutes composes only the mTLS-authenticated Host surface.
type MachineRoutes struct {
	runs          machineAPI
	conversations machineConversationAPI
	connections   machineConnectionAPI
}

func NewMachineRoutes(runs MachineRuns, conversations MachineConversations, connections MachineConnections) (*MachineRoutes, error) {
	if runs == nil || conversations == nil || connections == nil {
		return nil, errors.New("Machine route dependencies are required")
	}
	return &MachineRoutes{
		runs:          machineAPI{runs: runs},
		conversations: machineConversationAPI{conversations: conversations},
		connections:   machineConnectionAPI{connections: connections},
	}, nil
}

func (routes *MachineRoutes) mount(router chi.Router) {
	router.Group(func(machine chi.Router) {
		machine.Use(requireMachine)
		machine.Post("/machine/revoke", routes.connections.revokeFromHost)
		machine.Post("/runs/claim", routes.runs.claimRun)
		machine.Post("/runs/{run_id}/attempts/{attempt_id}/renew", routes.runs.renewRun)
		machine.Post("/runs/{run_id}/attempts/{attempt_id}/understanding", routes.runs.commitUnderstanding)
		machine.Post("/runs/{run_id}/attempts/{attempt_id}/outcome", routes.runs.finishAttempt)
		routes.conversations.mount(machine)
	})
}
