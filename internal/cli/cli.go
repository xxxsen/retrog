package cli

import (
	"context"

	"retrog/internal/app"
	appdb "retrog/internal/db"
	"retrog/internal/storage"

	"github.com/spf13/cobra"
	"github.com/xxxsen/common/database"
	"github.com/xxxsen/common/database/sqlite"
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
	var cfg string
	rootCmd.PersistentFlags().StringVar(&cfg, "config", "", "Path to configuration file")
	for _, r := range app.RunnerList() {
		rinst := app.MustResolveRunner(r)
		subcmd := &cobra.Command{
			Use:   rinst.Name(),
			Short: rinst.Desc(),
			RunE: func(cmd *cobra.Command, args []string) error {
				//配置加载及初始化
				ctx := context.Background()
				cc, err := LoadConfig(cfg)
				if err != nil {
					return err
				}
				client, err := storage.NewS3Client(ctx, cc.S3)
				if err != nil {
					return err
				}
				storage.SetDefaultClient(client)

				dbPath := cc.DB
				if override, ok := rinst.(app.DBPathOverride); ok {
					if val := override.DBOverridePath(); val != "" {
						dbPath = val
					}
				}
				sqliteDB, err := sqlite.New(dbPath, func(db database.IDatabase) error {
					return appdb.EnsureSchema(ctx, db)
				})
				if err != nil {
					return err
				}
				appdb.SetDefault(sqliteDB)

				//执行app流程
				if err := rinst.PreRun(ctx); err != nil {
					return err
				}
				if err := rinst.Run(ctx); err != nil {
					return err
				}
				if err := rinst.PostRun(ctx); err != nil {
					return err
				}
				return nil
			},
		}
		rinst.Init(subcmd.Flags())
		rootCmd.AddCommand(subcmd)

	}
}
