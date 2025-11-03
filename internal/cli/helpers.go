package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func commandContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
