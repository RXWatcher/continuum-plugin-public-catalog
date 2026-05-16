package server

import (
	"testing"
	"time"
)

func TestTokenRoundTrip(t *testing.T) {
	now := time.Unix(100, 0)
	token, err := signToken("01234567890123456789012345678901", tokenClaims{
		Scope:      "catalog",
		ExpiresAt:  now.Add(time.Hour).Unix(),
		LibraryIDs: []string{"lib-1"},
		MediaTypes: []string{"movie"},
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	got, err := verifyToken("01234567890123456789012345678901", token, now)
	if err != nil {
		t.Fatalf("verifyToken: %v", err)
	}
	if got.Scope != "catalog" || got.LibraryIDs[0] != "lib-1" || got.MediaTypes[0] != "movie" {
		t.Fatalf("bad claims: %+v", got)
	}
}

func TestVerifyTokenRejectsExpired(t *testing.T) {
	token, err := signToken("01234567890123456789012345678901", tokenClaims{
		Scope:     "catalog",
		ExpiresAt: time.Unix(100, 0).Unix(),
	})
	if err != nil {
		t.Fatalf("signToken: %v", err)
	}
	if _, err := verifyToken("01234567890123456789012345678901", token, time.Unix(101, 0)); err == nil {
		t.Fatal("expected expired token to fail")
	}
}
