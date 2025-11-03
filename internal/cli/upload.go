package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"retrog/internal/app"
)

func newUploadCommand() *cobra.Command {
	runnerIface, err := app.ResolveRunner("upload")
	if err != nil {
		panic(err)
	}
	cmdRunner := runnerIface.(*app.UploadCommand)
	var runner app.IRunner = cmdRunner

	cmd := &cobra.Command{
		Use:   "upload",
		Short: "Upload ROMs and media to object storage and emit metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := commandContext(cmd)
			if _, err := getConfig(cmd); err != nil {
				return err
			}

			if err := runner.PreRun(ctx); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			if err := runner.Run(ctx); err != nil {
				return err
			}
			return runner.PostRun(ctx)
		},
	}

	cmdRunner.Init(cmd.Flags())

	return cmd
}
