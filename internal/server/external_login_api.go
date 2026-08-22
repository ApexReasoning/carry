package server

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ApexReasoning/carry/internal/identity"
	"github.com/ApexReasoning/carry/internal/space"
	"github.com/google/uuid"
)

const externalLoginCookie = "__Host-carry_oauth"

// ExternalOrigin is the one configured HTTPS authority used for OAuth
// callbacks. Request and forwarded host fields can never change it.
type ExternalOrigin struct {
	value string
	host  string
}

func ParseExternalOrigin(value string) (ExternalOrigin, error) {
	if strings.TrimSpace(value) != value || value == "" {
		return ExternalOrigin{}, errors.New("external origin must be a canonical HTTPS origin")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" ||
		parsed.User != nil || parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		parsed.Opaque != "" || parsed.Host != strings.ToLower(parsed.Host) || value != "https://"+parsed.Host {
		return ExternalOrigin{}, errors.New("external origin must be a canonical HTTPS origin")
	}
	return ExternalOrigin{value: value, host: parsed.Host}, nil
}

func (origin ExternalOrigin) String() string {
	return origin.value
}

func (origin ExternalOrigin) CallbackURL(provider identity.ExternalLoginProvider) string {
	return origin.value + "/v1/auth/" + provider.String() + "/callback"
}

func (origin ExternalOrigin) matches(request *http.Request) bool {
	return request.Host == origin.host
}

func (origin ExternalOrigin) appURL(status string) string {
	if status == "" {
		return origin.value + "/"
	}
	return origin.value + "/?sign_in=" + url.QueryEscape(status)
}

func (origin ExternalOrigin) identityURL(purpose identity.ProofPurpose) string {
	status := "confirmed"
	if purpose == identity.LinkPurpose {
		status = "linked"
	}
	return origin.identityChangeURL(status)
}

func (origin ExternalOrigin) identityFailureURL(purpose identity.ProofPurpose, err error) string {
	action := "confirmation"
	if purpose == identity.LinkPurpose {
		action = "link"
	}
	outcome := "failed"
	if errors.Is(err, identity.ErrExternalLoginDenied) {
		outcome = "cancelled"
	} else if !errors.Is(err, identity.ErrExternalLoginRejected) {
		outcome = "unavailable"
	}
	return origin.identityChangeURL(action + "_" + outcome)
}

func (origin ExternalOrigin) identityChangeURL(status string) string {
	return origin.value + "/?identity_change=" + url.QueryEscape(status)
}

func (origin ExternalOrigin) acceptsSensitivePOST(request *http.Request) bool {
	return origin.matches(request) && request.Header.Get("Origin") == origin.value
}

// ExternalLogin is the Identity provider-login behavior consumed by HTTP.
type ExternalLogin interface {
	StartGoogle(context.Context, string, string) (identity.ExternalLoginStart, error)
	StartGitHub(context.Context, string, string) (identity.ExternalLoginStart, error)
	StartGoogleReauthentication(context.Context, string, string) (identity.ExternalLoginStart, error)
	StartGitHubReauthentication(context.Context, string, string) (identity.ExternalLoginStart, error)
	StartGoogleLink(context.Context, string, string) (identity.ExternalLoginStart, error)
	StartGitHubLink(context.Context, string, string) (identity.ExternalLoginStart, error)
	CompleteGoogle(context.Context, identity.ExternalLoginCallback) (identity.BrowserSession, error)
	CompleteGitHub(context.Context, identity.ExternalLoginCallback) (identity.BrowserSession, error)
}

type externalLoginAPI struct {
	login          ExternalLogin
	sessions       BrowserSessions
	credentials    identity.Credentials
	origin         ExternalOrigin
	requestSources RequestSource
}

func (api externalLoginAPI) startGoogle(response http.ResponseWriter, request *http.Request) {
	api.start(response, request, api.login.StartGoogle)
}

func (api externalLoginAPI) startGitHub(response http.ResponseWriter, request *http.Request) {
	api.start(response, request, api.login.StartGitHub)
}

func (api externalLoginAPI) startGoogleReauthentication(response http.ResponseWriter, request *http.Request) {
	api.startMethod(response, request, api.login.StartGoogleReauthentication)
}

func (api externalLoginAPI) startGitHubReauthentication(response http.ResponseWriter, request *http.Request) {
	api.startMethod(response, request, api.login.StartGitHubReauthentication)
}

func (api externalLoginAPI) startGoogleLink(response http.ResponseWriter, request *http.Request) {
	api.startMethod(response, request, api.login.StartGoogleLink)
}

func (api externalLoginAPI) startGitHubLink(response http.ResponseWriter, request *http.Request) {
	api.startMethod(response, request, api.login.StartGitHubLink)
}

func (api externalLoginAPI) start(
	response http.ResponseWriter,
	request *http.Request,
	start func(context.Context, string, string) (identity.ExternalLoginStart, error),
) {
	response.Header().Set("Referrer-Policy", "no-referrer")
	if !api.origin.matches(request) {
		writeAPIError(response, http.StatusBadRequest, "request authority is invalid")
		return
	}
	authenticated, err := api.hasAuthenticatedPrincipal(request)
	if err != nil {
		writeUserInternalError(response, userReadFailure, "check current Browser Session", err)
		return
	}
	if authenticated {
		writeAPIError(response, http.StatusConflict, "sign out before using another sign-in method")
		return
	}
	invitationID, ok := externalLoginInvitation(response, request)
	if !ok {
		return
	}
	if invitationID != "" && uuid.Validate(invitationID) != nil {
		writeAPIError(response, http.StatusBadRequest, "invitation continuation is invalid")
		return
	}
	source, err := api.requestSources.Resolve(request)
	if err != nil {
		writeAPIError(response, http.StatusBadRequest, "request source is invalid")
		return
	}
	result, err := start(request.Context(), invitationID, source)
	if errors.Is(err, identity.ErrExternalLoginRateLimited) {
		scope := "unspecified"
		switch {
		case errors.Is(err, identity.ErrExternalLoginSourceAdmissionLimited):
			scope = "source"
		case errors.Is(err, identity.ErrExternalLoginGlobalAdmissionLimited):
			scope = "global"
		}
		slog.Warn("external sign-in admission limited", "scope", scope)
		writeAPIError(response, http.StatusTooManyRequests, identity.ErrExternalLoginRateLimited.Error())
		return
	}
	if err != nil {
		writeAPIError(response, http.StatusServiceUnavailable, "start external sign-in")
		return
	}
	setExternalLoginCookie(response, result.BrowserCredential, result.ExpiresAt)
	http.Redirect(response, request, result.AuthorizationURL, http.StatusSeeOther)
}

func externalLoginInvitation(response http.ResponseWriter, request *http.Request) (string, bool) {
	if request.ContentLength != 0 {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/x-www-form-urlencoded" {
			writeAPIError(response, http.StatusBadRequest, "external sign-in form is invalid")
			return "", false
		}
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxCommandBytes)
	if err := request.ParseForm(); err != nil {
		writeAPIError(response, http.StatusBadRequest, "external sign-in form is invalid")
		return "", false
	}
	for name, values := range request.PostForm {
		if name != "invitation_id" || len(values) != 1 {
			writeAPIError(response, http.StatusBadRequest, "external sign-in form is invalid")
			return "", false
		}
	}
	return request.PostForm.Get("invitation_id"), true
}

