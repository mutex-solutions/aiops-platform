// Package encoding stands for gRPC's codec registry.
package encoding

type CodecV2 interface{ Name() string }

var codecs = map[string]CodecV2{}

func RegisterCodecV2(c CodecV2) { codecs[c.Name()] = c }
