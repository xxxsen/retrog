package cli

import (
	"context"

	"github.com/xxxsen/retrog/internal/app"

	"github.com/spf13/cobra"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"
)

var rootCmd = &cobra.Command{
	Use:   "retrog",
	Short: "Migrate Pegasus ROMs to S3 and manage metadata",
}

// Execute runs the CLI.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		logutil.GetLogger(context.Background()).Error("exec cmd failed", zap.Error(err))
		return err
	}
	return nil
}

func init() {
	for _, r := range app.RunnerList() {
		rinst := app.MustResolveRunner(r)
		runner := rinst
		subcmd := &cobra.Command{
			Use:   runner.Name(),
			Short: runner.Desc(),
			RunE: func(cmd *cobra.Command, args []string) error {
				ctx := context.Background()
				if err := runner.PreRun(ctx); err != nil {
					return err
				}
				if err := runner.Run(ctx); err != nil {
					return err
				}
				if err := runner.PostRun(ctx); err != nil {
					return err
				}
				return nil
			},
		}
		runner.Init(subcmd.Flags())
		rootCmd.AddCommand(subcmd)

	}
}
