package main

import (
	"io"
	"net/http"
	"time"
)

const providerResponseLimit = 1 << 20

func newOAuthHTTPClient() *http.Client {
	return &http.Client{
		Transport: &boundedOAuthTransport{base: http.DefaultTransport, limit: providerResponseLimit},
		Timeout:   10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type boundedOAuthTransport struct {
	base  http.RoundTripper
	limit int64
}

func (transport *boundedOAuthTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	response.Body = &boundedOAuthBody{
		Reader: io.LimitReader(response.Body, transport.limit+1),
		closer: response.Body,
	}
	return response, nil
}

type boundedOAuthBody struct {
	io.Reader
	closer io.Closer
}

func (body *boundedOAuthBody) Close() error {
	return body.closer.Close()
}