func (api externalLoginAPI) startMethod(
	response http.ResponseWriter,
	request *http.Request,
	start func(context.Context, string, string) (identity.ExternalLoginStart, error),
) {
	response.Header().Set("Referrer-Policy", "no-referrer")
	if !api.origin.acceptsSensitivePOST(request) {
		writeAPIError(response, http.StatusBadRequest, "request origin is invalid")
		return
	}
	if strings.TrimSpace(request.Header.Get("Authorization")) != "" {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is required")
		return
	}
	user, ok := currentUser(response, request)
	if !ok {
		return
	}
	cookie, err := request.Cookie(browserSessionCookie)
	if err != nil {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is required")
		return
	}
	sessionID, ok := api.credentials.ParseBrowserSessionCredential(cookie.Value)
	if !ok {
		writeAPIError(response, http.StatusUnauthorized, "Browser Session authentication is invalid")
		return
	}
	result, err := start(request.Context(), user.UserID, sessionID)
	if err != nil {
		writeIdentityMethodError(response, err, userMutationFailure, "complete external sign-in method change")
		return
	}
	setExternalLoginCookie(response, result.BrowserCredential, result.ExpiresAt)
	http.Redirect(response, request, result.AuthorizationURL, http.StatusSeeOther)
}

func (api externalLoginAPI) callbackGoogle(response http.ResponseWriter, request *http.Request) {
	api.callback(response, request, api.login.CompleteGoogle)
}

func (api externalLoginAPI) callbackGitHub(response http.ResponseWriter, request *http.Request) {
	api.callback(response, request, api.login.CompleteGitHub)
}

