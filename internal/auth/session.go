package auth

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
)

type Session struct {
	AccessToken string    `json:"accessToken"`
	TokenType   string    `json:"tokenType,omitempty"`
	Scope       string    `json:"scope,omitempty"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type SessionCodec struct {
	gcm cipher.AEAD
}

func NewSessionCodec(secret string) (*SessionCodec, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, errors.New("auth: session secret is required")
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
	return &SessionCodec{gcm: gcm}, nil
}

func (c *SessionCodec) Encode(session Session) (string, error) {
	payload, err := json.Marshal(session)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := c.gcm.Seal(nil, nonce, payload, nil)

	combined := append(nonce, ciphertext...)
	return base64.RawURLEncoding.EncodeToString(combined), nil
}

func (c *SessionCodec) Decode(encoded string) (Session, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Session{}, err
	}

	nonceSize := c.gcm.NonceSize()
	if len(raw) < nonceSize {
		return Session{}, errors.New("auth: invalid session cookie")
	}

	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plain, err := c.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return Session{}, err
	}

	var session Session
	if err := json.Unmarshal(plain, &session); err != nil {
		return Session{}, err
	}
	if strings.TrimSpace(session.AccessToken) == "" {
		return Session{}, errors.New("auth: session access token is empty")
	}
	return session, nil
}
