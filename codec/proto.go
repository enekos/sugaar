// Package codec adds Protobuf and Avro encoding to sugaar.Context, plus
// Hub adapters that publish typed messages.
//
// Protobuf is wire-compatible with gRPC clients hitting the HTTP edge;
// Avro fits Kafka/schema-registry pipelines where agentic events flow
// downstream.
package codec

import (
	"errors"
	"io"

	"github.com/eneko/sugaar"
	"google.golang.org/protobuf/proto"
)

// ProtoContentType is what we read and write for binary protobuf payloads.
const ProtoContentType = "application/x-protobuf"

// Proto writes a protobuf message as a binary response with status code.
func Proto(c *sugaar.Context, status int, m proto.Message) error {
	buf, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	c.W().Header().Set("Content-Type", ProtoContentType)
	c.W().WriteHeader(status)
	_, err = c.W().Write(buf)
	return err
}

// BindProto decodes a protobuf request body into m. Closes the body.
func BindProto(c *sugaar.Context, m proto.Message) error {
	if c.R().Body == nil {
		return errors.New("empty body")
	}
	defer c.R().Body.Close()
	buf, err := io.ReadAll(c.R().Body)
	if err != nil {
		return err
	}
	return proto.Unmarshal(buf, m)
}
