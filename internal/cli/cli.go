package cli

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "retrog",
	Short: "Migrate Pegasus ROMs to S3 and manage metadata",
}

// Execute runs the CLI.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().String(ConfigFlag, "", "Path to configuration file")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		_, err := ensureConfig(cmd)
		return err
	}
	rootCmd.AddCommand(newUploadCommand())
	rootCmd.AddCommand(newEnsureCommand())
	rootCmd.AddCommand(newCleanBucketCommand())
	rootCmd.AddCommand(newVerifyCommand())
}
