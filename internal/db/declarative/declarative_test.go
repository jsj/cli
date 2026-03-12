package declarative

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/supabase/cli/internal/db/diff"
	"github.com/supabase/cli/internal/utils"
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

	declarativeDir := DeclarativeDir()
	roles, err := afero.ReadFile(fsys, filepath.Join(declarativeDir, "cluster", "roles.sql"))
	require.NoError(t, err)
	assert.Equal(t, "create role app;", string(roles))

	users, err := afero.ReadFile(fsys, filepath.Join(declarativeDir, "schemas", "public", "tables", "users.sql"))
	require.NoError(t, err)
	assert.Equal(t, "create table users(id bigint);", string(users))

	cfg, err := afero.ReadFile(fsys, utils.ConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(cfg), `declarative-schemas`)
}

func TestDeclarativeDirUsesExperimentalPgdeltaPath(t *testing.T) {
	original := utils.Config.Experimental
	t.Cleanup(func() {
		utils.Config.Experimental = original
	})
	utils.Config.Experimental.Pgdelta.DeclarativeDirPath = "./declarative-schemas"

	assert.Equal(t, filepath.Join(utils.SupabaseDirPath, "declarative-schemas"), DeclarativeDir())
}

func TestDeclarativeDirRejectsPathOutsideProject(t *testing.T) {
	original := utils.Config.Experimental
	t.Cleanup(func() {
		utils.Config.Experimental = original
	})
	utils.Config.Experimental.Pgdelta.DeclarativeDirPath = "../outside"

	_, err := DeclarativeDirPath()
	assert.ErrorContains(t, err, "must stay within the supabase project directory")
}

func TestDeclarativeDirRejectsProjectRoot(t *testing.T) {
	original := utils.Config.Experimental
	t.Cleanup(func() {
		utils.Config.Experimental = original
	})
	utils.Config.Experimental.Pgdelta.DeclarativeDirPath = "."

	_, err := DeclarativeDirPath()
	assert.ErrorContains(t, err, "must point to a subdirectory")
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

func TestWriteDeclarativeSchemasRejectsAbsolutePath(t *testing.T) {
	fsys := afero.NewMemMapFs()
	err := WriteDeclarativeSchemas(diff.DeclarativeOutput{
		Files: []diff.DeclarativeFile{
			{Path: "/tmp/oops.sql", SQL: "select 1;"},
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

	cachePath := filepath.Join(utils.TempDir, "pgdelta", "catalog-migrations-"+hash+".json")
	require.NoError(t, afero.WriteFile(fsys, cachePath, []byte(`{"version":1}`), 0644))

	ref, err := getMigrationsCatalogRef(t.Context(), false, fsys)
	require.NoError(t, err)
	assert.Equal(t, cachePath, ref)
}

func TestStatusReportsConfiguredDirectory(t *testing.T) {
	fsys := afero.NewMemMapFs()
	original := utils.Config
	t.Cleanup(func() {
		utils.Config = original
	})
	utils.Config.Experimental.Pgdelta.DeclarativeDirPath = "./declarative-schemas"
	utils.Config.Db.Migrations.SchemaPaths = []string{"./declarative-schemas"}

	var out bytes.Buffer
	require.NoError(t, Status(t.Context(), &out, fsys))

	assert.Contains(t, out.String(), "supabase/declarative-schemas")
	assert.Contains(t, out.String(), "default migrate target")
}

func TestUpdateDeclarativeSchemaPathsConfigInsertsIntoExistingTable(t *testing.T) {
	fsys := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fsys, utils.ConfigPath, []byte("[db]\n\n[db.migrations]\nenabled = true\n"), 0644))

	require.NoError(t, updateDeclarativeSchemaPathsConfig(fsys))

	body, err := afero.ReadFile(fsys, utils.ConfigPath)
	require.NoError(t, err)
	assert.Contains(t, string(body), "[db.migrations]\nschema_paths = [")
	assert.Equal(t, 1, strings.Count(string(body), "[db.migrations]"))
}
