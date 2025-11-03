package app

import (
	"fmt"
	"sort"
)

var runnerRegistry = map[string]func() IRunner{}

// RegisterRunner registers a runner factory by name.
func RegisterRunner(name string, factory func() IRunner) {
	runnerRegistry[name] = factory
}

// ResolveRunner returns a new runner instance for the given name.
func ResolveRunner(name string) (IRunner, error) {
	factory, ok := runnerRegistry[name]
	if !ok {
		return nil, fmt.Errorf("runner %s not registered", name)
	}
	return factory(), nil
}

func MustResolveRunner(name string) IRunner {
	r, err := ResolveRunner(name)
	if err != nil {
		panic(err)
	}
	return r
}

func RunnerList() []string {
	rs := make([]string, 0, len(runnerRegistry))
	for k := range runnerRegistry {
		rs = append(rs, k)
	}
	sort.Strings(rs)
	return rs
}
