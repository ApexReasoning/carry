package host

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	hostdomain "github.com/ApexReasoning/carry/internal/host"
)

func TestParseServerURLRequiresHTTPSRoot(t *testing.T) {
	t.Parallel()

	if got, err := parseServerURL("https://carry.example.com/"); err != nil || got != "https://carry.example.com" {
		t.Fatalf("valid URL = %q, %v", got, err)
	}
	for _, invalid := range []string{
		"http://carry.example.com",
		"https://carry.example.com/v1",
		"https://user@carry.example.com",
	} {
		if _, err := parseServerURL(invalid); err == nil {
			t.Errorf("invalid URL %q accepted", invalid)
		}
	}
}

func TestMachineReportCannotSendMemberAuthorization(t *testing.T) {
	t.Parallel()

	connection := machineHTTP{
		serverURL: "https://carry.example.com",
		client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if authorization := request.Header.Get("Authorization"); authorization != "" {
				t.Fatalf("Machine request carried member authorization %q", authorization)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		})},
	}
	observations := []hostdomain.RuntimeObservation{
		{Kind: hostdomain.RuntimePi, Detection: hostdomain.RuntimeDetected},
		{Kind: hostdomain.RuntimeCodex, Detection: hostdomain.RuntimeNotFound},
	}
	if err := connection.reportRuntimes(context.Background(), observations); err != nil {
		t.Fatalf("report Runtimes: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
