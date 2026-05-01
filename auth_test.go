package sugaar_test

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eneko/sugaar"
	"github.com/eneko/sugaar/sugaartest"
)

func TestAuthStoresIdentity(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	auth := sugaar.AuthenticatorFunc(func(c *sugaar.Context) (*sugaar.Identity, error) {
		return &sugaar.Identity{Subject: "u42", Name: "Ada"}, nil
	})
	app.Use(sugaar.Auth(auth))
	app.GET("/me", func(c *sugaar.Context) error {
		id, ok := c.Identity()
		if !ok {
			return c.String(500, "no identity")
		}
		return c.String(200, id.Subject)
	})

	c := sugaartest.New(app)
	resp := c.GET("/me")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "u42" {
		t.Fatalf("body = %q", body)
	}
}

func TestAuthCustomOnFail(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	auth := sugaar.AuthenticatorFunc(func(c *sugaar.Context) (*sugaar.Identity, error) {
		return nil, errors.New("nope")
	})
	app.Use(sugaar.Auth(auth, sugaar.AuthOnFail(func(c *sugaar.Context, err error) error {
		c.W().Header().Set("X-Auth-Fail", "1")
		return sugaar.Unauthorized("custom: " + err.Error())
	})))
	app.GET("/x", func(c *sugaar.Context) error { return c.String(200, "ok") })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", nil)
	app.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Header().Get("X-Auth-Fail") != "1" {
		t.Fatalf("missing X-Auth-Fail header")
	}
	if !strings.Contains(rec.Body.String(), "custom: nope") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestBasicAuthAuthenticator(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	a := sugaar.BasicAuthAuthenticator(func(user, pass string) (*sugaar.Identity, error) {
		if user != "ada" || pass != "lovelace" {
			return nil, errors.New("bad creds")
		}
		return &sugaar.Identity{Subject: "ada", Name: "Ada Lovelace"}, nil
	})
	app.Use(sugaar.Auth(a))
	app.GET("/x", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	c := sugaartest.New(app)
	if r := c.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 without creds, got %d", r.StatusCode)
	}
	bad := c.With("Authorization", "Basic YWRhOndyb25n")
	if r := bad.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 with bad pass, got %d", r.StatusCode)
	}
	good := c.With("Authorization", "Basic YWRhOmxvdmVsYWNl")
	resp := good.GET("/x")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "ada" {
		t.Fatalf("body = %q", body)
	}
}

func TestBearerAuthAuthenticator(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	a := sugaar.BearerAuthAuthenticator(func(token string) (*sugaar.Identity, error) {
		if token != "s3cret" {
			return nil, errors.New("bad token")
		}
		return &sugaar.Identity{Subject: "agent-1"}, nil
	})
	app.Use(sugaar.Auth(a))
	app.GET("/x", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	c := sugaartest.New(app)
	if r := c.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
	bad := c.With("Authorization", "Bearer wrong")
	if r := bad.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
	good := c.With("Authorization", "Bearer s3cret")
	resp := good.GET("/x")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "agent-1" {
		t.Fatalf("body = %q", body)
	}
}

func TestAPIKeyAuthHeader(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	a := sugaar.APIKeyAuth("X-API-Key", "", func(key string) (*sugaar.Identity, error) {
		if key != "abc" {
			return nil, errors.New("bad key")
		}
		return &sugaar.Identity{Subject: "svc-1"}, nil
	})
	app.Use(sugaar.Auth(a))
	app.GET("/x", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	c := sugaartest.New(app)
	if r := c.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
	good := c.With("X-API-Key", "abc")
	resp := good.GET("/x")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "svc-1" {
		t.Fatalf("body = %q", body)
	}
}

func TestAPIKeyAuthQuery(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	a := sugaar.APIKeyAuth("", "key", func(k string) (*sugaar.Identity, error) {
		if k != "xyz" {
			return nil, errors.New("bad key")
		}
		return &sugaar.Identity{Subject: "svc-2"}, nil
	})
	app.Use(sugaar.Auth(a))
	app.GET("/x", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	c := sugaartest.New(app)
	resp := c.GET("/x?key=xyz")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "svc-2" {
		t.Fatalf("body = %q", body)
	}
}

func TestAnyOf(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	basic := sugaar.BasicAuthAuthenticator(func(u, p string) (*sugaar.Identity, error) {
		if u == "admin" && p == "admin" {
			return &sugaar.Identity{Subject: "admin"}, nil
		}
		return nil, errors.New("bad basic")
	})
	bearer := sugaar.BearerAuthAuthenticator(func(tok string) (*sugaar.Identity, error) {
		if tok == "tok" {
			return &sugaar.Identity{Subject: "token-user"}, nil
		}
		return nil, errors.New("bad bearer")
	})
	app.Use(sugaar.Auth(sugaar.AnyOf(basic, bearer)))
	app.GET("/x", func(c *sugaar.Context) error {
		id, _ := c.Identity()
		return c.String(200, id.Subject)
	})

	c := sugaartest.New(app)

	// neither creds -> 401
	if r := c.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}

	// basic works
	basicReq := c.With("Authorization", "Basic YWRtaW46YWRtaW4=")
	resp := basicReq.GET("/x")
	if resp.StatusCode != 200 || sugaartest.Body(resp) != "admin" {
		t.Fatalf("basic auth failed")
	}

	// bearer works
	bearerReq := c.With("Authorization", "Bearer tok")
	resp = bearerReq.GET("/x")
	if resp.StatusCode != 200 || sugaartest.Body(resp) != "token-user" {
		t.Fatalf("bearer auth failed")
	}
}

func TestAllOf(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	apiKey := sugaar.APIKeyAuth("X-API-Key", "", func(k string) (*sugaar.Identity, error) {
		if k != "k1" {
			return nil, errors.New("bad api key")
		}
		return &sugaar.Identity{Subject: "svc"}, nil
	})
	bearer := sugaar.BearerAuthAuthenticator(func(tok string) (*sugaar.Identity, error) {
		if tok != "t1" {
			return nil, errors.New("bad bearer")
		}
		return &sugaar.Identity{Subject: "svc"}, nil
	})
	app.Use(sugaar.Auth(sugaar.AllOf(apiKey, bearer)))
	app.GET("/x", func(c *sugaar.Context) error {
		return c.String(200, "ok")
	})

	c := sugaartest.New(app)
	// missing both -> 401
	if r := c.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
	// only api key -> 401
	onlyKey := c.With("X-API-Key", "k1")
	if r := onlyKey.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 with only api key, got %d", r.StatusCode)
	}
	// both -> 200
	both := c.With("X-API-Key", "k1").With("Authorization", "Bearer t1")
	if r := both.GET("/x"); r.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}
}

