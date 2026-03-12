package cmd

import (
	"context"
	"fmt"

	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/supabase/cli/internal/db/declarative"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/internal/utils/flags"
)

var (
	declarativeTopOverwrite     bool
	declarativeTopNoCache       bool
	declarativeTopMigrationName string
	declarativeFromMigrations   bool
	declarativeFromLocal        bool
	declarativeFromLinked       bool
	declarativeFromDBURL        string

	declarativeCmd = &cobra.Command{
		GroupID: groupLocalDev,
		Use:     "declarative",
		Short:   "Manage declarative database schemas",
	}

	declarativeGenerateCmd = &cobra.Command{
		Use:   "generate",
		Short: "Generate declarative schema files",
		RunE: func(cmd *cobra.Command, args []string) error {
			fsys := afero.NewOsFs()
			switch sourceSelectionCount() {
			case 0:
				return errors.New("must set exactly one of --from-migrations, --from-local, --from-linked, or --from-db-url")
			case 1:
			default:
				return errors.New("cannot use multiple declarative generate source flags together")
			}
			if declarativeFromMigrations {
				if err := flags.LoadConfig(fsys); err != nil {
					return err
				}
				return declarative.GenerateFromMigrations(cmd.Context(), declarativeTopOverwrite, declarativeTopNoCache, fsys)
			}
			config, err := resolveGenerateTarget(cmd.Context(), fsys)
			if err != nil {
				return err
			}
			return declarative.Generate(cmd.Context(), nil, config, declarativeTopOverwrite, declarativeTopNoCache, fsys)
		},
	}

	declarativeMigrateCmd = &cobra.Command{
		Use:   "migrate",
		Short: "Generate a migration from declarative schema changes",
		RunE: func(cmd *cobra.Command, args []string) error {
			fsys := afero.NewOsFs()
			if err := flags.LoadConfig(fsys); err != nil {
				return err
			}
			return declarative.Migrate(cmd.Context(), declarativeTopMigrationName, declarativeTopNoCache, fsys)
		},
	}

	declarativeApplyCmd = &cobra.Command{
		Use:   "apply",
		Short: "Reset local database and apply declarative schema",
		RunE: func(cmd *cobra.Command, args []string) error {
			fsys := afero.NewOsFs()
			if err := flags.LoadConfig(fsys); err != nil {
				return err
			}
			return declarative.ApplyLocal(cmd.Context(), fsys)
		},
	}

	declarativeStatusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show declarative configuration and local readiness",
		RunE: func(cmd *cobra.Command, args []string) error {
			fsys := afero.NewOsFs()
			if err := flags.LoadConfig(fsys); err != nil {
				return err
			}
			return declarative.Status(cmd.Context(), cmd.OutOrStdout(), fsys)
		},
	}
)

func sourceSelectionCount() int {
	count := 0
	for _, enabled := range []bool{
		declarativeFromMigrations,
		declarativeFromLocal,
		declarativeFromLinked,
		len(declarativeFromDBURL) > 0,
	} {
		if enabled {
			count++
		}
	}
	return count
}

func resolveGenerateTarget(ctx context.Context, fsys afero.Fs) (pgconn.Config, error) {
	if err := flags.LoadConfig(fsys); err != nil {
		return pgconn.Config{}, err
	}
	switch {
	case declarativeFromLocal:
		return pgconn.Config{
			Host:     utils.Config.Hostname,
			Port:     utils.Config.Db.Port,
			User:     "postgres",
			Password: utils.Config.Db.Password,
			Database: "postgres",
		}, nil
	case declarativeFromLinked:
		if err := flags.LoadProjectRef(fsys); err != nil {
			return pgconn.Config{}, err
		}
		return flags.NewDbConfigWithPassword(ctx, flags.ProjectRef)
	case len(declarativeFromDBURL) > 0:
		config, err := pgconn.ParseConfig(declarativeFromDBURL)
		if err != nil {
			return pgconn.Config{}, errors.Errorf("failed to parse connection string: %w", err)
		}
		return *config, nil
	default:
		return pgconn.Config{}, errors.New("missing declarative generate source")
	}
}

func init() {
	declarativeCmd.PersistentFlags().BoolVar(&declarativeTopNoCache, "no-cache", false, "Disable catalog cache and force fresh shadow database setup.")

	generateFlags := declarativeGenerateCmd.Flags()
	generateFlags.BoolVar(&declarativeTopOverwrite, "overwrite", false, "Overwrite declarative schema files without confirmation.")
	generateFlags.BoolVar(&declarativeFromMigrations, "from-migrations", false, "Generate declarative schema from local migrations.")
	generateFlags.BoolVar(&declarativeFromLocal, "from-local", false, "Generate declarative schema from the local database.")
	generateFlags.BoolVar(&declarativeFromLinked, "from-linked", false, "Generate declarative schema from the linked project.")
	generateFlags.StringVar(&declarativeFromDBURL, "from-db-url", "", "Generate declarative schema from the specified Postgres URL.")
	generateFlags.StringVarP(&dbPassword, "password", "p", "", "Password to your remote Postgres database.")
	cobra.CheckErr(viper.BindPFlag("DB_PASSWORD", generateFlags.Lookup("password")))

	migrateFlags := declarativeMigrateCmd.Flags()
	migrateFlags.StringVar(&declarativeTopMigrationName, "migration-name", "declarative_sync", "Saves the declarative diff to a new migration file.")

	declarativeCmd.AddCommand(declarativeGenerateCmd)
	declarativeCmd.AddCommand(declarativeMigrateCmd)
	declarativeCmd.AddCommand(declarativeApplyCmd)
	declarativeCmd.AddCommand(declarativeStatusCmd)
	rootCmd.AddCommand(declarativeCmd)
	experimental = append(experimental, declarativeCmd)

	// Keep the old db declarative surface as a hidden compatibility entrypoint.
	dbDeclarativeCmd.Hidden = true
	dbDeclarativeCmd.Deprecated = "use `supabase declarative ...` instead"
	declarativeGenerateCmd.PostRun = func(cmd *cobra.Command, args []string) {
		fmt.Println("Finished " + utils.Aqua("supabase declarative generate") + ".")
	}
	_ = context.Background()
}
