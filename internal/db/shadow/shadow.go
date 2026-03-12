package shadow

import (
	"context"
	"os"
	"strconv"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/go-connections/nat"
	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/db/start"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/pkg/migration"
)

const CreateTemplateSQL = "CREATE DATABASE contrib_regression TEMPLATE postgres"

func CreateDatabase(ctx context.Context, port uint16) (string, error) {
	config := start.NewContainerConfig("-c", "max_worker_processes=0")
	hostPort := strconv.FormatUint(uint64(port), 10)
	hostConfig := container.HostConfig{
		PortBindings: nat.PortMap{"5432/tcp": []nat.PortBinding{{HostPort: hostPort}}},
		AutoRemove:   true,
	}
	if utils.Config.Db.MajorVersion <= 14 {
		hostConfig.Tmpfs = map[string]string{"/docker-entrypoint-initdb.d": ""}
	}
	return utils.DockerStart(ctx, config, hostConfig, network.NetworkingConfig{}, "")
}

func Connect(ctx context.Context, timeout time.Duration, options ...func(*pgx.ConnConfig)) (*pgx.Conn, error) {
	policy := start.NewBackoffPolicy(ctx, timeout)
	config := pgconn.Config{Port: utils.Config.Db.ShadowPort}
	connect := func() (*pgx.Conn, error) {
		return utils.ConnectLocalPostgres(ctx, config, options...)
	}
	return backoff.RetryWithData(connect, policy)
}

func Migrate(ctx context.Context, containerID string, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	migrations, err := migration.ListLocalMigrations(utils.MigrationsDir, afero.NewIOFS(fsys))
	if err != nil {
		return err
	}
	conn, err := Connect(ctx, 10*time.Second, options...)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if err := start.SetupDatabase(ctx, conn, containerID[:12], os.Stderr, fsys); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, CreateTemplateSQL); err != nil {
		return errors.Errorf("failed to create template database: %w", err)
	}
	return migration.ApplyMigrations(ctx, migrations, conn, afero.NewIOFS(fsys))
}
