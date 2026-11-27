// SPDX-License-Identifier: Apache-2.0

// Package toy is the test definition of the tiny typed keys of the toy and fault
// plugins of the test distribution: toy.store, toy.policy and toy.fault. It is a
// fixture of the launcher's tests: no shipped distribution links it (ADR-059 decision
// 9), and its definition.yaml declares suite revision 0, no suite (ADR-060 decisions
// 28 and 55). toy.spine is a definition of its own (spine/v0), so the cycle test of
// toy-provider, which injects the spine and provides toy.store, mounts no fake of a
// key it provides.
package toy

import "github.com/mutex-solutions/aiops-platform/sdk/kernel"

// Store is what toy.store serves: the id of the plugin that provides it.
type Store struct{ Owner string }

// Policy is what toy.policy serves.
type Policy struct{ Owner string }

// Fault is what toy.fault serves, whichever fault plugin provides it.
type Fault struct{ Owner string }

// The keys. All are class "-" (open) but toy.policy, which is provider-bound, so the
// rogue-provider fault has a bound key to provide.
var (
	StoreKey  = kernel.NewKey[Store]("toy.store", 0)
	PolicyKey = kernel.NewKey[Policy]("toy.policy", 0, kernel.ProviderBound())
	FaultKey  = kernel.NewKey[Fault]("toy.fault", 0)
)
