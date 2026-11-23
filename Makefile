# `make check` runs locally what the `test` job of .github/workflows/ci.yml runs.
# Keep the two in step. (The docs also call `make check` the plugin check; that
# part arrives with aiopsctl at S2a.)
MOD := github.com/mutex-solutions/aiops-platform
KC  := go run ./tools/kernelcheck
export GOFLAGS ?= -mod=readonly

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

.PHONY: check
check:
	test -z "$$(gofmt -l . | tee /dev/stderr)"
	go build ./...
	go vet ./...
	go test -race -short -count=1 ./...
	cd sdk/kernel && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk/testkit && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk/kernel && go test -count=1 -coverprofile=cover.out ./...
	$(KC) modfile sdk/kernel
	$(KC) modfile sdk $(MOD)/sdk/kernel $(MOD)/sdk/testkit
	$(KC) lines sdk/plugin 2500 provisional
	go run ./tools/pluginapi .
	$(KC) vocab sdk/kernel tools/kernelcheck/vocabulary.txt
	$(KC) lines sdk/kernel 8200
	$(KC) testctor .
	$(KC) coverage sdk/kernel/cover.out $(MOD)/sdk/kernel
	$(KC) imports .
	$(KC) pluginvet plugins loader boot
	go test -count=1 -coverprofile=cover-boot.out ./boot
	go test -count=1 -coverprofile=cover-loader.out ./loader
	$(KC) lines boot 6800
	$(KC) coverage cover-boot.out $(MOD)/boot
	$(KC) lines loader 1300
	$(KC) vocab loader tools/kernelcheck/vocabulary.txt
	$(KC) coverage cover-loader.out $(MOD)/loader
	@# ADR-060 decision 35: each toy definition package, its fake and its suite, 800 each.
	for d in echoers stampers; do \
		$(KC) lines sdk/def/$$d/v0 800 provisional fake conformance && \
		$(KC) lines sdk/def/$$d/v0/fake 800 provisional && \
		$(KC) lines sdk/def/$$d/v0/conformance 800 provisional || exit 1; \
	done
