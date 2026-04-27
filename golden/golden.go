// Package golden is sugaar's "approved truth" test helper.
//
// A test captures an actual outcome — the full HTTP exchange, an event
// transcript, anything stringy — and compares it against a committed golden
// file written in plain text. When tests fail, the diff is printed line by
// line so a human can read what changed at a glance. To accept the new
// truth: re-run with -update and review the diff in git.
//
// Files live next to your tests under testdata/ and end in .golden.txt.
//
// Example:
//
//	func TestHomePage(t *testing.T) {
//	    rec := httptest.NewRecorder()
//	    app.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
//	    golden.Assert(t, "home", golden.Response(rec.Result()))
//	}
//
// Run `go test -update ./...` to refresh goldens after an intentional change.
package golden

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Update is set by `go test -update` and causes Assert to overwrite goldens.
var Update = flag.Bool("update", false, "update sugaar golden files")

// Assert compares actual against testdata/<name>.golden.txt. When -update is
// passed it writes the file instead.
func Assert(t testing.TB, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.txt")
	actual = normalize(actual)

	if *Update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("golden mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatalf("golden write: %v", err)
		}
		t.Logf("golden updated: %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden missing: %s\n  run `go test -update` to create it.\n  err: %v", path, err)
	}
	if got := actual; got != string(want) {
		t.Fatalf("golden mismatch in %s\n--- want\n+++ got\n%s\n\nrun `go test -update` to accept.", path, diff(string(want), got))
	}
}

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

// Response renders an *http.Response into the readable golden format:
//
//	< 200 OK
//	< Content-Type: application/json
//	---
//	{"hello":"world"}
//
// Header order is sorted for determinism. JSON bodies are pretty-printed.
// Volatile headers (Date, Set-Cookie expiry) are stripped.
func Response(resp *http.Response) string {
	var b strings.Builder
	fmt.Fprintf(&b, "< %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))

	keys := make([]string, 0, len(resp.Header))
	for k := range resp.Header {
		if isVolatileHeader(k) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		for _, v := range resp.Header[k] {
			fmt.Fprintf(&b, "< %s: %s\n", k, v)
		}
	}

	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(body) > 0 {
		b.WriteString("---\n")
		b.WriteString(prettyBody(resp.Header.Get("Content-Type"), body))
	}
	return b.String()
}

// Request renders a request the same way (for documenting fixtures).
func Request(req *http.Request) string {
	dump, _ := httputil.DumpRequest(req, true)
	out := strings.ReplaceAll(string(dump), "\r\n", "\n")
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fmt.Fprintf(&b, "> %s\n", line)
	}
	return b.String()
}

// Events renders a list of typed events as a transcript. Each event is one
// line: `<type> <pretty-json-data>`.
func Events(items []map[string]any) string {
	var b strings.Builder
	for _, ev := range items {
		typ, _ := ev["type"].(string)
		data := ev["data"]
		if data == nil {
			fmt.Fprintf(&b, "%s\n", typ)
			continue
		}
		j, _ := json.Marshal(data)
		fmt.Fprintf(&b, "%s %s\n", typ, j)
	}
	return b.String()
}

func prettyBody(contentType string, body []byte) string {
	if strings.Contains(contentType, "application/json") {
		var v any
		if err := json.Unmarshal(body, &v); err == nil {
			out, _ := json.MarshalIndent(v, "", "  ")
			return string(out) + "\n"
		}
	}
	if !bytes.HasSuffix(body, []byte("\n")) {
		body = append(body, '\n')
	}
	return string(body)
}

func isVolatileHeader(k string) bool {
	switch http.CanonicalHeaderKey(k) {
	case "Date", "Set-Cookie", "Server":
		return true
	}
	return false
}

// diff returns a minimal line-level diff. Not a real Myers algorithm — just
// readable enough to point at what changed.
func diff(want, got string) string {
	wl := strings.Split(want, "\n")
	gl := strings.Split(got, "\n")
	var b strings.Builder
	max := len(wl)
	if len(gl) > max {
		max = len(gl)
	}
	for i := 0; i < max; i++ {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		switch {
		case w == g:
			fmt.Fprintf(&b, "  %s\n", w)
		case w == "":
			fmt.Fprintf(&b, "+ %s\n", g)
		case g == "":
			fmt.Fprintf(&b, "- %s\n", w)
		default:
			fmt.Fprintf(&b, "- %s\n+ %s\n", w, g)
		}
	}
	return b.String()
}
