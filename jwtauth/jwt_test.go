package jwtauth_test

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
	"time"

	"github.com/eneko/sugaar"
	"github.com/eneko/sugaar/jwtauth"
	"github.com/eneko/sugaar/sugaartest"
	"github.com/golang-jwt/jwt/v5"
)

func TestJWTRoundTrip(t *testing.T) {
	secret := []byte("super-secret")
	auth := jwtauth.New(secret)

	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(auth))
	app.GET("/me", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-42",
	}).SignedString(secret)
	if err != nil {
		t.Fatal(err)
	}

	c := sugaartest.New(app)
	authed := c.With("Authorization", "Bearer "+tok)
	resp := authed.GET("/me")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "user-42" {
		t.Fatalf("body = %q", body)
	}
}

func TestJWTExpired(t *testing.T) {
	secret := []byte("super-secret")
	auth := jwtauth.New(secret)

	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(auth))
	app.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u",
		"exp": time.Now().Add(-time.Hour).Unix(),
	}).SignedString(secret)

	c := sugaartest.New(app)
	authed := c.With("Authorization", "Bearer "+tok)
	if r := authed.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 for expired token, got %d", r.StatusCode)
	}
}

func TestJWTWrongSecret(t *testing.T) {
	secret := []byte("super-secret")
	auth := jwtauth.New(secret)

	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(auth))
	app.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u",
	}).SignedString([]byte("wrong-secret"))

	c := sugaartest.New(app)
	authed := c.With("Authorization", "Bearer "+tok)
	if r := authed.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 for wrong secret, got %d", r.StatusCode)
	}
}

func TestJWTRolesMapping(t *testing.T) {
	secret := []byte("super-secret")
	auth := jwtauth.New(secret, jwtauth.WithClaimsMap(func(claims jwt.MapClaims) (*sugaar.Identity, error) {
		id := &sugaar.Identity{Subject: claims["sub"].(string), Claims: make(map[string]any)}
		if roles, ok := claims["roles"].([]any); ok {
			for _, r := range roles {
				if s, ok := r.(string); ok {
					id.Roles = append(id.Roles, s)
				}
			}
		}
		return id, nil
	}))

	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(auth))
	app.GET("/me", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.JSON(200, id.Roles)
	})

	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":   "user-1",
		"roles": []string{"admin", "editor"},
	}).SignedString(secret)

	c := sugaartest.New(app)
	resp := c.With("Authorization", "Bearer "+tok).GET("/me")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body := sugaartest.Body(resp)
	if !strings.Contains(body, "admin") || !strings.Contains(body, "editor") {
		t.Fatalf("expected roles in body, got %s", body)
	}
}

func TestJWTExtractorFromQuery(t *testing.T) {
	secret := []byte("super-secret")
	auth := jwtauth.New(secret, jwtauth.WithExtractor(jwtauth.ExtractorFromQuery("token")))

	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(auth))
	app.GET("/x", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "q-user",
	}).SignedString(secret)

	c := sugaartest.New(app)
	resp := c.GET("/x?token=" + tok)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "q-user" {
		t.Fatalf("body = %q", body)
	}
}

func TestJWTWithIssuerAndAudience(t *testing.T) {
	secret := []byte("super-secret")
	auth := jwtauth.New(secret,
		jwtauth.WithIssuer("test-issuer"),
		jwtauth.WithAudience("test-audience"),
	)

	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(auth))
	app.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	// valid token
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u",
		"iss": "test-issuer",
		"aud": "test-audience",
	}).SignedString(secret)

	c := sugaartest.New(app)
	if r := c.With("Authorization", "Bearer "+tok).GET("/x"); r.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}

	// wrong issuer
	tok2, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u",
		"iss": "wrong-issuer",
		"aud": "test-audience",
	}).SignedString(secret)
	if r := c.With("Authorization", "Bearer "+tok2).GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 for wrong issuer, got %d", r.StatusCode)
	}
}

func TestJWTRS256(t *testing.T) {
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	auth := jwtauth.NewRS256(&privKey.PublicKey)

	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(auth))
	app.GET("/x", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub": "rsa-user",
	}).SignedString(privKey)
	if err != nil {
		t.Fatal(err)
	}

	c := sugaartest.New(app)
	resp := c.With("Authorization", "Bearer "+tok).GET("/x")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "rsa-user" {
		t.Fatalf("body = %q", body)
	}
}

func TestJWTLeeway(t *testing.T) {
	secret := []byte("super-secret")
	// Without leeway, a token that expired 1 second ago is rejected.
	authStrict := jwtauth.New(secret)
	appStrict := sugaar.New(sugaar.Options{DisablePprof: true})
	appStrict.Use(sugaar.Auth(authStrict))
	appStrict.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u",
		"exp": time.Now().Add(-time.Second).Unix(),
	}).SignedString(secret)

	c := sugaartest.New(appStrict)
	if r := c.With("Authorization", "Bearer "+tok).GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 without leeway, got %d", r.StatusCode)
	}

	// With 5-second leeway, the same token is accepted.
	authLax := jwtauth.New(secret, jwtauth.WithLeeway(5*time.Second))
	appLax := sugaar.New(sugaar.Options{DisablePprof: true})
	appLax.Use(sugaar.Auth(authLax))
	appLax.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	c2 := sugaartest.New(appLax)
	if r := c2.With("Authorization", "Bearer "+tok).GET("/x"); r.StatusCode != 200 {
		t.Fatalf("expected 200 with leeway, got %d", r.StatusCode)
	}
}
