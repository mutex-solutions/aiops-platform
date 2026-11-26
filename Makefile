# `make check` runs locally what the `test` job of .github/workflows/ci.yml runs.
# Keep the two in step. The docs also call `make check` the plugin check: its last
# step, `make plugin-check`, runs the built aiopsctl's plugin check on every plugin.
MOD := github.com/mutex-solutions/aiops-platform
KC  := go run ./tools/kernelcheck
# Every build has the tag grpcnotrace, which drops golang.org/x/net/trace and the two
# handlers its init registers on http.DefaultServeMux (ADR-020 decision 27); plugingen
# lists the graph of a signed manifest with the same flags (plugingen.BuildFlags).
export GOFLAGS ?= -mod=readonly -tags=grpcnotrace

# `make s1-suites` is the s1-suites job: the failure matrix, the recomposition suite, the
# loader smoke test, the install of a pack into a running process through aiopsctl and
# the outcomes of enable on a running process (ADR-062 decision 18), on the built test
# binary. `make check` skips them (-short).
# -race in GOFLAGS reaches the suites' nested go build, so the binary is race-built too.
# A cell passes whatever the binary prints and the harness kills it, so GORACE writes
# every race report to a file and any report fails the target.
.PHONY: s1-suites
s1-suites:
	r=$$(mktemp -d); s=0; \
	GOFLAGS='$(GOFLAGS) -race' GORACE="log_path=$$r/report" \
		go test -count=1 ./test/failure-matrix/... ./test/recomposition/... ./test/loader-smoke/... || s=$$?; \
	GOFLAGS='$(GOFLAGS) -race' GORACE="log_path=$$r/report" \
		go test -count=1 -run '^(TestInstallAndEnableReachARunningProcess|TestEnableOutcomesOnARunningProcess)$$' ./cmd/aiopsctl || s=$$?; \
	if ls $$r/report.* >/dev/null 2>&1; then cat $$r/report.*; echo 's1-suites: a race-built process reported a data race'; s=1; fi; \
	rm -rf $$r; exit $$s

# `make templates` is the templates job (ADR-060 decisions 24 and 36): through the
# aiopsctl built in the repository, on a throwaway home that trusts it with a key made
# in the test (ADR-020 decision 13), plugin new writes a plugin from the template of each
# registry, which compiles, passes T0 and the generated tests, and fails plugin check
# against the test distribution with exactly one line per obligation. The connector
# template writes a pack of the test distribution's instantiable echoer, whose plugin
# check names the suite binary that this aiopsctl does not carry; the builder job runs
# the case on the aiopsctl of the test release (ADR-060 decision 24, note of
# 2026-11-26). It builds Go in modules of its own, which reach the modules of this
# repository through the module cache, so `make check` (-short) skips it. It fails, and
# does not skip, when the case does not run.
.PHONY: templates
templates:
	go mod download
	out=$$(go test -count=1 -v -run '^TestTemplateJob$$' ./internal/authoring 2>&1); s=$$?; \
	echo "$$out"; [ $$s -eq 0 ] && echo "$$out" | grep -q -- '--- PASS: TestTemplateJob'

# `make builder` is the builder job (ADR-060 decisions 22 and 36; ADR-044): the corpus of
# the licence classifier is what tools/spdxcorpus writes from the pinned SPDX data; the
# cases of the builder that read the tree of the pinned toolchain module, the licence
# report of aiops and aiopsctl among them, with THIRD_PARTY_LICENSES/, the NOTICE texts
# and the MPL-2.0 lines written for each as for an image, and the deny-list scan of the
# whole program; dist build on the local path, then dist sign and dist use of what it
# wrote, which builds two binaries from nothing; and the test release of
# test/release/dist.yaml built in two phases with a throwaway release key (ADR-060
# decision 38). Both modules come from the module proxy into the module cache, so `make
# check` (-short) skips these cases; here they fail, and do not skip, when they do not
# run.
TOOLCHAIN := golang.org/toolchain@v0.0.1-go1.26.8.linux-amd64
SPDXDATA  := github.com/spdx/license-list-data@v3.29.0+incompatible
.PHONY: builder
builder:
	go mod download $(TOOLCHAIN) $(SPDXDATA)
	go run ./tools/spdxcorpus .
	out=$$(go test -count=1 -v -timeout 30m -run '^(TestBoringReading|TestStdNotices|TestLicenceReport|TestGRPCNeedsTheTag|TestToolchainLicenceFiles)$$' ./internal/builder 2>&1 && \
		go test -count=1 -v -timeout 40m -run '^(TestDistBuildSignAndUse|TestTestRelease)$$' ./cmd/aiopsctl 2>&1); s=$$?; \
	echo "$$out"; [ $$s -eq 0 ] && ! echo "$$out" | grep -q -- '--- SKIP' && \
		[ $$(echo "$$out" | grep -c -- '--- PASS') -eq 7 ]

