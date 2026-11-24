// SPDX-License-Identifier: Apache-2.0

package plugingen

// BuildFlags are the flags of every build of a binary of the repository: the tag
// grpcnotrace drops golang.org/x/net/trace, which gRPC links through the signing
// library and whose init registers /debug/requests and /debug/events on
// http.DefaultServeMux (ADR-020 decision 27; ADR-044 decision 21). The builder lists a
// graph and builds with them, make check and CI build with them in GOFLAGS, and so
// does the harness of the test binary.
var BuildFlags = []string{"-tags=grpcnotrace"}
