package app

import (
	"context"
)

// IRunner represents a runnable command in the application layer.
type IRunner interface {
	Run(ctx context.Context) error
}
