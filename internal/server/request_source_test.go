package server

import (
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestRequestSourceRejectsSpoofedForwardingHeaderFromDirectClient(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest("POST", "/v1/auth/email/challenges", nil)
	request.RemoteAddr = "203.0.113.18:49152"
	request.Header.Set("X-Forwarded-For", "198.51.100.99")

	resolved, err := NewRequestSource(nil).Resolve(request)
	if err != nil {
		t.Fatalf("resolve direct source: %v", err)
	}
	if resolved != "203.0.113.18" {
		t.Fatalf("direct source = %q", resolved)
	}
}

func TestRequestSourceUsesRightmostUntrustedAddressBehindTrustedProxy(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	request := httptest.NewRequest("POST", "/v1/auth/email/challenges", nil)
	request.RemoteAddr = "10.0.0.8:443"
	request.Header.Set("X-Forwarded-For", "192.0.2.44, 198.51.100.17, 10.1.2.3")

	resolved, err := NewRequestSource(trusted).Resolve(request)
	if err != nil {
		t.Fatalf("resolve trusted proxy chain: %v", err)
	}
	if resolved != "198.51.100.17" {
		t.Fatalf("trusted proxy source = %q", resolved)
	}
}

func TestRequestSourceRejectsMalformedTrustedProxyChain(t *testing.T) {
	t.Parallel()
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	request := httptest.NewRequest("POST", "/v1/auth/email/challenges", nil)
	request.RemoteAddr = "10.0.0.8:443"
	request.Header.Set("X-Forwarded-For", "198.51.100.17, not-an-address")

	if _, err := NewRequestSource(trusted).Resolve(request); err == nil {
		t.Fatal("malformed trusted proxy chain was accepted")
	}
}

func TestRequestSourceHandlesIPv4AndIPv6PeersAndChains(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		trusted  []netip.Prefix
		peer     string
		forward  string
		expected string
	}{
		{name: "direct IPv4", peer: "192.0.2.5:8080", expected: "192.0.2.5"},
		{name: "direct IPv6", peer: "[2001:db8::5]:8080", expected: "2001:db8::5"},
		{
			name: "trusted IPv6 chain", trusted: []netip.Prefix{netip.MustParsePrefix("2001:db8:ffff::/48")},
			peer: "[2001:db8:ffff::5]:443", forward: "2001:db8::9, 2001:db8:ffff::4", expected: "2001:db8::9",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/v1/auth/email/challenges", nil)
			request.RemoteAddr = test.peer
			if test.forward != "" {
				request.Header.Set("X-Forwarded-For", test.forward)
			}
			resolved, err := NewRequestSource(test.trusted).Resolve(request)
			if err != nil {
				t.Fatalf("resolve request source: %v", err)
			}
			if resolved != test.expected {
				t.Fatalf("source = %q, want %q", resolved, test.expected)
			}
		})
	}
}
