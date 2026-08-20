package reference

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLookupUsesFixedEscapedGETAndReturnsUTF8Text(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", request.Method)
		}
		if request.URL.EscapedPath() != "/v1/references/a%2Fb%3Fc=d" {
			t.Fatalf("escaped path = %q", request.URL.EscapedPath())
		}
		if request.Header.Get("Authorization") != "" || request.Header.Get("Content-Type") != "" {
			t.Fatalf("credential-bearing headers = %#v", request.Header)
		}
		_, _ = io.WriteString(response, "Reference text: 中文")
	}))
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := client.Lookup(context.Background(), "a/b?c=d")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if value != "Reference text: 中文" {
		t.Fatalf("value = %q", value)
	}
}

func TestNewRejectsUntrustedBaseURLs(t *testing.T) {
	for _, baseURL := range []string{
		"http://example.com",
		"https://user@example.com",
		"https://example.com/catalog",
		"https://example.com?token=secret",
		"https://example.com#fragment",
	} {
		t.Run(baseURL, func(t *testing.T) {
			if _, err := New(baseURL); err == nil {
				t.Fatal("untrusted base URL accepted")
			}
		})
	}
}

func TestLookupRejectsFailureRedirectOversizeAndInvalidUTF8(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"failure": func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusNotFound)
		},
		"redirect": func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, "/other", http.StatusFound)
		},
		"oversize": func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte(strings.Repeat("x", MaxResponseBytes+1)))
		},
		"invalid utf8": func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte{0xff, 0xfe})
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			client, err := New(server.URL)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if _, err := client.Lookup(context.Background(), "key"); err == nil {
				t.Fatal("invalid reference response accepted")
			}
		})
	}
}

func TestLookupHonorsCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()
	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, lookupErr := client.Lookup(ctx, "key")
		result <- lookupErr
	}()
	<-started
	cancel()
	select {
	case lookupErr := <-result:
		if lookupErr == nil || !errors.Is(lookupErr, context.Canceled) {
			t.Fatalf("Lookup error = %v, want context.Canceled", lookupErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Lookup did not honor cancellation")
	}
}
