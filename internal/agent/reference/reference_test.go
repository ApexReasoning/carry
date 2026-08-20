package reference

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

func TestLookupRejectsInvalidKeys(t *testing.T) {
	client, err := New("https://references.example")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for name, key := range map[string]string{
		"empty":        "",
		"dot":          ".",
		"dot dot":      "..",
		"nul":          "key\x00tail",
		"invalid utf8": string([]byte{0xff}),
		"oversize":     strings.Repeat("k", MaxKeyBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := client.Lookup(context.Background(), key); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("Lookup(%q) error = %v, want ErrInvalidKey", key, err)
			}
		})
	}
}

func TestLookupUsesOneHTTPAttempt(t *testing.T) {
	var droppedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/references/seed" {
			_, _ = io.WriteString(response, "seed")
			return
		}
		droppedRequests.Add(1)
		connection, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack response: %v", err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Lookup(context.Background(), "seed"); err != nil {
		t.Fatalf("seed Lookup: %v", err)
	}
	if _, err := client.Lookup(context.Background(), "drop"); err == nil {
		t.Fatal("dropped lookup succeeded")
	}
	if got := droppedRequests.Load(); got != 1 {
		t.Fatalf("catalog attempts = %d, want 1", got)
	}
}

func TestLookupUsesHTTP1AgainstHTTP2CapableCatalog(t *testing.T) {
	protocol := make(chan int, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		protocol <- request.ProtoMajor
		_, _ = io.WriteString(response, "reference")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	transport := client.http.Transport.(*http.Transport)
	serverTransport := server.Client().Transport.(*http.Transport)
	transport.TLSClientConfig = serverTransport.TLSClientConfig.Clone()
	if _, err := client.Lookup(context.Background(), "key"); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got := <-protocol; got != 1 {
		t.Fatalf("HTTP protocol major = %d, want 1 to prevent HTTP/2 replay", got)
	}
}

func TestLookupHasTransportTimeout(t *testing.T) {
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
	client.http.Timeout = 25 * time.Millisecond
	result := make(chan error, 1)
	go func() {
		_, lookupErr := client.Lookup(context.Background(), "key")
		result <- lookupErr
	}()
	<-started
	select {
	case lookupErr := <-result:
		if lookupErr == nil || !strings.Contains(lookupErr.Error(), "Client.Timeout") {
			t.Fatalf("Lookup error = %v, want client timeout", lookupErr)
		}
	case <-time.After(time.Second):
		t.Fatal("Lookup exceeded its transport timeout")
	}
}

func TestLookupFailsWhenCatalogIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, err := New(server.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	server.Close()
	if _, err := client.Lookup(context.Background(), "key"); err == nil {
		t.Fatal("unavailable catalog lookup succeeded")
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
