// Package sugaartest provides ergonomic helpers for testing sugaar apps.
//
// Typical use with golden assertions:
//
//	c := sugaartest.New(app)
//	resp := c.GET("/users/42")
//	golden.Assert(t, "user_42", golden.Response(resp))
package sugaartest

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/eneko/sugaar"
)

// Client is a tiny wrapper around an *sugaar.App that turns method calls
// into rendered *http.Response objects.
type Client struct {
	App     *sugaar.App
	Headers http.Header
}

// New returns a Client for the given app.
func New(app *sugaar.App) *Client {
	return &Client{App: app, Headers: http.Header{}}
}

// With returns a copy of the client with extra request headers applied.
func (c *Client) With(key, value string) *Client {
	h := c.Headers.Clone()
	h.Set(key, value)
	return &Client{App: c.App, Headers: h}
}

// Do runs an arbitrary request through the app and returns the response.
func (c *Client) Do(req *http.Request) *http.Response {
	for k, vs := range c.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	c.App.ServeHTTP(rec, req)
	return rec.Result()
}

// GET issues a GET to path.
func (c *Client) GET(path string) *http.Response {
	return c.Do(httptest.NewRequest(http.MethodGet, path, nil))
}

// POSTJSON issues a POST with body marshalled to JSON.
func (c *Client) POSTJSON(path string, body any) *http.Response {
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	return c.Do(req)
}

// POSTForm issues a POST with form-encoded values.
func (c *Client) POSTForm(path string, form map[string]string) *http.Response {
	pairs := make([]string, 0, len(form))
	for k, v := range form {
		pairs = append(pairs, k+"="+v)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Join(pairs, "&")))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.Do(req)
}

// Body returns the response body as a string and closes it.
func Body(resp *http.Response) string {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}
