package server

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

var errInvalidRequestSource = errors.New("request source is invalid")

// RequestSource resolves the anti-abuse source without trusting forwarding
// headers received from a direct client.
type RequestSource struct {
	trustedProxyCIDRs []netip.Prefix
}

func NewRequestSource(trustedProxyCIDRs []netip.Prefix) RequestSource {
	return RequestSource{trustedProxyCIDRs: append([]netip.Prefix(nil), trustedProxyCIDRs...)}
}

func (source RequestSource) Resolve(request *http.Request) (string, error) {
	peer, err := parsePeerAddress(request.RemoteAddr)
	if err != nil {
		return "", errInvalidRequestSource
	}
	if !source.isTrustedProxy(peer) {
		return peer.String(), nil
	}

	forwardedValues := request.Header.Values("X-Forwarded-For")
	if len(forwardedValues) == 0 {
		return peer.String(), nil
	}
	var forwarded []netip.Addr
	for _, value := range forwardedValues {
		for part := range strings.SplitSeq(value, ",") {
			address, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil {
				return "", errInvalidRequestSource
			}
			forwarded = append(forwarded, address.Unmap())
		}
	}
	if len(forwarded) == 0 {
		return "", errInvalidRequestSource
	}
	for _, address := range slices.Backward(forwarded) {
		if !source.isTrustedProxy(address) {
			return address.String(), nil
		}
	}
	return forwarded[0].String(), nil
}

func (source RequestSource) isTrustedProxy(address netip.Addr) bool {
	for _, prefix := range source.trustedProxyCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parsePeerAddress(remoteAddress string) (netip.Addr, error) {
	trimmed := strings.TrimSpace(remoteAddress)
	host, _, err := net.SplitHostPort(trimmed)
	if err == nil {
		address, parseErr := netip.ParseAddr(host)
		if parseErr != nil {
			return netip.Addr{}, parseErr
		}
		return address.Unmap(), nil
	}
	address, parseErr := netip.ParseAddr(trimmed)
	if parseErr != nil {
		return netip.Addr{}, parseErr
	}
	return address.Unmap(), nil
}
