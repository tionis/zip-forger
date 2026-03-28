package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"zip-forger/internal/source"
)

type Config struct {
	ForgejoBaseURL string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	Scopes         []string
	CookieName     string
	CookieSecure   bool
	SessionSecret  string
	SessionTTL     time.Duration
	StateTTL       time.Duration
}

type Manager struct {
	cfg    Config
	logger *log.Logger
	client *http.Client
	codec  *SessionCodec

	stateMu sync.Mutex
	states  map[string]oauthState
}

type oauthState struct {
	expiresAt time.Time
	returnTo  string
	redirectTo string
}

func NewManager(cfg Config, logger *log.Logger) (*Manager, error) {
	if logger == nil {
		logger = log.Default()
	}

	cfg.CookieName = strings.TrimSpace(cfg.CookieName)
	if cfg.CookieName == "" {
		cfg.CookieName = "zip_forger_session"
	}
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.StateTTL <= 0 {
		cfg.StateTTL = 10 * time.Minute
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"read:repository", "read:user", "write:repository"}
	}

	manager := &Manager{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 15 * time.Second},
		states: make(map[string]oauthState),
	}

	cfg.ForgejoBaseURL = strings.TrimSuffix(strings.TrimSpace(cfg.ForgejoBaseURL), "/")
	if cfg.ForgejoBaseURL == "" {
		return nil, errors.New("auth: forgejo base URL is required")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("auth: client ID is required")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("auth: client secret is required")
	}

	codec, err := NewSessionCodec(cfg.SessionSecret)
	if err != nil {
		return nil, err
	}
	manager.codec = codec
	manager.cfg = cfg

	return manager, nil
}

func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	if m == nil {
		return
	}
	mux.HandleFunc("GET /auth/login", m.handleLogin)
	mux.HandleFunc("GET /auth/callback", m.handleCallback)
	mux.HandleFunc("POST /auth/logout", m.handleLogout)
	mux.HandleFunc("GET /auth/me", m.handleMe)
}

func (m *Manager) Middleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, _, _, err := m.extractToken(r)
		if err != nil {
			m.clearSessionCookie(w)
			writeAuthError(w, http.StatusUnauthorized, "authentication_failed", "invalid authentication session")
			return
		}

		if token == "" {
			writeAuthError(w, http.StatusUnauthorized, "authentication_required", "authentication required")
			return
		}

		ctx := source.WithAccessToken(r.Context(), token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (m *Manager) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomToken(24)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "state_generation_failed", "unable to start login flow")
		return
	}
	redirectURL, err := m.resolveRedirectURL(r)
	if err != nil {
		writeAuthError(w, http.StatusBadRequest, "invalid_redirect_url", "unable to determine oauth redirect url")
		return
	}

	returnTo := sanitizeReturnTo(r.URL.Query().Get("return_to"))
	m.storeState(state, oauthState{
		expiresAt:  time.Now().Add(m.cfg.StateTTL),
		returnTo:   returnTo,
		redirectTo: redirectURL,
	})

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", m.cfg.ClientID)
	query.Set("redirect_uri", redirectURL)
	query.Set("state", state)
	query.Set("scope", strings.Join(m.cfg.Scopes, " "))

	authorizeURL := m.cfg.ForgejoBaseURL + "/login/oauth/authorize?" + query.Encode()
	m.logger.Printf("auth: redirecting to %s", authorizeURL)
	http.Redirect(w, r, authorizeURL, http.StatusFound)
}

func (m *Manager) handleCallback(w http.ResponseWriter, r *http.Request) {
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if state == "" || code == "" {
		writeAuthError(w, http.StatusBadRequest, "invalid_callback", "missing state or code")
		return
	}

	storedState, ok := m.consumeState(state)
	if !ok || time.Now().After(storedState.expiresAt) {
		writeAuthError(w, http.StatusBadRequest, "invalid_state", "state is invalid or expired")
		return
	}
	redirectURL := storedState.redirectTo
	var err error
	if strings.TrimSpace(redirectURL) == "" {
		redirectURL, err = m.resolveRedirectURL(r)
		if err != nil {
			writeAuthError(w, http.StatusBadRequest, "invalid_redirect_url", "unable to determine oauth redirect url")
			return
		}
	}

	token, tokenType, tokenScope, expiresAt, err := m.exchangeCode(r.Context(), code, redirectURL)
	if err != nil {
		m.logger.Printf("auth callback exchange failed: %v", err)
		writeAuthError(w, http.StatusBadGateway, "token_exchange_failed", "unable to exchange oauth code")
		return
	}

	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(m.cfg.SessionTTL)
	}
	session := Session{
		AccessToken: token,
		TokenType:   tokenType,
		Scope:       tokenScope,
		ExpiresAt:   expiresAt,
	}
	encoded, err := m.codec.Encode(session)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "session_encode_failed", "unable to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.CookieName,
		Value:    encoded,
		HttpOnly: true,
		Secure:   m.cfg.CookieSecure,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
	})

	if storedState.returnTo == "" {
		storedState.returnTo = "/"
	}
	http.Redirect(w, r, storedState.returnTo, http.StatusFound)
}

