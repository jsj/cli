package declarative

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/supabase/cli/internal/db/diff"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/pkg/config"
)

func TestWriteDeclarativeSchemas(t *testing.T) {
	// This verifies the main happy path for declarative export materialization:
	// files are written to expected locations and config is updated accordingly.
	fsys := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fsys, utils.ConfigPath, []byte("[db]\n"), 0644))

	output := diff.DeclarativeOutput{
		Files: []diff.DeclarativeFile{
			{Path: "cluster/roles.sql", SQL: "create role app;"},
			{Path: "schemas/public/tables/users.sql", SQL: "create table users(id bigint);"},
		},
	}

	err := WriteDeclarativeSchemas(output, fsys)
	require.NoError(t, err)

	roles, err := afero.ReadFile(fsys, filepath.Join(utils.DeclarativeDir, "cluster", "roles.sql"))
	require.NoError(t, err)
	assert.Equal(t, "create role app;", string(roles))

	users, err := afero.ReadFile(fsys, filepath.Join(utils.DeclarativeDir, "schemas", "public", "tables", "users.sql"))
	require.NoError(t, err)
	assert.Equal(t, "create table users(id bigint);", string(users))

	cfg, err := afero.ReadFile(fsys, utils.ConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(cfg), `"declarative"`)
}

