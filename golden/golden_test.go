package golden_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eneko/sugaar/golden"
)

func TestResponseFormat(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(http.StatusTeapot)
	rec.Body.WriteString(`{"flavor":"earl grey"}`)
	got := golden.Response(rec.Result())
	want := "< 418 I'm a teapot\n" +
		"< Content-Type: application/json\n" +
		"---\n" +
		"{\n  \"flavor\": \"earl grey\"\n}\n"
	if got != want {
		t.Fatalf("unexpected golden output:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestEventsTranscript(t *testing.T) {
	got := golden.Events([]map[string]any{
		{"type": "agent.start"},
		{"type": "token", "data": map[string]string{"v": "hi"}},
	})
	want := "agent.start\ntoken {\"v\":\"hi\"}\n"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
