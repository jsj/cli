package pgdelta

import (
	"context"
	"os"

	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/utils"
)

// ApplyDeclarative applies files from supabase/declarative to the target
// database using pg-delta's declarative apply engine.
//
// This is intentionally separate from migration apply so declarative workflows
// can evolve independently from timestamped migration execution.
func ApplyDeclarative(ctx context.Context, config pgconn.Config, fsys afero.Fs) error {
	declarativeDir, err := utils.GetDeclarativeDirPath()
	if err != nil {
		return err
	}
	if _, err := fsys.Stat(declarativeDir); err != nil {
		return errors.Errorf("declarative schema directory not found: %s", declarativeDir)
	}
	return ApplyDeclarativePath(ctx, config, declarativeDir, os.Stdout, os.Stderr)
}
