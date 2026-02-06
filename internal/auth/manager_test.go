package auth

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOAuthLoginCallbackFlow(t *testing.T) {
	manager, err := NewManager(Config{
		Enabled:        true,
		Required:       true,
		ForgejoBaseURL: "http://forgejo.local",
		ClientID:       "client-id",
		ClientSecret:   "client-secret",
		RedirectURL:    "http://example.local/auth/callback",
		SessionSecret:  "session-secret",
		CookieSecure:   false,
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	manager.client = &http.Client{
		Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/login/oauth/access_token" {
				return responseJSON(http.StatusNotFound, `{"error":"not_found"}`), nil
			}
			if r.Method != http.MethodPost {
				return responseJSON(http.StatusMethodNotAllowed, `{"error":"method_not_allowed"}`), nil
			}
			return responseJSON(http.StatusOK, `{"access_token":"token-xyz","token_type":"token","expires_in":3600}`), nil
		}),
	}

	mux := http.NewServeMux()
	manager.RegisterRoutes(mux)

	loginReq := httptest.NewRequest(http.MethodGet, "/auth/login?return_to=/ui/downloads", nil)
	loginResp := httptest.NewRecorder()
	mux.ServeHTTP(loginResp, loginReq)

	if loginResp.Code != http.StatusFound {
		t.Fatalf("login status=%d body=%s", loginResp.Code, loginResp.Body.String())
	}

	location := loginResp.Header().Get("Location")
	if !strings.HasPrefix(location, "http://forgejo.local/login/oauth/authorize?") {
		t.Fatalf("unexpected login redirect location: %s", location)
	}
	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("url.Parse failed: %v", err)
	}
	state := redirectURL.Query().Get("state")
	if state == "" {
		t.Fatalf("expected state query parameter")
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/auth/callback?code=abc&state="+url.QueryEscape(state), nil)
	callbackResp := httptest.NewRecorder()
	mux.ServeHTTP(callbackResp, callbackReq)

	if callbackResp.Code != http.StatusFound {
		t.Fatalf("callback status=%d body=%s", callbackResp.Code, callbackResp.Body.String())
	}
	if callbackResp.Header().Get("Location") != "/ui/downloads" {
		t.Fatalf("unexpected callback redirect target: %s", callbackResp.Header().Get("Location"))
	}
	setCookie := callbackResp.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "zip_forger_session=") {
		t.Fatalf("expected session cookie, got: %s", setCookie)
	}
}

func TestMiddlewareRequiredBearerToken(t *testing.T) {
	manager, err := NewManager(Config{
		Enabled:       false,
		Required:      true,
		SessionSecret: "session-secret",
	}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	handler := manager.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/preview", nil)
	req.Header.Set("Authorization", "Bearer abc123")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 with bearer token, got %d body=%s", resp.Code, resp.Body.String())
	}

	unauthReq := httptest.NewRequest(http.MethodGet, "/api/repos/acme/rules/preview", nil)
	unauthResp := httptest.NewRecorder()
	handler.ServeHTTP(unauthResp, unauthReq)
	if unauthResp.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d body=%s", unauthResp.Code, unauthResp.Body.String())
	}

	var body map[string]map[string]string
	if err := json.Unmarshal(unauthResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("unable to decode response json: %v", err)
	}
	if body["error"]["code"] != "authentication_required" {
		t.Fatalf("unexpected error code: %#v", body)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func responseJSON(status int, payload string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(payload)),
	}
}
