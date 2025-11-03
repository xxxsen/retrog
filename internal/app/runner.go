package app

import (
	"context"

	"github.com/spf13/pflag"
)

// IRunner represents a runnable command in the application layer.
type IRunner interface {
	Name() string
	Init(fst *pflag.FlagSet)
	PreRun(ctx context.Context) error
	Run(ctx context.Context) error
	PostRun(ctx context.Context) error
}