# `make builder-image` is the builder-image job (ADR-060 decisions 22 and 36; ADR-044
# decisions 1, 3 to 6 and 17; ADR-020 decision 13): the release built in two phases from
# the served modules, the builder image written from dist/builder/image.yaml on no base
# layer, the licence report of the release image and of the release aiopsctl, with
# THIRD_PARTY_LICENSES/, the NOTICE texts and the MPL-2.0 lines, the MPL-2.0 release
# check of ADR-044 decision 6 (tools/mplsources), and a site's dist build on the image
# path, which runs that builder image in a container runtime, compared with the local
# path. It needs docker and the module cache of the builder job, so `make
# check` (-short) skips it; here it fails, and does not skip, when it does not run. It
# removes the image it pulls and pushes nothing outside the job.
.PHONY: builder-image
builder-image:
	go mod download $(TOOLCHAIN)
	out=$$(go test -count=1 -v -timeout 40m -run '^TestReleaseAndBuilderImage$$' ./cmd/aiopsctl 2>&1); s=$$?; \
	echo "$$out"; [ $$s -eq 0 ] && echo "$$out" | grep -q -- '--- PASS: TestReleaseAndBuilderImage'

# `make s2a-exit` is the S2a exit (ADR-060 decisions 30, 31 and 36): test/authoring builds
# the test release of test/release/dist.yaml in two phases with a throwaway release key,
# from the modules it serves at a pre-release version in a file-based module proxy, runs
# the pinned registry image, and drives the author's repository, the pack author's
# machine and the clean machine, each as containers on a network of its own that reaches
# the registry and, from the author's repository, the module proxy, which a container
# serves over HTTP; the clean machine and, on the image path, the author's repository
# have a container runtime of their own on that network, a daemon of the job's runtime
# in a privileged container. EXIT_BUILDERS names the path of the clean machine's
# builds, each run in turn: image, the CI job s2a-exit, and local, its local-toolchain
# variant s2a-exit-local. The test binary is race-built, GORACE writes every race report
# of a race-built process to a file, and any report fails the target, as s1-suites does;
# so does a skip. It needs a container runtime, whose daemon programs are on PATH, and
# the module cache of the builder job, so `make check` (-short) skips it. It removes the
# containers, with their volumes, and the networks and images it makes or pulls.
EXIT_BUILDERS ?= image local
.PHONY: s2a-exit
s2a-exit:
	go mod download $(TOOLCHAIN)
	r=$$(mktemp -d); s=0; \
	for b in $(EXIT_BUILDERS); do \
		out=$$(AIOPS_EXIT_BUILDER=$$b GOFLAGS='$(GOFLAGS) -race' GORACE="log_path=$$r/report" \
			go test -count=1 -v -timeout 120m -run '^TestS2aExit$$' ./test/authoring 2>&1); c=$$?; \
		echo "$$out"; \
		if [ $$c -ne 0 ] || echo "$$out" | grep -q -- '--- SKIP' || ! echo "$$out" | grep -q -- '^--- PASS: TestS2aExit'; then \
			echo "s2a-exit: the exit on the $$b path did not pass"; s=1; fi; \
	done; \
	if ls $$r/report.* >/dev/null 2>&1; then cat $$r/report.*; echo 's2a-exit: a race-built process reported a data race'; s=1; fi; \
	rm -rf $$r; exit $$s

# `make cosign` is the cosign job (ADR-060 decision 36; ADR-020 decisions 1, 2 and 4):
# cosign verifies an artifact and a distribution manifest that aiopsctl signs, and
# aiopsctl verifies ones that cosign signs, in key mode, on a registry inside the case.
# cosign is a CI tool that no artifact carries (ADR-060 decision 34): it is built from
# its module at the pin with go install, by the toolchain that the go 1.27 of its go.mod
# needs, each fetched through the module proxy and checked against the checksum
# database, into a temporary directory that the target removes. `make check` skips the
# case, which needs the binary; here it fails, and does not skip, when it does not run.
COSIGN           := github.com/sigstore/cosign/v3/cmd/cosign@v3.1.4
COSIGN_TOOLCHAIN := go1.27.4
.PHONY: cosign
cosign:
	b=$$(mktemp -d); s=0; \
	GOFLAGS= GOTOOLCHAIN=$(COSIGN_TOOLCHAIN) GOBIN=$$b go install -trimpath $(COSIGN) || s=$$?; \
	out=$$(AIOPS_COSIGN=$$b/cosign go test -count=1 -v -run '^TestCosignInterop$$' ./cmd/aiopsctl 2>&1) || s=$$?; \
	echo "$$out"; rm -rf $$b; [ $$s -eq 0 ] && echo "$$out" | grep -q -- '--- PASS: TestCosignInterop'

