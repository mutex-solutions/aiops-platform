// Package prometheus stands for the registry of the Prometheus client: its init
// registers its own collector with its default registry.
package prometheus

type Collector interface{ Name() string }

type Registerer interface {
	Register(Collector) error
	MustRegister(...Collector)
}

type Registry struct{ cs []Collector }

func (r *Registry) Register(c Collector) error { r.cs = append(r.cs, c); return nil }

func (r *Registry) MustRegister(cs ...Collector) { r.cs = append(r.cs, cs...) }

var DefaultRegisterer Registerer = &Registry{}

func Register(c Collector) error { return DefaultRegisterer.Register(c) }

func MustRegister(cs ...Collector) { DefaultRegisterer.MustRegister(cs...) }

type process struct{}

func (process) Name() string { return "process" }

func init() { MustRegister(process{}) }
