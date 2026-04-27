package codec_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eneko/sugaar"
	"github.com/eneko/sugaar/codec"
	"github.com/hamba/avro/v2"
	"google.golang.org/protobuf/proto"
	tspb "google.golang.org/protobuf/types/known/timestamppb"
)

func nowFromTest() time.Time { return time.Unix(1700000000, 0).UTC() }
func protoMarshal(m proto.Message) ([]byte, error) {
	return proto.Marshal(m)
}

func newApp() *sugaar.App { return sugaar.New(sugaar.Options{DisablePprof: true}) }

func TestProtoRoundTrip(t *testing.T) {
	app := newApp()
	app.POST("/echo", func(c *sugaar.Context) error {
		var in tspb.Timestamp
		if err := codec.BindProto(c, &in); err != nil {
			return err
		}
		return codec.Proto(c, http.StatusOK, &in)
	})

	body, err := protoMarshal(tspb.New(nowFromTest()))
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/echo", bytes.NewReader(body))
	req.Header.Set("Content-Type", codec.ProtoContentType)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != codec.ProtoContentType {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
}

var avroSchema = codec.MustSchema(`{
  "type":"record","name":"Tick","fields":[
    {"name":"n","type":"long"},
    {"name":"msg","type":"string"}
  ]
}`)

type tick struct {
	N   int64  `avro:"n"`
	Msg string `avro:"msg"`
}

func TestAvroRoundTrip(t *testing.T) {
	app := newApp()
	app.POST("/echo", func(c *sugaar.Context) error {
		var in tick
		if err := codec.BindAvro(c, avroSchema, &in); err != nil {
			return err
		}
		return codec.Avro(c, http.StatusOK, avroSchema, in)
	})

	in := tick{N: 7, Msg: "hi"}
	body, err := avro.Marshal(avroSchema, in)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/echo", bytes.NewReader(body))
	req.Header.Set("Content-Type", codec.AvroContentType)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body=%q", rec.Code, rec.Body.String())
	}
	var out tick
	if err := avro.Unmarshal(avroSchema, rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("out = %+v, want %+v", out, in)
	}
}

func TestRenderNegotiates(t *testing.T) {
	app := newApp()
	app.GET("/it", func(c *sugaar.Context) error {
		return codec.Render(c, 200, tick{N: 1, Msg: "x"}, codec.AvroSchema(avroSchema))
	})

	t.Run("json default", func(t *testing.T) {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest("GET", "/it", nil))
		if got := rec.Header().Get("Content-Type"); got == "" || got[0] != 'a' {
			// JSON; just check 200
			if rec.Code != 200 {
				t.Fatalf("status = %d", rec.Code)
			}
		}
	})

	t.Run("avro accept", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/it", nil)
		req.Header.Set("Accept", codec.AvroContentType)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		if got := rec.Header().Get("Content-Type"); got != codec.AvroContentType {
			t.Fatalf("content-type = %q", got)
		}
	})
}
