# `make check` runs locally what the `test` job of .github/workflows/ci.yml runs.
# Keep the two in step. (The docs also call `make check` the plugin check; that
# part arrives with aiopsctl at S2a.)
MOD := github.com/mutex-solutions/aiops-platform
KC  := go run ./tools/kernelcheck
# Every build has the tag grpcnotrace, which drops golang.org/x/net/trace and the two
# handlers its init registers on http.DefaultServeMux (ADR-020 decision 27); plugingen
# lists the graph of a signed manifest with the same flags (plugingen.BuildFlags).
export GOFLAGS ?= -mod=readonly -tags=grpcnotrace

# `make s1-suites` is the s1-suites job: the failure matrix, the recomposition suite and
# the loader smoke test on the built test binary. `make check` skips them (-short).
# -race in GOFLAGS reaches the suites' nested go build, so the binary is race-built too.
# A cell passes whatever the binary prints and the harness kills it, so GORACE writes
# every race report to a file and any report fails the target.
.PHONY: s1-suites
s1-suites:
	r=$$(mktemp -d); s=0; \
	GOFLAGS='$(GOFLAGS) -race' GORACE="log_path=$$r/report" \
		go test -count=1 ./test/failure-matrix/... ./test/recomposition/... ./test/loader-smoke/... || s=$$?; \
	if ls $$r/report.* >/dev/null 2>&1; then cat $$r/report.*; echo 's1-suites: a race-built process reported a data race'; s=1; fi; \
	rm -rf $$r; exit $$s

# `make templates` is the templates job (ADR-060 decisions 24 and 36): a plugin
# generated from the template of each registry compiles, passes T0 and the generated
# tests, and fails conformance with exactly one line per obligation. It builds Go in
# modules of its own, which reach the modules of this repository through the module
# cache, so `make check` (-short) skips it. It fails, and does not skip, when the case
# does not run.
.PHONY: templates
templates:
	go mod download
	out=$$(go test -count=1 -v -run '^TestTemplateJob$$' ./internal/authoring 2>&1); s=$$?; \
	echo "$$out"; [ $$s -eq 0 ] && echo "$$out" | grep -q -- '--- PASS: TestTemplateJob'

# `make peer-check` is the peer-credential step of the `test` job (ADR-062 decision 18):
# a client run as root is refused by reload-socket and one run as another user cannot
# open its socket. It needs passwordless sudo, so `make check` does not run it. It fails,
# and does not skip, when it cannot run the cases.
.PHONY: peer-check
peer-check:
	out=$$(AIOPS_PEER_SUDO=1 go test -count=1 -v -run '^TestPeerCredentialsAcrossUsers$$' ./plugins/reload-socket 2>&1); s=$$?; \
	echo "$$out"; [ $$s -eq 0 ] && echo "$$out" | grep -q -- '--- PASS: TestPeerCredentialsAcrossUsers'

.PHONY: check
check:
	test -z "$$(gofmt -l . | tee /dev/stderr)"
	go build ./...
	go vet ./...
	go test -race -short -count=1 ./...
	cd sdk/kernel && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk/testkit && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk && go test -count=1 ./remote/handshake
	cd sdk/kernel && go test -count=1 -coverprofile=cover.out ./...
	$(KC) modfile sdk/kernel
	$(KC) modfile sdk $(MOD)/sdk/kernel $(MOD)/sdk/testkit
	$(KC) lines sdk/plugin 2500 provisional
	$(KC) lines internal/plugingen 1500 provisional
	$(KC) lines cmd/aiopsctl 2500 provisional
	@# ADR-060 decision 35 and ADR-061: the operations library, plugins/operations, 3,500
	@# lines, provisional: the row package, internal/ skipped, and its internal package,
	@# with the client of reload-socket (ADR-062 decision 16), each.
	$(KC) lines plugins/operations 3500 provisional internal
	$(KC) lines plugins/operations/internal/operations 3500 provisional
	go run ./tools/pluginapi .
	$(KC) vocab sdk/kernel tools/kernelcheck/vocabulary.txt
	$(KC) lines sdk/kernel 8200
	$(KC) testctor .
	$(KC) coverage sdk/kernel/cover.out $(MOD)/sdk/kernel
	@# ADR-054 decision 13: the import boundary U2 reads the upstream register. ADR-060
	@# decision 28: no package under test/ or examples/ in cmd/aiops or cmd/aiopsctl.
	$(KC) imports . tools/upstream-components.yaml
	$(KC) pluginvet plugins loader boot
	go run ./tools/deflint .
	@# ADR-060 decisions 11, 20, 28 and 36: manifest parity of every plugin.yaml and its generated tests,
	@# and of the signed distribution manifest of each binary built in the repository; the
	@# release serves no definition outside sdk/def.
	go run ./tools/plugingen .
	@# ADR-054 decisions 12, 14, 15 and 22, ADR-060 decision 51: the register check U7,
	@# the name lint U1 and suite neutrality U3.
	go run ./tools/register .
	go test -count=1 -coverprofile=cover-boot.out ./boot
	go test -count=1 -coverprofile=cover-loader.out ./loader
	@# ADR-060 decision 35: boot's provisional cap during S2a (the manifest reader, ADR-048 decision 18),
	@# 10,500 since its note of 2026-11-24.
	$(KC) lines boot 10500 provisional
	$(KC) vocab boot tools/kernelcheck/vocabulary-boot.txt
	$(KC) coverage cover-boot.out $(MOD)/boot
	$(KC) lines loader 1300
	$(KC) vocab loader tools/kernelcheck/vocabulary-loader.txt
	$(KC) coverage cover-loader.out $(MOD)/loader
	@# ADR-060 decision 35: each toy definition package, its fake and its suite, 800 each.
	for d in echoers stampers; do \
		$(KC) lines sdk/def/$$d/v0 800 provisional fake conformance && \
		$(KC) lines sdk/def/$$d/v0/fake 800 provisional && \
		$(KC) lines sdk/def/$$d/v0/conformance 800 provisional || exit 1; \
	done
	$(KC) lines sdk/def/clock/v0/conformance 800 provisional
	$(KC) lines sdk/def/composition/v0/conformance 800 provisional
	$(KC) lines sdk/def/configrefs/v0/conformance 800 provisional
	$(KC) lines sdk/def/reload/v0/conformance 800 provisional
	$(KC) lines sdk/def/launch/v0/conformance 800 provisional
	@# ADR-060 decision 35 and ADR-061, Consequences: the commands definition, its fake and its
	@# suite, 800 lines each; the cli row and each command row, 1,500 each.
	$(KC) lines sdk/def/commands/v0 800 provisional fake conformance
	$(KC) lines sdk/def/commands/v0/fake 800 provisional
	$(KC) lines sdk/def/commands/v0/conformance 800 provisional
	for p in cli command-profile command-plugin command-trust command-dist command-key; do \
		$(KC) lines plugins/$$p 1500 provisional || exit 1; \
	done
	@# ADR-060 decision 35 and ADR-061: the operations definition, its fake and its suite,
	@# 800 each.
	$(KC) lines sdk/def/operations/v0 800 provisional fake conformance
	$(KC) lines sdk/def/operations/v0/fake 800 provisional
	$(KC) lines sdk/def/operations/v0/conformance 800 provisional
