package sugaar

import (
	"crypto/subtle"
	"errors"
)

// ---------------------------------------------------------------------------
// Built-in Authenticators
// ---------------------------------------------------------------------------

// BasicAuthAuthenticator returns an Authenticator that reads HTTP Basic
// credentials and delegates verification to the supplied function. The
// function receives the username and password and should return an Identity
// on success or an error on failure.
func BasicAuthAuthenticator(verify func(user, pass string) (*Identity, error)) Authenticator {
	return AuthenticatorFunc(func(c *Context) (*Identity, error) {
		u, p, ok := c.R().BasicAuth()
		if !ok {
			c.W().Header().Set("WWW-Authenticate", `Basic realm="sugaar"`)
			return nil, errors.New("missing basic auth credentials")
		}
		id, err := verify(u, p)
		if err != nil {
			c.W().Header().Set("WWW-Authenticate", `Basic realm="sugaar"`)
			return nil, err
		}
		return id, nil
	})
}

// BearerAuthAuthenticator returns an Authenticator that reads an
// "Authorization: Bearer <token>" header and delegates verification to the
// supplied function.
func BearerAuthAuthenticator(verify func(token string) (*Identity, error)) Authenticator {
	return AuthenticatorFunc(func(c *Context) (*Identity, error) {
		tok, ok := stripBearer(c)
		if !ok {
			return nil, errors.New("missing bearer token")
		}
		id, err := verify(tok)
		if err != nil {
			return nil, err
		}
		return id, nil
	})
}

// StaticBearerAuth returns an Authenticator that performs constant-time
// comparison against a static map of tokens to identities.
func StaticBearerAuth(tokens map[string]*Identity) Authenticator {
	return AuthenticatorFunc(func(c *Context) (*Identity, error) {
		tok, ok := stripBearer(c)
		if !ok {
			return nil, errors.New("missing bearer token")
		}
		got := []byte(tok)
		for t, id := range tokens {
			if subtle.ConstantTimeCompare(got, []byte(t)) == 1 {
				return id, nil
			}
		}
		return nil, errors.New("invalid bearer token")
	})
}

// APIKeyAuth returns an Authenticator that looks for an API key in a request
// header or query parameter (whichever is non-empty). If both header and query
// names are empty, header defaults to "X-API-Key".
func APIKeyAuth(header, query string, verify func(key string) (*Identity, error)) Authenticator {
	if header == "" && query == "" {
		header = "X-API-Key"
	}
	return AuthenticatorFunc(func(c *Context) (*Identity, error) {
		var key string
		if header != "" {
			key = c.Header(header)
		}
		if key == "" && query != "" {
			key = c.Query(query)
		}
		if key == "" {
			return nil, errors.New("missing api key")
		}
		id, err := verify(key)
		if err != nil {
			return nil, err
		}
		return id, nil
	})
}

// AnyOf returns an Authenticator that tries each supplied authenticator in
// order and returns the first successful identity. If all fail, the error from
// the last authenticator is returned.
func AnyOf(auths ...Authenticator) Authenticator {
	return AuthenticatorFunc(func(c *Context) (*Identity, error) {
		var lastErr error
		for _, a := range auths {
			id, err := a.Authenticate(c)
			if err == nil {
				return id, nil
			}
			lastErr = err
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, errors.New("authentication required")
	})
}

// AllOf returns an Authenticator that requires every supplied authenticator to
// succeed. The returned Identity is the one produced by the *last*
// authenticator. Useful for multi-factor scenarios (e.g. mTLS + Bearer).
func AllOf(auths ...Authenticator) Authenticator {
	return AuthenticatorFunc(func(c *Context) (*Identity, error) {
		var id *Identity
		for _, a := range auths {
			var err error
			id, err = a.Authenticate(c)
			if err != nil {
				return nil, err
			}
		}
		return id, nil
	})
}

// ---------------------------------------------------------------------------
// Built-in Authorizers
// ---------------------------------------------------------------------------

// roleAuthorizer is an Authorizer that checks role membership.
type roleAuthorizer struct {
	roles      []string
	requireAll bool
}

// Authorize implements the Authorizer interface.
func (ra *roleAuthorizer) Authorize(_ *Context, id *Identity) error {
	if id == nil {
		return Unauthorized("authentication required")
	}
	if ra.requireAll {
		for _, r := range ra.roles {
			if !id.HasRole(r) {
				return Forbidden("insufficient privileges").WithCode("forbidden_missing_role")
			}
		}
		return nil
	}
	// require any
	for _, r := range ra.roles {
		if id.HasRole(r) {
			return nil
		}
	}
	return Forbidden("insufficient privileges").WithCode("forbidden_missing_role")
}

// RequireRoles returns middleware that requires the authenticated identity to
// have *all* of the given roles. If no identity is present it returns 401; if
// roles are missing it returns 403.
func RequireRoles(roles ...string) Middleware {
	return Authorize(&roleAuthorizer{roles: roles, requireAll: true})
}

// RequireAnyRole returns middleware that requires the authenticated identity to
// have at least one of the given roles. If no identity is present it returns
// 401; if none match it returns 403.
func RequireAnyRole(roles ...string) Middleware {
	return Authorize(&roleAuthorizer{roles: roles, requireAll: false})
}