func TestTryCacheMigrationsCatalogWritesPrefixedCache(t *testing.T) {
	fsys := afero.NewMemMapFs()
	original := utils.Config.Experimental.PgDelta
	utils.Config.Experimental.PgDelta = &config.PgDeltaConfig{Enabled: true}
	t.Cleanup(func() {
		utils.Config.Experimental.PgDelta = original
		exportCatalog = diff.ExportCatalogPgDelta
	})
	p := filepath.Join(utils.MigrationsDir, "20240101000000_first.sql")
	require.NoError(t, afero.WriteFile(fsys, p, []byte("create table a();"), 0644))
	exportCatalog = func(_ context.Context, targetRef, role string, _ ...func(*pgx.ConnConfig)) (string, error) {
		assert.Equal(t, "postgres", role)
		assert.Contains(t, targetRef, "db.test.supabase.co")
		return `{"version":1}`, nil
	}

	err := TryCacheMigrationsCatalog(t.Context(), pgconn.Config{
		Host:     "db.test.supabase.co",
		Port:     5432,
		User:     "postgres",
		Password: "postgres",
		Database: "postgres",
	}, "remote-ref", "", fsys)
	require.NoError(t, err)

	hash, err := hashMigrations(fsys)
	require.NoError(t, err)
	cachePath := filepath.Join(utils.TempDir, "pgdelta", "catalog-remote-ref-migrations-"+hash+".json")
	cached, err := afero.ReadFile(fsys, cachePath)
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1}`, string(cached))
}

func TestTryCacheMigrationsCatalogSkipsPartialApply(t *testing.T) {
	fsys := afero.NewMemMapFs()
	original := utils.Config.Experimental.PgDelta
	utils.Config.Experimental.PgDelta = &config.PgDeltaConfig{Enabled: true}
	called := false
	t.Cleanup(func() {
		utils.Config.Experimental.PgDelta = original
		exportCatalog = diff.ExportCatalogPgDelta
	})
	exportCatalog = func(_ context.Context, _ string, _ string, _ ...func(*pgx.ConnConfig)) (string, error) {
		called = true
		return `{"version":1}`, nil
	}

	err := TryCacheMigrationsCatalog(t.Context(), pgconn.Config{
		Host: "127.0.0.1", Port: 5432, User: "postgres", Password: "postgres", Database: "postgres",
	}, "", "20240101000000", fsys)
	require.NoError(t, err)
	assert.False(t, called)
}

func TestCatalogPrefixFromConfig(t *testing.T) {
	local := catalogPrefixFromConfig(pgconn.Config{Host: utils.Config.Hostname, Port: utils.Config.Db.Port})
	assert.Equal(t, "local", local)

	linked := catalogPrefixFromConfig(pgconn.Config{Host: "db.abcdefghijklmnopqrst.supabase.co", Port: 5432})
	assert.Equal(t, "abcdefghijklmnopqrst", linked)

	custom := catalogPrefixFromConfig(pgconn.Config{Host: "db.example.com", Port: 5432, Database: "postgres", User: "postgres"})
	sum := sha256.Sum256([]byte("postgres@db.example.com:5432/postgres"))
	assert.Equal(t, "url-"+hex.EncodeToString(sum[:])[:12], custom)
}

func TestWriteDeclarativeSchemasUsesConfiguredDir(t *testing.T) {
	fsys := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fsys, utils.ConfigPath, []byte("[db]\n"), 0644))
	original := utils.Config.Experimental.PgDelta
	utils.Config.Experimental.PgDelta = &config.PgDeltaConfig{
		DeclarativeSchemaPath: filepath.Join(utils.SupabaseDirPath, "db", "decl"),
	}
	t.Cleanup(func() {
		utils.Config.Experimental.PgDelta = original
	})

	output := diff.DeclarativeOutput{
		Files: []diff.DeclarativeFile{
			{Path: "cluster/roles.sql", SQL: "create role app;"},
		},
	}

	err := WriteDeclarativeSchemas(output, fsys)
	require.NoError(t, err)

	rolesPath := filepath.Join(utils.SupabaseDirPath, "db", "decl", "cluster", "roles.sql")
	roles, err := afero.ReadFile(fsys, rolesPath)
	require.NoError(t, err)
	assert.Equal(t, "create role app;", string(roles))

	cfg, err := afero.ReadFile(fsys, utils.ConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(cfg), `db/decl`)
}

func TestWriteDeclarativeSchemasRejectsUnsafePath(t *testing.T) {
	// Export paths must stay within supabase/declarative to prevent traversal.
	fsys := afero.NewMemMapFs()
	err := WriteDeclarativeSchemas(diff.DeclarativeOutput{
		Files: []diff.DeclarativeFile{
			{Path: "../oops.sql", SQL: "select 1;"},
		},
	}, fsys)
	assert.ErrorContains(t, err, "unsafe declarative export path")
}

func TestHashMigrationsChangesWithContent(t *testing.T) {
	// Cache keys must change whenever migration SQL changes.
	fsys := afero.NewMemMapFs()
	p1 := filepath.Join(utils.MigrationsDir, "20240101000000_first.sql")
	p2 := filepath.Join(utils.MigrationsDir, "20240101000001_second.sql")
	require.NoError(t, afero.WriteFile(fsys, p1, []byte("create table a();"), 0644))
	require.NoError(t, afero.WriteFile(fsys, p2, []byte("create table b();"), 0644))

	h1, err := hashMigrations(fsys)
	require.NoError(t, err)
	require.NotEmpty(t, h1)

	require.NoError(t, afero.WriteFile(fsys, p2, []byte("create table b(id bigint);"), 0644))
	h2, err := hashMigrations(fsys)
	require.NoError(t, err)

	assert.NotEqual(t, h1, h2)
}

func TestGetMigrationsCatalogRefUsesCache(t *testing.T) {
	// When a matching hash snapshot exists, catalog generation should be skipped.
	fsys := afero.NewMemMapFs()
	p := filepath.Join(utils.MigrationsDir, "20240101000000_first.sql")
	require.NoError(t, afero.WriteFile(fsys, p, []byte("create table a();"), 0644))
	hash, err := hashMigrations(fsys)
	require.NoError(t, err)

	cachePath := filepath.Join(utils.TempDir, "pgdelta", "catalog-local-migrations-"+hash+".json")
	require.NoError(t, afero.WriteFile(fsys, cachePath, []byte(`{"version":1}`), 0644))

	ref, err := getMigrationsCatalogRef(t.Context(), false, fsys, "local")
	require.NoError(t, err)
	assert.Equal(t, cachePath, ref)
}

func TestGetMigrationsCatalogRefUsesProjectPrefix(t *testing.T) {
	fsys := afero.NewMemMapFs()
	p := filepath.Join(utils.MigrationsDir, "20240101000000_first.sql")
	require.NoError(t, afero.WriteFile(fsys, p, []byte("create table a();"), 0644))
	hash, err := hashMigrations(fsys)
	require.NoError(t, err)

	cachePath := filepath.Join(utils.TempDir, "pgdelta", "catalog-testproject-migrations-"+hash+".json")
	require.NoError(t, afero.WriteFile(fsys, cachePath, []byte(`{"version":1}`), 0644))

	ref, err := getMigrationsCatalogRef(t.Context(), false, fsys, "testproject")
	require.NoError(t, err)
	assert.Equal(t, cachePath, ref)
}
