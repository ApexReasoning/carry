package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	googleAuthorizationEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenEndpoint         = "https://oauth2.googleapis.com/token"
	googleJWKSURL               = "https://www.googleapis.com/oauth2/v3/certs"
	canonicalGoogleIssuer       = "https://accounts.google.com"
)

type googleEndpoints struct {
	authorization string
	token         string
	jwks          string
}

type googleLogin struct {
	config   oauth2.Config
	verifier *oidc.IDTokenVerifier
	client   *http.Client
}

func newGoogleLogin(clientID string, clientSecret string, redirectURL string) (*googleLogin, error) {
	return newGoogleLoginAt(clientID, clientSecret, redirectURL, googleEndpoints{
		authorization: googleAuthorizationEndpoint,
		token:         googleTokenEndpoint,
		jwks:          googleJWKSURL,
	})
}

func newGoogleLoginAt(
	clientID string,
	clientSecret string,
	redirectURL string,
	endpoints googleEndpoints,
) (*googleLogin, error) {
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" ||
		strings.TrimSpace(redirectURL) == "" || strings.TrimSpace(endpoints.authorization) == "" ||
		strings.TrimSpace(endpoints.token) == "" || strings.TrimSpace(endpoints.jwks) == "" {
		return nil, errors.New("Google OAuth configuration is incomplete")
	}
	client := newOAuthHTTPClient()
	keyContext := oidc.ClientContext(context.Background(), client)
	keySet := oidc.NewRemoteKeySet(keyContext, endpoints.jwks)
	return &googleLogin{
		config: oauth2.Config{
			ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
			Endpoint: oauth2.Endpoint{
				AuthURL: endpoints.authorization, TokenURL: endpoints.token,
				AuthStyle: oauth2.AuthStyleInParams,
			},
			Scopes: []string{oidc.ScopeOpenID},
		},
		verifier: oidc.NewVerifier(canonicalGoogleIssuer, keySet, &oidc.Config{
			ClientID: clientID, SkipIssuerCheck: true,
		}),
		client: client,
	}, nil
}

func (login *googleLogin) AuthorizationURL(state string, nonce string, codeChallenge string) string {
	return login.config.AuthCodeURL(
		state,
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

func (login *googleLogin) Authenticate(
	ctx context.Context,
	code string,
	codeVerifier string,
	nonce string,
) (identity.GoogleIdentityProof, error) {
	providerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	providerCtx = context.WithValue(providerCtx, oauth2.HTTPClient, login.client)
	token, err := login.config.Exchange(providerCtx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return identity.GoogleIdentityProof{}, fmt.Errorf("exchange Google authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return identity.GoogleIdentityProof{}, errors.New("Google token response has no ID token")
	}
	idToken, err := login.verifier.Verify(providerCtx, rawIDToken)
	if err != nil {
		return identity.GoogleIdentityProof{}, fmt.Errorf("verify Google ID token: %w", err)
	}
	if idToken.Issuer != canonicalGoogleIssuer && idToken.Issuer != "accounts.google.com" {
		return identity.GoogleIdentityProof{}, errors.New("Google ID token issuer is invalid")
	}
	if len(idToken.Audience) != 1 || subtle.ConstantTimeCompare([]byte(idToken.Audience[0]), []byte(login.config.ClientID)) != 1 {
		return identity.GoogleIdentityProof{}, errors.New("Google ID token audience is invalid")
	}
	if idToken.Subject == "" || len(idToken.Subject) > 255 {
		return identity.GoogleIdentityProof{}, errors.New("Google ID token subject is invalid")
	}
	if idToken.IssuedAt.IsZero() || idToken.IssuedAt.After(time.Now().Add(time.Minute)) {
		return identity.GoogleIdentityProof{}, errors.New("Google ID token issued time is invalid")
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		return identity.GoogleIdentityProof{}, errors.New("Google ID token nonce is invalid")
	}
	return identity.GoogleIdentityProof{Issuer: canonicalGoogleIssuer, Subject: idToken.Subject}, nil
}
