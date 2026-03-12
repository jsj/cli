package declarative

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/db/diff"
	"github.com/supabase/cli/internal/db/reset"
	migrationup "github.com/supabase/cli/internal/migration/up"
	"github.com/supabase/cli/internal/pgdelta"
	"github.com/supabase/cli/internal/utils"
)

func GenerateFromMigrations(ctx context.Context, overwrite bool, noCache bool, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	targetRef, err := getMigrationsCatalogRef(ctx, noCache, fsys, options...)
	if err != nil {
		return err
	}
	return generateToDir(ctx, "", targetRef, overwrite, fsys, options...)
}

func Migrate(ctx context.Context, file string, noCache bool, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	if exists, err := afero.DirExists(fsys, DeclarativeDir()); err != nil {
		return err
	} else if !exists {
		return errors.Errorf("No declarative schema directory found. Run %s first.", utils.Aqua("supabase declarative generate"))
	}
	sourceRef, err := getMigrationsCatalogRef(ctx, noCache, fsys, options...)
	if err != nil {
		return err
	}
	targetRef, err := getDeclarativeCatalogRef(ctx, fsys, options...)
	if err != nil {
		return err
	}
	args := utils.GetPgdeltaMigrateArgs()
	out, err := pgdelta.DiffSQL(ctx, sourceRef, targetRef, args, options...)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(file)) == 0 {
		file = "declarative_sync"
	}
	if err := diff.SaveDiff(out, file, fsys); err != nil {
		return err
	}
	drops := findDropStatements(out)
	if len(drops) > 0 {
		fmt.Fprintln(os.Stderr, "Found drop statements in schema diff. Please double check if these are expected:")
		fmt.Fprintln(os.Stderr, utils.Yellow(strings.Join(drops, "\n")))
	}
	if err := maybeApplyToLocal(ctx, fsys, options...); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Review the generated migration carefully before rolling it out remotely.")
	fmt.Fprintln(os.Stderr, "Suggested next step: "+utils.Bold("supabase migration up --linked"))
	return nil
}

func ApplyLocal(ctx context.Context, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	declarativeDir, err := DeclarativeDirPath()
	if err != nil {
		return err
	}
	if exists, err := afero.DirExists(fsys, declarativeDir); err != nil {
		return err
	} else if !exists {
		return errors.Errorf("No declarative schema directory found. Run %s first.", utils.Aqua("supabase declarative generate"))
	}
	if err := utils.AssertSupabaseDbIsRunning(); err != nil {
		return err
	}
	msg := "Reset the local database and apply the declarative schema?"
	if shouldApply, err := utils.NewConsole().PromptYesNo(ctx, msg, false); err != nil {
		return err
	} else if !shouldApply {
		return errors.New(context.Canceled)
	}
	config := localDbConfig()
	if err := reset.Run(ctx, "", 0, config, fsys, options...); err != nil {
		return err
	}
	declarativeDir, err = DeclarativeDirPath()
	if err != nil {
		return err
	}
	return pgdelta.ApplyDeclarativePath(ctx, config, declarativeDir, os.Stdout, os.Stderr, utils.GetPgdeltaApplyArgs()...)
}

func Status(ctx context.Context, w io.Writer, fsys afero.Fs) error {
	declarativeDir, err := DeclarativeDirPath()
	if err != nil {
		return err
	}
	schemaPathsEntry, err := utils.GetDeclarativeSchemaPathsEntry()
	if err != nil {
		return err
	}
	localRunning := utils.AssertSupabaseDbIsRunning() == nil
	usesDeclarative := len(utils.Config.Db.Migrations.SchemaPaths) == 1 && utils.Config.Db.Migrations.SchemaPaths[0] == declarativeDir
	if !usesDeclarative {
		for _, entry := range utils.Config.Db.Migrations.SchemaPaths {
			if entry == schemaPathsEntry || entry == declarativeDir {
				usesDeclarative = true
				break
			}
		}
	}
	fmt.Fprintln(w, "Declarative directory:", declarativeDir)
	fmt.Fprintln(w, "schema_paths configured for declarative:", usesDeclarative)
	fmt.Fprintln(w, "local database running:", localRunning)
	fmt.Fprintln(w, "default generate source: explicit --from-* selection")
	fmt.Fprintln(w, "default migrate target: local migrations baseline")
	if localRunning {
		fmt.Fprintln(w, "To rebuild local state from declarative schema, run:", utils.Bold("supabase declarative apply"))
	} else {
		fmt.Fprintln(w, "Start local services to use local declarative flows:", utils.Bold("supabase start"))
	}
	return nil
}

func maybeApplyToLocal(ctx context.Context, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	if err := utils.AssertSupabaseDbIsRunning(); err != nil {
		if errors.Is(err, utils.ErrNotRunning) {
			return nil
		}
		return err
	}
	msg := "Apply the new migration to the local database now?"
	if shouldApply, err := utils.NewConsole().PromptYesNo(ctx, msg, false); err != nil {
		return err
	} else if !shouldApply {
		return nil
	}
	return migrationup.Run(ctx, false, localDbConfig(), fsys, options...)
}

func localDbConfig() pgconn.Config {
	return pgconn.Config{
		Host:     utils.Config.Hostname,
		Port:     utils.Config.Db.Port,
		User:     "postgres",
		Password: utils.Config.Db.Password,
		Database: "postgres",
	}
}
