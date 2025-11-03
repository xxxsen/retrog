package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"retrog/internal/app"
)

func newCleanBucketCommand() *cobra.Command {
	cmdRunner := app.NewCleanBucketCommand(nil)
	var runner app.IRunner = cmdRunner

	cmd := &cobra.Command{
		Use:   "clean-bucket",
		Short: "Remove all objects from the configured ROM and media buckets",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := getConfig(cmd)
			if err != nil {
				return err
			}

			ctx := commandContext(cmd)
			cmdRunner.SetConfig(cfg)
			if err := runner.PreRun(ctx); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			if err := runner.Run(ctx); err != nil {
				return err
			}

			if err := runner.PostRun(ctx); err != nil {
				return err
			}

			cmd.Println("Buckets cleaned successfully")
			return nil
		},
	}

	cmdRunner.Init(cmd.Flags())

	return cmd
}