# `make peer-check` is the peer-credential step of the `test` job (ADR-062 decision 18):
# a client run as root is refused by reload-socket and one run as another user cannot
# open its socket. It needs passwordless sudo, so `make check` does not run it. It fails,
# and does not skip, when it cannot run the cases.
.PHONY: peer-check
peer-check:
	out=$$(AIOPS_PEER_SUDO=1 go test -count=1 -v -run '^TestPeerCredentialsAcrossUsers$$' ./plugins/reload-socket 2>&1); s=$$?; \
	echo "$$out"; [ $$s -eq 0 ] && echo "$$out" | grep -q -- '--- PASS: TestPeerCredentialsAcrossUsers'

# `make plugin-check` is standing rule 2 (ADR-060 decisions 32 and 36; ADR-020 decision
# 13; ADR-061 decision 23), which `make check` runs last and the `test` job runs as a
# step: a Go test builds aiopsctl, makes a throwaway $AIOPS_HOME trust the manifest it
# embeds through a key made in the test, and runs plugin check on every plugin under
# plugins/ and examples/; a T3 boot runs a row of each, in the test binary or in the cli
# profile of that aiopsctl. -short skips both cases, so this target fails, and does not
# skip, when they do not run.
.PHONY: plugin-check
plugin-check:
	out=$$(go test -count=1 -v -timeout 20m -run '^(TestPluginCheckOnEveryPlugin|TestBootOfEachPluginsRow)$$' ./cmd/aiopsctl 2>&1); s=$$?; \
	echo "$$out"; [ $$s -eq 0 ] && ! echo "$$out" | grep -q -- '--- SKIP' && \
		[ $$(echo "$$out" | grep -c -- '^--- PASS: Test') -eq 2 ]

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
	@# ADR-060 decisions 21 and 36; ADR-044 decision 23: the served copies of the go.mod of
	@# the root module and of each SDK module.
	go test -count=1 -run '^TestServedGoMod$$' ./internal/builder
	cd sdk/kernel && go test -count=1 -coverprofile=cover.out ./...
	$(KC) modfile sdk/kernel
	$(KC) modfile sdk $(MOD)/sdk/kernel $(MOD)/sdk/testkit
	$(KC) lines sdk/plugin 2500 provisional
	$(KC) lines internal/plugingen 1500 provisional
	@# ADR-060 decision 35: cmd/aiopsctl and its launcher, internal/aiopsctl, which the
	@# aiopsctl that dist build writes runs too, share the one 2,500 lines of the row of
	@# cmd/aiopsctl; the builder, 3,700 lines since the note of 2026-11-26, provisional and
	@# awaiting the owner.
	$(KC) lines cmd/aiopsctl:internal/aiopsctl 2500 provisional
	$(KC) lines internal/builder 3700 provisional
	@# ADR-060 decision 35 and ADR-061: the operations library, plugins/operations, 4,700
	@# lines since the note of 2026-11-27, provisional and awaiting the owner: the row
	@# package, internal/ skipped, and its internal package, with the client of
	@# reload-socket (ADR-062 decision 16), each.
	$(KC) lines plugins/operations 4700 provisional internal
	$(KC) lines plugins/operations/internal/operations 4700 provisional
	go run ./tools/pluginapi .
	$(KC) vocab sdk/kernel tools/kernelcheck/vocabulary.txt
	$(KC) lines sdk/kernel 8200
	$(KC) testctor .
	$(KC) coverage sdk/kernel/cover.out $(MOD)/sdk/kernel
	@# ADR-054 decision 13: the import boundary U2 reads the upstream register. ADR-060
	@# decision 28: no package under test/ or examples/ in cmd/aiops or cmd/aiopsctl. ADR-061
	@# decision 21: no part of the toolbox (plugingen, the analyzer, the templates, the
	@# suites) in cmd/aiops. ADR-044, Consequences: no package of golang.org/x/mod in
	@# cmd/aiops but semver and sumdb/note, which sigstore-go links; the module zip is the
	@# toolbox's.
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
	@# ADR-060 decision 35 and ADR-061: the operations definition, 800 lines; its fake and
	@# its suite, 1,300 each since the note of 2026-11-25.
	$(KC) lines sdk/def/operations/v0 800 provisional fake conformance
	$(KC) lines sdk/def/operations/v0/fake 1300 provisional
	$(KC) lines sdk/def/operations/v0/conformance 1300 provisional
	@# ADR-060 decision 32, standing rule 2: plugin check and a T3 boot of every plugin.
	$(MAKE) --no-print-directory plugin-check
