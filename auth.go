package sugaar

import (
	"crypto/subtle"
	"strings"
)

// identityKey is the Context.Set key for the authenticated Identity.
const identityKey = "sugaar.identity"

// Identity represents an authenticated principal.
type Identity struct {
	Subject string         // unique identifier (user ID, client ID, etc.)
	Name    string         // display name
	Roles   []string       // role slugs
	Claims  map[string]any // extra claims from the auth mechanism
}

// HasRole reports whether the identity possesses the given role.
func (id *Identity) HasRole(role string) bool {
	for _, r := range id.Roles {
		if subtle.ConstantTimeCompare([]byte(r), []byte(role)) == 1 {
			return true
		}
	}
	return false
}

// Authenticator verifies credentials from a request and returns an Identity.
type Authenticator interface {
	Authenticate(c *Context) (*Identity, error)
}

// AuthenticatorFunc is an adapter that lets ordinary functions work as
// Authenticators.
type AuthenticatorFunc func(c *Context) (*Identity, error)

// Authenticate implements the Authenticator interface.
func (f AuthenticatorFunc) Authenticate(c *Context) (*Identity, error) {
	return f(c)
}

// Authorizer checks permissions after authentication.
type Authorizer interface {
	Authorize(c *Context, id *Identity) error
}

// AuthorizerFunc is an adapter for ordinary functions.
type AuthorizerFunc func(c *Context, id *Identity) error

// Authorize implements the Authorizer interface.
func (f AuthorizerFunc) Authorize(c *Context, id *Identity) error {
	return f(c, id)
}

// Identity returns the authenticated identity, if any.
func (c *Context) Identity() (*Identity, bool) {
	v, ok := c.Get(identityKey)
	if !ok {
		return nil, false
	}
	id, ok := v.(*Identity)
	return id, ok
}

// MustIdentity returns the authenticated identity. It panics if no identity is
// present. Safe to call inside handlers wrapped with Auth middleware.
func (c *Context) MustIdentity() *Identity {
	id, ok := c.Identity()
	if !ok {
		panic("sugaar: no identity on context; wrap route with Auth middleware")
	}
	return id
}

// authConfig holds options for the Auth middleware.
type authConfig struct {
	onFail func(c *Context, err error) error
}

// AuthOption customizes the behaviour of Auth middleware.
type AuthOption func(*authConfig)

// AuthOnFail sets a custom handler that is invoked when authentication fails.
// The returned error is passed down the middleware chain.
func AuthOnFail(fn func(c *Context, err error) error) AuthOption {
	return func(cfg *authConfig) {
		cfg.onFail = fn
	}
}

// Auth runs the given Authenticator and, on success, stores the resulting
// Identity on the context via Context.Set. On failure it returns 401
// Unauthorized (or the error produced by an AuthOnFail option).
func Auth(a Authenticator, opts ...AuthOption) Middleware {
	cfg := &authConfig{}
	for _, o := range opts {
		o(cfg)
	}
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			id, err := a.Authenticate(c)
			if err != nil {
				if cfg.onFail != nil {
					return cfg.onFail(c, err)
				}
				msg := err.Error()
				if msg == "" {
					msg = "unauthorized"
				}
				return Unauthorized(msg)
			}
			c.Set(identityKey, id)
			return next(c)
		}
	}
}

// Authorize runs the given Authorizer after authentication. If no identity is
// present it returns 401; if the authorizer rejects it returns 403.
func Authorize(a Authorizer) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			id, ok := c.Identity()
			if !ok {
				return Unauthorized("authentication required")
			}
			if err := a.Authorize(c, id); err != nil {
				return err
			}
			return next(c)
		}
	}
}

// stripBearer extracts the token from an "Authorization: Bearer <token>"
// header. It returns the token and true on success; otherwise empty string
// and false.
func stripBearer(c *Context) (string, bool) {
	h := c.Header("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return h[len(prefix):], true
}