func (api externalLoginAPI) callback(
	response http.ResponseWriter,
	request *http.Request,
	complete func(context.Context, identity.ExternalLoginCallback) (identity.BrowserSession, error),
) {
	response.Header().Set("Referrer-Policy", "no-referrer")
	if !api.origin.matches(request) {
		writeAPIError(response, http.StatusBadRequest, "request authority is invalid")
		return
	}
	callback, ok := parseExternalCallback(request)
	if !ok {
		expireExternalLoginCookie(response)
		http.Redirect(response, request, api.origin.appURL("invalid"), http.StatusSeeOther)
		return
	}
	cookie, err := request.Cookie(externalLoginCookie)
	if err != nil || cookie.Value == "" {
		expireExternalLoginCookie(response)
		http.Redirect(response, request, api.origin.appURL("invalid"), http.StatusSeeOther)
		return
	}
	callback.BrowserCredential = cookie.Value
	session, err := complete(request.Context(), callback)
	expireExternalLoginCookie(response)
	if err == nil {
		credential, credentialErr := api.credentials.BrowserSessionCredential(session.SessionID)
		if credentialErr != nil {
			http.Redirect(response, request, api.origin.appURL("unavailable"), http.StatusSeeOther)
			return
		}
		setBrowserSessionCookie(response, credential, session.ExpiresAt)
		if session.Purpose == identity.LinkPurpose || session.Purpose == identity.ReauthenticatePurpose {
			http.Redirect(response, request, api.origin.identityURL(session.Purpose), http.StatusSeeOther)
			return
		}
		if destination := api.invitationURL(session.InvitationID, ""); destination != "" {
			http.Redirect(response, request, destination, http.StatusSeeOther)
			return
		}
		http.Redirect(response, request, api.origin.appURL(""), http.StatusSeeOther)
		return
	}
	if purpose, ok := identity.ExternalProofFailurePurpose(err); ok && purpose != identity.LoginPurpose {
		http.Redirect(response, request, api.origin.identityFailureURL(purpose, err), http.StatusSeeOther)
		return
	}
	status := "invalid"
	if errors.Is(err, identity.ErrExternalLoginDenied) {
		status = "cancelled"
	} else if errors.Is(err, identity.ErrExternalLoginUnavailable) || errors.Is(err, identity.ErrExternalLoginConflict) {
		status = "unavailable"
	}
	if destination := api.invitationURL(identity.ExternalProofFailureInvitationID(err), status); destination != "" {
		http.Redirect(response, request, destination, http.StatusSeeOther)
		return
	}
	http.Redirect(response, request, api.origin.appURL(status), http.StatusSeeOther)
}

func (api externalLoginAPI) invitationURL(invitationID, status string) string {
	path, err := space.InvitationPath(invitationID)
	if err != nil {
		return ""
	}
	if status == "" {
		return api.origin.value + path
	}
	return api.origin.value + path + "?" + url.Values{"sign_in": {status}}.Encode()
}

func parseExternalCallback(request *http.Request) (identity.ExternalLoginCallback, bool) {
	if len(request.URL.RawQuery) == 0 || len(request.URL.RawQuery) > 8192 {
		return identity.ExternalLoginCallback{}, false
	}
	query := request.URL.Query()
	state, ok := oneQueryValue(query, "state", true)
	if !ok || len(state) > 255 {
		return identity.ExternalLoginCallback{}, false
	}
	code, codeOK := oneQueryValue(query, "code", false)
	providerError, errorOK := oneQueryValue(query, "error", false)
	if len(code) > 4096 || len(providerError) > 255 {
		return identity.ExternalLoginCallback{}, false
	}
	if !codeOK || !errorOK || (code != "" && providerError != "") || (code == "" && providerError == "") {
		return identity.ExternalLoginCallback{}, false
	}
	callback := identity.ExternalLoginCallback{
		State: state, Code: code, ExactResponse: request.URL.RawQuery,
	}
	if code != "" {
		callback.Outcome = identity.ExternalCallbackCode
	} else if providerError == "access_denied" {
		callback.Outcome = identity.ExternalCallbackDenied
	} else {
		callback.Outcome = identity.ExternalCallbackUnavailable
	}
	return callback, true
}

func oneQueryValue(values url.Values, name string, required bool) (string, bool) {
	items, exists := values[name]
	if !exists {
		return "", !required
	}
	if len(items) != 1 || items[0] == "" {
		return "", false
	}
	return items[0], true
}

func (api externalLoginAPI) hasAuthenticatedPrincipal(request *http.Request) (bool, error) {
	if strings.TrimSpace(request.Header.Get("Authorization")) != "" {
		return true, nil
	}
	cookie, err := request.Cookie(browserSessionCookie)
	if err != nil {
		return false, nil
	}
	sessionID, ok := api.credentials.ParseBrowserSessionCredential(cookie.Value)
	if !ok {
		return false, nil
	}
	_, err = api.sessions.AuthenticateBrowserSession(request.Context(), sessionID)
	if errors.Is(err, identity.ErrUnauthenticated) {
		return false, nil
	}
	return err == nil, err
}

func setExternalLoginCookie(response http.ResponseWriter, credential string, expiresAt time.Time) {
	http.SetCookie(response, &http.Cookie{
		Name: externalLoginCookie, Value: credential, Path: "/",
		Expires: expiresAt, MaxAge: maxAgeUntil(expiresAt),
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

func expireExternalLoginCookie(response http.ResponseWriter) {
	http.SetCookie(response, &http.Cookie{
		Name: externalLoginCookie, Value: "", Path: "/", MaxAge: -1,
		Expires: time.Unix(1, 0), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}
