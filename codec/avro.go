package codec

import (
	"errors"
	"io"

	"github.com/eneko/sugaar"
	"github.com/hamba/avro/v2"
)

// AvroContentType is what we read and write for binary Avro payloads. Avro
// requires both ends to share the schema; pre-parse it once with avro.MustParse
// at startup and pass the *avro.Schema to these helpers.
const AvroContentType = "application/avro"

// Avro writes v as Avro-encoded bytes against schema with status code.
func Avro(c *sugaar.Context, status int, schema avro.Schema, v any) error {
	buf, err := avro.Marshal(schema, v)
	if err != nil {
		return err
	}
	c.W().Header().Set("Content-Type", AvroContentType)
	c.W().WriteHeader(status)
	_, err = c.W().Write(buf)
	return err
}

// BindAvro decodes an Avro request body into v against schema.
func BindAvro(c *sugaar.Context, schema avro.Schema, v any) error {
	if c.R().Body == nil {
		return errors.New("empty body")
	}
	defer c.R().Body.Close()
	buf, err := io.ReadAll(c.R().Body)
	if err != nil {
		return err
	}
	return avro.Unmarshal(schema, buf, v)
}

// MustSchema parses schema text or panics. Convenience for package-level vars.
func MustSchema(text string) avro.Schema { return avro.MustParse(text) }
