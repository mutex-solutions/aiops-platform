module github.com/mutex-solutions/aiops-platform/sdk/testkit

go 1.26.0

toolchain go1.26.8

require github.com/mutex-solutions/aiops-platform/sdk/kernel v0.0.0

require go.uber.org/goleak v1.3.0

replace github.com/mutex-solutions/aiops-platform/sdk/kernel => ../kernel
