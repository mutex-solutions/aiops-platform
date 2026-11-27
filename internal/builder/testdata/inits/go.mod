module example.test/inits

go 1.26.0

require (
	github.com/prometheus/client_golang v0.0.0
	google.golang.org/grpc v1.82.1
)

replace (
	github.com/prometheus/client_golang => ./prometheus
	google.golang.org/grpc => ./grpc
)
