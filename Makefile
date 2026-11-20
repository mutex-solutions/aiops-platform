# `make check` runs locally what the `test` job of .github/workflows/ci.yml runs.
# Keep the two in step. (The docs also call `make check` the plugin check; that
# part arrives with aiopsctl at S2a.)
MOD := github.com/mutex-solutions/aiops-platform
KC  := go run ./tools/kernelcheck
export GOFLAGS ?= -mod=readonly

# `make s1-suites` runs the s1-suites job: the failure matrix, the recomposition suite and
# the loader smoke test on the built test binary. `make check` skips them (-short).
.PHONY: s1-suites
s1-suites:
	go test -count=1 ./test/failure-matrix/... ./test/recomposition/... ./test/loader-smoke/...

.PHONY: check
check:
	test -z "$$(gofmt -l . | tee /dev/stderr)"
	go build ./...
	go vet ./...
	go test -race -short -count=1 ./...
	cd sdk/kernel && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk/testkit && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk/def && go build ./... && go vet ./... && go test -race -count=1 ./...
	cd sdk/kernel && go test -count=1 -coverprofile=cover.out ./...
	$(KC) modfile sdk/kernel
	$(KC) vocab sdk/kernel tools/kernelcheck/vocabulary.txt
	$(KC) lines sdk/kernel 8200
	$(KC) testctor .
	$(KC) coverage sdk/kernel/cover.out $(MOD)/sdk/kernel
	$(KC) imports .
	$(KC) pluginvet plugins loader boot
	go test -count=1 -coverprofile=cover-boot.out ./boot
	go test -count=1 -coverprofile=cover-loader.out ./loader
	$(KC) lines boot 6300
	$(KC) coverage cover-boot.out $(MOD)/boot
	$(KC) lines loader 1100
	$(KC) vocab loader tools/kernelcheck/vocabulary.txt
	$(KC) coverage cover-loader.out $(MOD)/loader
