// Package proto stands for gRPC's proto codec, which registers itself from init.
package proto

import "google.golang.org/grpc/encoding"

type codec struct{}

func (codec) Name() string { return "proto" }

func init() { encoding.RegisterCodecV2(codec{}) }
