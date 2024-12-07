package total_table_sizes

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/spf13/afero"
	"github.com/supabase/cli/v2/internal/db/reset"
	"github.com/supabase/cli/v2/internal/migration/list"
	"github.com/supabase/cli/v2/internal/utils"
	"github.com/supabase/cli/v2/pkg/pgxv5"
)

//go:embed total_table_sizes.sql
var TotalTableSizesQuery string

type Result struct {
	Schema string
	Name   string
	Size   string
}

func Run(ctx context.Context, config pgconn.Config, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	conn, err := utils.ConnectByConfig(ctx, config, options...)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	rows, err := conn.Query(ctx, TotalTableSizesQuery, reset.LikeEscapeSchema(utils.PgSchemas))
	if err != nil {
		return errors.Errorf("failed to query rows: %w", err)
	}
	result, err := pgxv5.CollectRows[Result](rows)
	if err != nil {
		return err
	}

	table := "Schema|Table|Size|\n|-|-|-|\n"
	for _, r := range result {
		table += fmt.Sprintf("|`%s`|`%s`|`%s`|\n", r.Schema, r.Name, r.Size)
	}
	return list.RenderTable(table)
}
