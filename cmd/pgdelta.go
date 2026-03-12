package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/supabase/cli/internal/pgdelta"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/internal/utils/flags"
)

var (
	pgdeltaMigrationName   string
	pgdeltaDeclarativePath string
	pgdeltaCatalogOutput   string

	pgdeltaCmd = &cobra.Command{
		GroupID: groupLocalDev,
		Use:     "pgdelta",
		Short:   "Run experimental pg-delta workflows",
		Hidden:  true,
	}

	pgdeltaPlanCmd = &cobra.Command{
		Use:   "plan",
		Short: "Plan schema changes and save a migration",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pgdelta.PlanMigration(cmd.Context(), pgdeltaMigrationName, flags.DbConfig, passthroughArgs(cmd, args), cmd.OutOrStdout(), cmd.ErrOrStderr(), afero.NewOsFs())
		},
	}

	pgdeltaCatalogExportCmd = &cobra.Command{
		Use:   "catalog-export",
		Short: "Export a pg-delta catalog snapshot",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return pgdelta.ExportCatalog(cmd.Context(), flags.DbConfig, pgdeltaCatalogOutput, passthroughArgs(cmd, args), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	pgdeltaDeclarativeCmd = &cobra.Command{
		Use:   "declarative",
		Short: "Manage declarative schemas with pg-delta",
	}

	pgdeltaDeclarativeExportCmd = &cobra.Command{
		Use:   "export",
		Short: "Export declarative schema files",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := effectivePgdeltaDeclarativePath(cmd)
			if err != nil {
				return err
			}
			return pgdelta.ExportDeclarative(cmd.Context(), flags.DbConfig, path, passthroughArgs(cmd, args), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	pgdeltaDeclarativeApplyCmd = &cobra.Command{
		Use:   "apply",
		Short: "Apply declarative schema files",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := effectivePgdeltaDeclarativePath(cmd)
			if err != nil {
				return err
			}
			return pgdelta.ApplyDeclarativePath(cmd.Context(), flags.DbConfig, path, cmd.OutOrStdout(), cmd.ErrOrStderr(), passthroughArgs(cmd, args)...)
		},
	}
)

func passthroughArgs(cmd *cobra.Command, args []string) []string {
	if dash := cmd.ArgsLenAtDash(); dash >= 0 && dash <= len(args) {
		return args[dash:]
	}
	return nil
}

func effectivePgdeltaDeclarativePath(cmd *cobra.Command) (string, error) {
	if cmd.Flags().Changed("path") {
		return pgdeltaDeclarativePath, nil
	}
	return utils.GetDeclarativeDirPath()
}

func init() {
	targetFlags := func(cmd *cobra.Command, linkedDefault, localDefault bool) {
		flags := cmd.Flags()
		flags.String("db-url", "", "Connect using the specified Postgres URL (must be percent-encoded).")
		flags.Bool("linked", linkedDefault, "Connect to the linked project.")
		flags.Bool("local", localDefault, "Connect to the local database.")
		cmd.MarkFlagsMutuallyExclusive("db-url", "linked", "local")
		flags.StringVarP(&dbPassword, "password", "p", "", "Password to your remote Postgres database.")
		cmd.MarkFlagsMutuallyExclusive("db-url", "password")
		cobra.CheckErr(viper.BindPFlag("DB_PASSWORD", flags.Lookup("password")))
	}

	targetFlags(pgdeltaPlanCmd, false, true)
	planFlags := pgdeltaPlanCmd.Flags()
	planFlags.StringVarP(&pgdeltaMigrationName, "file", "f", "pgdelta_plan", "Saves the pg-delta plan as a new migration file.")

	targetFlags(pgdeltaCatalogExportCmd, false, true)
	catalogFlags := pgdeltaCatalogExportCmd.Flags()
	catalogFlags.StringVarP(&pgdeltaCatalogOutput, "output", "o", filepath.Join(utils.TempDir, "pgdelta", "catalog.json"), "Path to save the catalog snapshot.")

	targetFlags(pgdeltaDeclarativeExportCmd, true, false)
	exportFlags := pgdeltaDeclarativeExportCmd.Flags()
	exportFlags.StringVar(&pgdeltaDeclarativePath, "path", "", "Directory path for declarative schema files.")

	targetFlags(pgdeltaDeclarativeApplyCmd, false, true)
	applyFlags := pgdeltaDeclarativeApplyCmd.Flags()
	applyFlags.StringVar(&pgdeltaDeclarativePath, "path", "", "Directory path for declarative schema files.")

	pgdeltaDeclarativeCmd.AddCommand(pgdeltaDeclarativeExportCmd)
	pgdeltaDeclarativeCmd.AddCommand(pgdeltaDeclarativeApplyCmd)
	pgdeltaCmd.AddCommand(pgdeltaPlanCmd)
	pgdeltaCmd.AddCommand(pgdeltaCatalogExportCmd)
	pgdeltaCmd.AddCommand(pgdeltaDeclarativeCmd)
	rootCmd.AddCommand(pgdeltaCmd)
	experimental = append(experimental, pgdeltaCmd)

	wrapHelp := func(command *cobra.Command, upstreamArgs ...string) {
		defaultHelp := command.HelpFunc()
		command.SetHelpFunc(func(cmd *cobra.Command, args []string) {
			defaultHelp(cmd, args)
			fmt.Fprintln(cmd.OutOrStdout(), "\nUpstream pg-delta help:")
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			if err := pgdelta.RunCLI(ctx, upstreamArgs, nil, nil, cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Unable to load upstream pg-delta help: %v\n", err)
			}
		})
	}
	wrapHelp(pgdeltaCmd, "--help")
	wrapHelp(pgdeltaPlanCmd, "plan", "--help")
	wrapHelp(pgdeltaCatalogExportCmd, "catalog-export", "--help")
	wrapHelp(pgdeltaDeclarativeCmd, "declarative", "--help")
	wrapHelp(pgdeltaDeclarativeExportCmd, "declarative", "export", "--help")
	wrapHelp(pgdeltaDeclarativeApplyCmd, "declarative", "apply", "--help")
}
