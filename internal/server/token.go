package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var errInvalidToken = errors.New("invalid token")

type tokenClaims struct {
	Scope      string   `json:"scope"`
	ExpiresAt  int64    `json:"exp"`
	LibraryIDs []string `json:"library_ids,omitempty"`
	MediaTypes []string `json:"media_types,omitempty"`
}

func signToken(secret string, claims tokenClaims) (string, error) {
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + sig, nil
}

func verifyToken(secret, token string, now time.Time) (tokenClaims, error) {
	var claims tokenClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return claims, errInvalidToken
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(got, want) {
		return claims, errInvalidToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, errInvalidToken
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return claims, errInvalidToken
	}
	if claims.Scope != "catalog" {
		return claims, fmt.Errorf("%w: wrong scope", errInvalidToken)
	}
	if claims.ExpiresAt > 0 && claims.ExpiresAt <= now.Unix() {
		return claims, fmt.Errorf("%w: expired", errInvalidToken)
	}
	return claims, nil
}
