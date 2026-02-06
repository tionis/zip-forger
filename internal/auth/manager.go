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
	Enabled        bool
	Required       bool
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
		cfg.Scopes = []string{"read:repository"}
	}

	manager := &Manager{
		cfg:    cfg,
		logger: logger,
		client: &http.Client{Timeout: 15 * time.Second},
		states: make(map[string]oauthState),
	}

	if !cfg.Enabled {
		return manager, nil
	}

	cfg.ForgejoBaseURL = strings.TrimSuffix(strings.TrimSpace(cfg.ForgejoBaseURL), "/")
	if cfg.ForgejoBaseURL == "" {
		return nil, errors.New("auth: forgejo base URL is required when auth is enabled")
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, errors.New("auth: client ID is required when auth is enabled")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("auth: client secret is required when auth is enabled")
	}
	if strings.TrimSpace(cfg.RedirectURL) == "" {
		return nil, errors.New("auth: redirect URL is required when auth is enabled")
	}

	codec, err := NewSessionCodec(cfg.SessionSecret)
	if err != nil {
		return nil, err
	}
	manager.codec = codec
	manager.cfg = cfg

	return manager, nil
}

func (m *Manager) Enabled() bool {
	return m != nil && m.cfg.Enabled
}

func (m *Manager) Required() bool {
	return m != nil && m.cfg.Required
}

func (m *Manager) RegisterRoutes(mux *http.ServeMux) {
	if m == nil || !m.cfg.Enabled {
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
		token, _, err := m.extractToken(r)
		if err != nil {
			if m.cfg.Required {
				writeAuthError(w, http.StatusUnauthorized, "authentication_failed", "invalid authentication session")
				return
			}
			m.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}

		if token == "" {
			if m.cfg.Required {
				writeAuthError(w, http.StatusUnauthorized, "authentication_required", "authentication required")
				return
			}
			next.ServeHTTP(w, r)
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

	returnTo := sanitizeReturnTo(r.URL.Query().Get("return_to"))
	m.storeState(state, oauthState{
		expiresAt: time.Now().Add(m.cfg.StateTTL),
		returnTo:  returnTo,
	})

	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", m.cfg.ClientID)
	query.Set("redirect_uri", m.cfg.RedirectURL)
	query.Set("state", state)
	query.Set("scope", strings.Join(m.cfg.Scopes, " "))

	redirectURL := m.cfg.ForgejoBaseURL + "/login/oauth/authorize?" + query.Encode()
	http.Redirect(w, r, redirectURL, http.StatusFound)
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

	token, tokenType, expiresAt, err := m.exchangeCode(r.Context(), code)
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
	token, sourceName, err := m.extractToken(r)
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
	})
}

func (m *Manager) extractToken(r *http.Request) (token, sourceName string, err error) {
	if headerToken := bearerTokenFromHeader(r.Header.Get("Authorization")); headerToken != "" {
		return headerToken, "authorization_header", nil
	}

	if m.codec == nil {
		return "", "", nil
	}

	cookie, err := r.Cookie(m.cfg.CookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return "", "", nil
		}
		return "", "", err
	}

	session, err := m.codec.Decode(cookie.Value)
	if err != nil {
		return "", "", err
	}
	if !session.ExpiresAt.IsZero() && time.Now().After(session.ExpiresAt) {
		return "", "", errors.New("auth: session expired")
	}
	return session.AccessToken, "session_cookie", nil
}

func (m *Manager) exchangeCode(ctx context.Context, code string) (accessToken, tokenType string, expiresAt time.Time, err error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", m.cfg.ClientID)
	form.Set("client_secret", m.cfg.ClientSecret)
	form.Set("redirect_uri", m.cfg.RedirectURL)
	form.Set("code", code)

	endpoint := m.cfg.ForgejoBaseURL + "/login/oauth/access_token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return "", "", time.Time{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", time.Time{}, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", "", time.Time{}, fmt.Errorf("oauth exchange failed status=%d body=%q", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil || tokenResponse.AccessToken == "" {
		parsed, parseErr := url.ParseQuery(string(body))
		if parseErr != nil {
			if err != nil {
				return "", "", time.Time{}, err
			}
			return "", "", time.Time{}, errors.New("auth: token response missing access token")
		}
		tokenResponse.AccessToken = parsed.Get("access_token")
		tokenResponse.TokenType = parsed.Get("token_type")
	}

	if tokenResponse.AccessToken == "" {
		return "", "", time.Time{}, errors.New("auth: oauth exchange did not return access token")
	}
	if tokenResponse.TokenType == "" {
		tokenResponse.TokenType = "token"
	}
	if tokenResponse.ExpiresIn > 0 {
		expiresAt = time.Now().Add(time.Duration(tokenResponse.ExpiresIn) * time.Second)
	}

	return tokenResponse.AccessToken, tokenResponse.TokenType, expiresAt, nil
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
