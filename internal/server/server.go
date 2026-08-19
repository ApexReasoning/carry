package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// Readiness reports whether the server's required persistence is available.
type Readiness interface {
	Ping(context.Context) error
}

// Server serves Carry's explicitly composed HTTP route trees.
type Server struct {
	handler   http.Handler
	readiness Readiness
}

// NewAPI composes the member, Machine, and Agent route trees without merging
// their authority or persistence contracts.
func NewAPI(
	readiness Readiness,
	member *MemberRoutes,
	machine *MachineRoutes,
	agent *AgentRoutes,
) (*Server, error) {
	if member == nil || machine == nil || agent == nil {
		return nil, errors.New("member, Machine, and Agent routes are required")
	}
	return newServer(readiness, member, machine, agent), nil
}

func newServer(
	readiness Readiness,
	member *MemberRoutes,
	machine *MachineRoutes,
	agent *AgentRoutes,
) *Server {
	server := &Server{readiness: readiness}
	router := chi.NewRouter()
	router.Get("/healthz", server.health)
	router.Route("/v1", func(versionOne chi.Router) {
		member.mount(versionOne)
		versionOne.Route("/host", machine.mount)
		versionOne.Route("/agent", agent.mount)
	})
	// Fetch-Metadata and Origin checks protect browser mutations while CLI and
	// Machine requests, which carry neither browser header, remain supported.
	server.handler = http.NewCrossOriginProtection().Handler(router)
	return server
}

// Handler returns the fully composed HTTP surface.
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) health(response http.ResponseWriter, request *http.Request) {
	status := "ready"
	statusCode := http.StatusOK
	if s.readiness != nil {
		ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
		defer cancel()
		if err := s.readiness.Ping(ctx); err != nil {
			status = "unavailable"
			statusCode = http.StatusServiceUnavailable
		}
	}

	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(struct {
		Status string `json:"status"`
	}{Status: status})
}
