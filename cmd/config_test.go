package main

import (
	"os"
	"testing"

	"github.com/International-Combat-Archery-Alliance/voting-api/api"
)

func TestJWKSURLForEnv(t *testing.T) {
	const localOnHost = "http://localhost:3001/login/.well-known/jwks.json"
	const prod = "https://api.icaa.world/login/.well-known/jwks.json"

	t.Run("override wins in both environments", func(t *testing.T) {
		t.Setenv("LOGIN_JWKS_URL", "http://override.test/jwks.json")
		if got := jwksURLForEnv(api.LOCAL); got != "http://override.test/jwks.json" {
			t.Fatalf("jwksURLForEnv(LOCAL) = %q, want override", got)
		}
		if got := jwksURLForEnv(api.PROD); got != "http://override.test/jwks.json" {
			t.Fatalf("jwksURLForEnv(PROD) = %q, want override", got)
		}
	})

	t.Run("prod", func(t *testing.T) {
		t.Setenv("LOGIN_JWKS_URL", "")
		if got := jwksURLForEnv(api.PROD); got != prod {
			t.Fatalf("jwksURLForEnv(PROD) = %q, want %q", got, prod)
		}
	})

	t.Run("local on host", func(t *testing.T) {
		if _, err := os.Stat("/.dockerenv"); err == nil {
			t.Skip("inside a container: the local host is not localhost")
		}
		t.Setenv("LOGIN_JWKS_URL", "")
		if got := jwksURLForEnv(api.LOCAL); got != localOnHost {
			t.Fatalf("jwksURLForEnv(LOCAL) = %q, want %q", got, localOnHost)
		}
	})
}