func (m *Manager) handleLogout(w http.ResponseWriter, _ *http.Request) {
	m.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (m *Manager) handleMe(w http.ResponseWriter, r *http.Request) {
	token, scope, sourceName, err := m.extractToken(r)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "authentication_failed", "invalid authentication session")
		return
	}
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"source":        sourceName,
		"scope":         scope,
	})
}

func (m *Manager) extractToken(r *http.Request) (token, scope, sourceName string, err error) {
	if headerToken := bearerTokenFromHeader(r.Header.Get("Authorization")); headerToken != "" {
		return headerToken, "", "authorization_header", nil
	}

	if m.codec == nil {
		return "", "", "", nil
	}

	cookie, err := r.Cookie(m.cfg.CookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", "", "", nil
		}
		return "", "", "", err
	}

	session, err := m.codec.Decode(cookie.Value)
	if err != nil {
		return "", "", "", err
	}
	if !session.ExpiresAt.IsZero() && time.Now().After(session.ExpiresAt) {
		return "", "", "", errors.New("auth: session expired")
	}
	return session.AccessToken, session.Scope, "session_cookie", nil
}

func (m *Manager) exchangeCode(ctx context.Context, code, redirectURL string) (accessToken, tokenType, scope string, expiresAt time.Time, err error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", m.cfg.ClientID)
	form.Set("client_secret", m.cfg.ClientSecret)
	form.Set("redirect_uri", redirectURL)
	form.Set("code", code)

	endpoint := m.cfg.ForgejoBaseURL + "/login/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", "", "", time.Time{}, fmt.Errorf("oauth exchange failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil || tokenResponse.AccessToken == "" {
		parsed, parseErr := url.ParseQuery(string(body))
		if parseErr == nil && parsed.Get("access_token") != "" {
			tokenResponse.AccessToken = parsed.Get("access_token")
			tokenResponse.TokenType = parsed.Get("token_type")
			tokenResponse.Scope = parsed.Get("scope")
		} else {
			return "", "", "", time.Time{}, fmt.Errorf("auth: token response invalid: %s", string(body))
		}
	}
	if tokenResponse.AccessToken == "" {
		return "", "", "", time.Time{}, errors.New("auth: oauth exchange did not return access token")
	}
	if tokenResponse.TokenType == "" {
		tokenResponse.TokenType = "token"
	}

	// If scope is empty in response, it often means it matches the requested scopes.
	grantedScope := tokenResponse.Scope
	if grantedScope == "" {
		grantedScope = strings.Join(m.cfg.Scopes, " ")
	}
	m.logger.Printf("auth: token exchange successful, scope granted: %q", grantedScope)

	if tokenResponse.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	}

	return tokenResponse.AccessToken, tokenResponse.TokenType, grantedScope, expiresAt, nil
}

func (m *Manager) storeState(key string, value oauthState) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	now := time.Now()
	for stateKey, stateValue := range m.states {
		if now.After(stateValue.expiresAt) {
			delete(m.states, stateKey)
		}
	}
	m.states[key] = value
}

func (m *Manager) consumeState(key string) (oauthState, bool) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()

	value, ok := m.states[key]
	if ok {
		delete(m.states, key)
	}
	return value, ok
}

func (m *Manager) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.CookieName,
		Value:    "",
		HttpOnly: true,
		Secure:   m.cfg.CookieSecure,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func bearerTokenFromHeader(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const prefix = "bearer "
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(value[len(prefix):])
}

func sanitizeReturnTo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		return "/"
	}
	return value
}

func (m *Manager) resolveRedirectURL(r *http.Request) (string, error) {
	if explicit := strings.TrimSpace(m.cfg.RedirectURL); explicit != "" {
		return explicit, nil
	}
	if r == nil {
		return "", errors.New("auth: request is required to derive redirect url")
	}

	scheme, host := forwardedOrigin(r)
	if host == "" {
		return "", errors.New("auth: request host is empty")
	}
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return scheme + "://" + host + "/auth/callback", nil
}

func forwardedOrigin(r *http.Request) (scheme, host string) {
	if r == nil {
		return "", ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("Forwarded")); forwarded != "" {
		first := strings.Split(forwarded, ",")[0]
		for _, part := range strings.Split(first, ";") {
			piece := strings.TrimSpace(part)
			key, value, ok := strings.Cut(piece, "=")
			if !ok {
				continue
			}
			key = strings.ToLower(strings.TrimSpace(key))
			value = strings.Trim(strings.TrimSpace(value), `"`)
			switch key {
			case "proto":
				if scheme == "" {
					scheme = value
				}
			case "host":
				if host == "" {
					host = value
				}
			}
		}
	}

	if scheme == "" {
		scheme = firstForwardedValue(r.Header.Get("X-Forwarded-Proto"))
	}
	if host == "" {
		host = firstForwardedValue(r.Header.Get("X-Forwarded-Host"))
	}
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	return scheme, host
}

func firstForwardedValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(value, ",")[0])
}

func randomToken(numBytes int) (string, error) {
	if numBytes <= 0 {
		return "", errors.New("auth: random token size must be > 0")
	}
	raw := make([]byte, numBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
