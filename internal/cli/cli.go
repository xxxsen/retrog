package cli

import (
	"github.com/spf13/cobra"

	"retrog/internal/cli/cleanbucket"
	"retrog/internal/cli/common"
	"retrog/internal/cli/ensure"
	"retrog/internal/cli/upload"
	"retrog/internal/cli/verify"
)

var rootCmd = &cobra.Command{
	Use:   "retrog",
	Short: "Migrate Pegasus ROMs to S3 and manage metadata",
}

// Execute runs the CLI.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().String(common.ConfigFlag, "", "Path to configuration file")
	rootCmd.AddCommand(upload.NewCommand())
	rootCmd.AddCommand(ensure.NewCommand())
	rootCmd.AddCommand(cleanbucket.NewCommand())
	rootCmd.AddCommand(verify.NewCommand())
}
