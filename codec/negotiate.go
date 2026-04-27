package codec

import (
	"strings"

	"github.com/eneko/sugaar"
	"github.com/hamba/avro/v2"
	"google.golang.org/protobuf/proto"
)

// Render picks the response encoder from the client's Accept header.
// Supports JSON (default), application/x-protobuf, and application/avro.
//
// payloads MUST satisfy the chosen encoder's input type:
//   - JSON  -> any json.Marshaler / map / struct
//   - proto -> proto.Message
//   - avro  -> any value compatible with the schema (passed alongside)
//
// Use AvroSchema(schema) to pin a schema for negotiated Avro responses.
func Render(c *sugaar.Context, status int, payload any, opts ...RenderOpt) error {
	o := renderOptions{}
	for _, fn := range opts {
		fn(&o)
	}
	switch chooseEncoder(c.Header("Accept")) {
	case encProto:
		if m, ok := payload.(proto.Message); ok {
			return Proto(c, status, m)
		}
	case encAvro:
		if o.avroSchema != nil {
			return Avro(c, status, o.avroSchema, payload)
		}
	}
	return c.JSON(status, payload)
}

// RenderOpt customises Render.
type RenderOpt func(*renderOptions)

type renderOptions struct {
	avroSchema avro.Schema
}

// AvroSchema lets Render emit Avro when the client asks for it.
func AvroSchema(s avro.Schema) RenderOpt {
	return func(o *renderOptions) { o.avroSchema = s }
}

type encoder int

const (
	encJSON encoder = iota
	encProto
	encAvro
)

func chooseEncoder(accept string) encoder {
	// Cheap, allocation-free precedence: explicit binary types beat */*.
	for _, part := range strings.Split(accept, ",") {
		t := strings.TrimSpace(part)
		if i := strings.IndexByte(t, ';'); i >= 0 {
			t = t[:i]
		}
		switch t {
		case ProtoContentType, "application/protobuf":
			return encProto
		case AvroContentType:
			return encAvro
		}
	}
	return encJSON
}
