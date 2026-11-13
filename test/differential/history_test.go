// SPDX-License-Identifier: Apache-2.0

package differential

import "testing"

// Historical receipts bind their actual historical interpreter, not whichever
// newer instrumented source happens to be live. This explicit allowlist accepts
// no arbitrary hash/path and verifies the preserved bytes themselves. Live-run
// tests still require the current source digest independently.
func historicalOracleDigest(t *testing.T, recorded string) string {
	t.Helper()
	const baseline = "b0f4e4474e4ffba2b5c9316680fa21581c2032d699fb845d0a9c6c5d0d6488b1"
	if recorded != baseline {
		t.Fatalf("unrecognized historical oracle source %q; record a reviewed new identity", recorded)
	}
	return digest(load(t, "oracle/history/replay-"+baseline+".mjs"))
}

func TestHistoricalOracleSourceIdentityIsPreserved(t *testing.T) {
	const baseline = "b0f4e4474e4ffba2b5c9316680fa21581c2032d699fb845d0a9c6c5d0d6488b1"
	if historicalOracleDigest(t, baseline) != baseline {
		t.Fatal("historical interpreter bytes changed")
	}
}
