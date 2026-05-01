package jwtauth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eneko/sugaar"
	"github.com/golang-jwt/jwt/v5"
)

// Extractor pulls a raw JWT string from a request.
type Extractor func(c *sugaar.Context) (string, error)

// ExtractorFromHeader returns an Extractor that reads a Bearer token from the
// given header. If header is empty it defaults to "Authorization".
func ExtractorFromHeader(header string) Extractor {
	if header == "" {
		header = "Authorization"
	}
	const prefix = "Bearer "
	return func(c *sugaar.Context) (string, error) {
		h := c.Header(header)
		if !strings.HasPrefix(h, prefix) {
			return "", errors.New("missing bearer token")
		}
		return h[len(prefix):], nil
	}
}

// ExtractorFromQuery returns an Extractor that reads the token from a query
// parameter.
func ExtractorFromQuery(param string) Extractor {
	return func(c *sugaar.Context) (string, error) {
		tok := c.Query(param)
		if tok == "" {
			return "", errors.New("missing token")
		}
		return tok, nil
	}
}

// ExtractorFromCookie returns an Extractor that reads the token from a cookie.
func ExtractorFromCookie(name string) Extractor {
	return func(c *sugaar.Context) (string, error) {
		tok := c.Cookie(name)
		if tok == "" {
			return "", errors.New("missing token")
		}
		return tok, nil
	}
}

// IdentityMapper converts validated JWT claims into a sugaar.Identity.
type IdentityMapper func(claims jwt.MapClaims) (*sugaar.Identity, error)

// defaultMapper extracts sub as Subject and name as Name. It looks for a
// "roles" claim (string or []string) and populates Identity.Roles.
func defaultMapper(claims jwt.MapClaims) (*sugaar.Identity, error) {
	id := &sugaar.Identity{
		Subject: claimsSubject(claims),
		Claims:  make(map[string]any),
	}
	if name, ok := claims["name"].(string); ok {
		id.Name = name
	}
	id.Roles = claimsRoles(claims)
	for k, v := range claims {
		id.Claims[k] = v
	}
	return id, nil
}

func claimsSubject(claims jwt.MapClaims) string {
	if sub, err := claims.GetSubject(); err == nil {
		return sub
	}
	return ""
}

func claimsRoles(claims jwt.MapClaims) []string {
	switch v := claims["roles"].(type) {
	case string:
		return []string{v}
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, r := range v {
			if s, ok := r.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Option customizes the JWT authenticator.
type Option func(*config)

type config struct {
	extractor Extractor
	mapper    IdentityMapper
	issuer    string
	audience  string
	subject   string
	leeway    time.Duration
	keyFunc   jwt.Keyfunc
}

// WithExtractor sets a custom token extractor.
func WithExtractor(e Extractor) Option {
	return func(c *config) { c.extractor = e }
}

// WithClaimsMap sets a custom mapper from JWT claims to sugaar.Identity.
func WithClaimsMap(m IdentityMapper) Option {
	return func(c *config) { c.mapper = m }
}

// WithIssuer requires the token to have the given issuer ("iss").
func WithIssuer(iss string) Option {
	return func(c *config) { c.issuer = iss }
}

// WithAudience requires the token to have the given audience ("aud").
func WithAudience(aud string) Option {
	return func(c *config) { c.audience = aud }
}

// WithSubject requires the token to have the given subject ("sub").
func WithSubject(sub string) Option {
	return func(c *config) { c.subject = sub }
}

// WithLeeway sets the clock skew tolerance for exp/nbf/iat checks.
func WithLeeway(d time.Duration) Option {
	return func(c *config) { c.leeway = d }
}

// New returns an Authenticator that validates HMAC-signed tokens.
func New(secret []byte, opts ...Option) sugaar.Authenticator {
	return newAuthenticator(func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	}, opts...)
}

// NewRS256 returns an Authenticator that validates RS256-signed tokens.
func NewRS256(pubKey *rsa.PublicKey, opts ...Option) sugaar.Authenticator {
	return newAuthenticator(func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	}, opts...)
}

func newAuthenticator(keyFunc jwt.Keyfunc, opts ...Option) sugaar.Authenticator {
	cfg := &config{
		extractor: ExtractorFromHeader(""),
		mapper:    defaultMapper,
		keyFunc:   keyFunc,
	}
	for _, o := range opts {
		o(cfg)
	}
	return sugaar.AuthenticatorFunc(func(c *sugaar.Context) (*sugaar.Identity, error) {
		raw, err := cfg.extractor(c)
		if err != nil {
			return nil, err
		}
		parserOpts := []jwt.ParserOption{}
		if cfg.leeway > 0 {
			parserOpts = append(parserOpts, jwt.WithLeeway(cfg.leeway))
		}
		if cfg.issuer != "" {
			parserOpts = append(parserOpts, jwt.WithIssuer(cfg.issuer))
		}
		if cfg.audience != "" {
			parserOpts = append(parserOpts, jwt.WithAudience(cfg.audience))
		}
		if cfg.subject != "" {
			parserOpts = append(parserOpts, jwt.WithSubject(cfg.subject))
		}
		token, err := jwt.Parse(raw, cfg.keyFunc, parserOpts...)
		if err != nil {
			return nil, err
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return nil, errors.New("invalid claims")
		}
		return cfg.mapper(claims)
	})
}
