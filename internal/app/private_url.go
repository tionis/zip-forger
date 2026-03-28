package app

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"zip-forger/internal/filter"
)

const privateDownloadTokenParam = "token"

type PrivateDownloadCodec struct {
	gcm cipher.AEAD
	ttl time.Duration
}

type privateDownloadPayload struct {
	Owner       string          `json:"owner"`
	Repo        string          `json:"repo"`
	Commit      string          `json:"commit"`
	Preset      string          `json:"preset,omitempty"`
	Adhoc       filter.Criteria `json:"adhoc,omitempty"`
	AccessToken string          `json:"accessToken"`
	ExpiresAt   time.Time       `json:"expiresAt"`
}

func NewPrivateDownloadCodec(secret string, ttl time.Duration) (*PrivateDownloadCodec, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, errors.New("app: private download secret is required")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &PrivateDownloadCodec{gcm: gcm, ttl: ttl}, nil
}

func (c *PrivateDownloadCodec) Encode(owner, repo, commit, preset, accessToken string, adhoc filter.Criteria) (string, time.Time, error) {
	if c == nil {
		return "", time.Time{}, errors.New("app: private download codec is not configured")
	}
	if strings.TrimSpace(owner) == "" || strings.TrimSpace(repo) == "" || strings.TrimSpace(commit) == "" {
		return "", time.Time{}, errors.New("app: private download parameters are incomplete")
	}
	if strings.TrimSpace(accessToken) == "" {
		return "", time.Time{}, errors.New("app: access token is required for private download URLs")
	}

	expiresAt := time.Now().Add(c.ttl)
	payload := privateDownloadPayload{
		Owner:       strings.TrimSpace(owner),
		Repo:        strings.TrimSpace(repo),
		Commit:      strings.TrimSpace(commit),
		Preset:      strings.TrimSpace(preset),
		Adhoc:       normalizeCriteria(adhoc),
		AccessToken: strings.TrimSpace(accessToken),
		ExpiresAt:   expiresAt,
	}
	token, err := c.encodePayload(payload)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (c *PrivateDownloadCodec) Decode(token string) (privateDownloadPayload, error) {
	if c == nil {
		return privateDownloadPayload{}, errors.New("app: private download codec is not configured")
	}
	payload, err := c.decodePayload(token)
	if err != nil {
		return privateDownloadPayload{}, err
	}
	if time.Now().After(payload.ExpiresAt) {
		return privateDownloadPayload{}, errors.New("app: private download token expired")
	}
	if payload.Owner == "" || payload.Repo == "" || payload.Commit == "" || payload.AccessToken == "" {
		return privateDownloadPayload{}, errors.New("app: private download token is invalid")
	}
	payload.Adhoc = normalizeCriteria(payload.Adhoc)
	return payload, nil
}

func (c *PrivateDownloadCodec) encodePayload(payload privateDownloadPayload) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := c.gcm.Seal(nil, nonce, raw, nil)
	return base64.RawURLEncoding.EncodeToString(append(nonce, ciphertext...)), nil
}

func (c *PrivateDownloadCodec) decodePayload(token string) (privateDownloadPayload, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(token))
	if err != nil {
		return privateDownloadPayload{}, err
	}
	nonceSize := c.gcm.NonceSize()
	if len(raw) < nonceSize {
		return privateDownloadPayload{}, errors.New("app: private download token is truncated")
	}

	plain, err := c.gcm.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return privateDownloadPayload{}, err
	}

	var payload privateDownloadPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return privateDownloadPayload{}, err
	}
	return payload, nil
}

func normalizeCriteria(criteria filter.Criteria) filter.Criteria {
	return filter.Criteria{
		IncludeGlobs: normalizeStringList(criteria.IncludeGlobs),
		ExcludeGlobs: normalizeStringList(criteria.ExcludeGlobs),
		Extensions:   normalizeStringList(criteria.Extensions),
		PathPrefixes: normalizeStringList(criteria.PathPrefixes),
	}
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
