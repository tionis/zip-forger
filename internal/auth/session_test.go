package auth

import (
	"testing"
	"time"
)

func TestSessionCodecRoundTrip(t *testing.T) {
	codec, err := NewSessionCodec("test-secret")
	if err != nil {
		t.Fatalf("NewSessionCodec failed: %v", err)
	}

	input := Session{
		AccessToken: "abc123",
		TokenType:   "token",
		ExpiresAt:   time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}

	encoded, err := codec.Encode(input)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	output, err := codec.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if output.AccessToken != input.AccessToken {
		t.Fatalf("unexpected access token: %q", output.AccessToken)
	}
	if output.TokenType != input.TokenType {
		t.Fatalf("unexpected token type: %q", output.TokenType)
	}
	if !output.ExpiresAt.Equal(input.ExpiresAt) {
		t.Fatalf("unexpected expiresAt: got=%v want=%v", output.ExpiresAt, input.ExpiresAt)
	}
}
