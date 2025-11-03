package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"retrog/internal/app"
)

func newCleanBucketCommand() *cobra.Command {
	runnerIface, err := app.ResolveRunner("clean-bucket")
	if err != nil {
		panic(err)
	}
	cmdRunner := runnerIface.(*app.CleanBucketCommand)
	var runner app.IRunner = cmdRunner

	cmd := &cobra.Command{
		Use:   "clean-bucket",
		Short: "Remove all objects from the configured ROM and media buckets",
		RunE: func(cmd *cobra.Command, args []string) error {
			if _, err := getConfig(cmd); err != nil {
				return err
			}

			ctx := commandContext(cmd)
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
