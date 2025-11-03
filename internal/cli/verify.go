package cli

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"
	"github.com/xxxsen/common/logutil"
	"go.uber.org/zap"

	"retrog/internal/app"
)

func newVerifyCommand() *cobra.Command {
	var rootDir string

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Scan ROMs directory and report duplicate or colliding files",
		RunE: func(cmd *cobra.Command, args []string) error {
			if rootDir == "" {
				return errors.New("verify requires --dir")
			}

			ctx := commandContext(cmd)
			logutil.GetLogger(ctx).Info("starting verify", zap.String("dir", rootDir))
			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			runner := app.NewVerifyCommand(rootDir)
			var exec app.IRunner = runner
			if err := exec.Run(ctx); err != nil {
				return err
			}

			result := runner.Result()

			logutil.GetLogger(ctx).Info("verify summary",
				zap.Int("rom_duplicates", len(result.RomDuplicates)),
				zap.Int("rom_collisions", len(result.RomCollisions)),
				zap.Int("media_duplicates", len(result.MediaDuplicates)),
				zap.Int("media_collisions", len(result.MediaCollisions)),
			)

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

	cmd.Flags().StringVar(&rootDir, "dir", "", "ROM root directory to verify")

	return cmd
}
