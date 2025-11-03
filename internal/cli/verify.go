package cli

import (
	"context"
	"time"

	"github.com/spf13/cobra"

	"retrog/internal/app"
)

func newVerifyCommand() *cobra.Command {
	runnerIface, err := app.ResolveRunner("verify")
	if err != nil {
		panic(err)
	}
	cmdRunner := runnerIface.(*app.VerifyCommand)
	var runner app.IRunner = cmdRunner

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Scan ROMs directory and report duplicate or colliding files",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := commandContext(cmd)
			if err := runner.PreRun(ctx); err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			if err := runner.Run(ctx); err != nil {
				return err
			}
			if err := runner.PostRun(ctx); err != nil {
				return err
			}

			result := cmdRunner.Result()

			printGroups := func(title string, groups []app.DuplicateGroup) {
				if len(groups) == 0 {
					return
				}
				cmd.Println(title)
				for _, group := range groups {
					cmd.Printf("  MD5: %s\n", group.MD5)
					for _, file := range group.Files {
						cmd.Printf("    %s (SHA1: %s)\n", file.Path, file.SHA1)
					}
				}
			}

			printGroups("ROM duplicates (identical files):", result.RomDuplicates)
			printGroups("ROM hash collisions (different SHA1):", result.RomCollisions)
			printGroups("Media duplicates (identical files):", result.MediaDuplicates)
			printGroups("Media hash collisions (different SHA1):", result.MediaCollisions)

			if len(result.RomDuplicates)+len(result.RomCollisions)+len(result.MediaDuplicates)+len(result.MediaCollisions) == 0 {
				cmd.Println("No duplicate or colliding files detected.")
			}

			return nil
		},
	}

	cmdRunner.Init(cmd.Flags())

	return cmd
}