func TestRequireRoles(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(sugaar.BearerAuthAuthenticator(func(tok string) (*sugaar.Identity, error) {
		switch tok {
		case "admin":
			return &sugaar.Identity{Subject: "a", Roles: []string{"admin", "user"}}, nil
		case "user":
			return &sugaar.Identity{Subject: "u", Roles: []string{"user"}}, nil
		default:
			return nil, errors.New("bad token")
		}
	})))
	app.GET("/admin", sugaar.RequireRoles("admin")(func(c *sugaar.Context) error {
		return c.String(200, "admin-only")
	}))

	c := sugaartest.New(app)

	// no auth -> 401
	if r := c.GET("/admin"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}

	// user token -> 403
	userReq := c.With("Authorization", "Bearer user")
	resp := userReq.GET("/admin")
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
	body := sugaartest.Body(resp)
	if !strings.Contains(body, "forbidden_missing_role") {
		t.Fatalf("missing code in body: %s", body)
	}

	// admin token -> 200
	adminReq := c.With("Authorization", "Bearer admin")
	resp = adminReq.GET("/admin")
	if resp.StatusCode != 200 || sugaartest.Body(resp) != "admin-only" {
		t.Fatalf("admin access failed")
	}
}

func TestRequireAnyRole(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.Auth(sugaar.BearerAuthAuthenticator(func(tok string) (*sugaar.Identity, error) {
		switch tok {
		case "editor":
			return &sugaar.Identity{Subject: "e", Roles: []string{"editor"}}, nil
		case "viewer":
			return &sugaar.Identity{Subject: "v", Roles: []string{"viewer"}}, nil
		default:
			return nil, errors.New("bad token")
		}
	})))
	app.GET("/content", sugaar.RequireAnyRole("admin", "editor")(func(c *sugaar.Context) error {
		return c.String(200, "ok")
	}))

	c := sugaartest.New(app)

	// viewer -> 403
	viewer := c.With("Authorization", "Bearer viewer")
	if r := viewer.GET("/content"); r.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", r.StatusCode)
	}

	// editor -> 200
	editor := c.With("Authorization", "Bearer editor")
	if r := editor.GET("/content"); r.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}
}

func TestOldBasicAuthStillWorks(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.Use(sugaar.BasicAuth("ada", "lovelace"))
	app.GET("/x", func(c *sugaar.Context) error {
		user, _ := c.Get("user")
		return c.String(200, user.(string))
	})

	c := sugaartest.New(app)
	if r := c.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
	good := c.With("Authorization", "Basic YWRhOmxvdmVsYWNl")
	resp := good.GET("/x")
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if body := sugaartest.Body(resp); body != "ada" {
		t.Fatalf("body = %q", body)
	}
}

func TestOldBearerAuthStillWorks(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	g := app.Group("/private", sugaar.BearerAuth("s3cret"))
	g.GET("/me", func(c *sugaar.Context) error { return c.String(200, "ok") })

	c := sugaartest.New(app)
	if r := c.GET("/private/me"); r.StatusCode != 401 {
		t.Fatalf("expected 401, got %d", r.StatusCode)
	}
	authed := c.With("Authorization", "Bearer s3cret")
	if r := authed.GET("/private/me"); r.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", r.StatusCode)
	}
}

func TestIdentityHasRoleConstantTime(t *testing.T) {
	id := &sugaar.Identity{Roles: []string{"admin", "user"}}
	if !id.HasRole("admin") {
		t.Fatal("expected HasRole(admin) true")
	}
	if !id.HasRole("user") {
		t.Fatal("expected HasRole(user) true")
	}
	if id.HasRole("superadmin") {
		t.Fatal("expected HasRole(superadmin) false")
	}
}

func TestMustIdentityPanicsWhenMissing(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	app.GET("/x", func(c *sugaar.Context) error {
		_ = c.MustIdentity()
		return c.String(200, "ok")
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	// recoverMiddleware catches the panic and returns 500
	if rec.Code != 500 {
		t.Fatalf("expected 500 after panic, got %d", rec.Code)
	}
}

func TestAuthorizeMiddleware(t *testing.T) {
	app := sugaar.New(sugaar.Options{DisablePprof: true})
	// route with Authorize middleware but *no* Auth middleware before it
	app.GET("/x", sugaar.Authorize(sugaar.AuthorizerFunc(func(c *sugaar.Context, id *sugaar.Identity) error {
		return nil
	}))(func(c *sugaar.Context) error {
		return c.String(200, "ok")
	}))

	c := sugaartest.New(app)
	if r := c.GET("/x"); r.StatusCode != 401 {
		t.Fatalf("expected 401 when no identity present, got %d", r.StatusCode)
	}
}
