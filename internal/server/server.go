package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
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

// NewAPI composes User and Machine routes without merging their principals.
func NewAPI(
	readiness Readiness,
	user *UserRoutes,
	machine *MachineRoutes,
) (*Server, error) {
	if user == nil || machine == nil {
		return nil, errors.New("User and Machine routes are required")
	}
	return newServer(readiness, user, machine), nil
}

func newServer(
	readiness Readiness,
	user *UserRoutes,
	machine *MachineRoutes,
) *Server {
	server := &Server{readiness: readiness}
	router := chi.NewRouter()
	router.Get("/healthz", server.health)
	router.Route("/v1", func(versionOne chi.Router) {
		user.mount(versionOne)
		versionOne.Route("/host", machine.mount)
	})
	// Fetch-Metadata and Origin checks protect browser mutations while CLI and
	// Machine requests, which carry neither browser header, remain supported.
	// noStoreV1 stays outside that protection so even early rejections and
	// unmatched credential-surface responses cannot be cached.
	server.handler = noStoreV1(http.NewCrossOriginProtection().Handler(router))
	return server
}

func noStoreV1(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1" || strings.HasPrefix(request.URL.Path, "/v1/") {
			response.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(response, request)
	})
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
