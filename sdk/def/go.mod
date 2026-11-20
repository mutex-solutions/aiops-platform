module github.com/mutex-solutions/aiops-platform/sdk/def

go 1.26.0

toolchain go1.26.8

require github.com/mutex-solutions/aiops-platform/sdk/kernel v0.0.0

// sdk/testkit is required by the _test.go files only (07-c4-plugin-sdk.md:28,
// :1407). No non-test package of this module imports it, so go list -deps ./...
// does not list it.
require github.com/mutex-solutions/aiops-platform/sdk/testkit v0.0.0

replace (
	github.com/mutex-solutions/aiops-platform/sdk/kernel => ../kernel
	github.com/mutex-solutions/aiops-platform/sdk/testkit => ../testkit
)
