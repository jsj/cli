package pgdelta

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/h2non/gock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/supabase/cli/internal/testing/apitest"
	"github.com/supabase/cli/internal/utils"
)

func TestBuildCLIArgs(t *testing.T) {
	assert.Equal(t,
		[]string{"x", PgDeltaPackage, "plan", "--target", "postgres://db"},
		buildCLIArgs("plan", "--target", "postgres://db"),
	)
}

func TestContainerRef(t *testing.T) {
	assert.Equal(t, "postgres://db", containerRef("postgres://db"))
	assert.Equal(t, "/workspace/supabase/.temp/catalog.json", containerRef("supabase/.temp/catalog.json"))
}

func TestDiffSQLTreatsExitCodeTwoAsChangesDetected(t *testing.T) {
	utils.Config.ProjectId = "test-project"
	require.NoError(t, apitest.MockDocker(utils.Docker))
	defer gock.OffAll()

	const containerID = "pgdelta-test"
	apitest.MockDockerStart(utils.Docker, BunImage, containerID)
	require.NoError(t, apitest.MockDockerLogsStream(utils.Docker, containerID, 2, strings.NewReader("create table test();\n")))
	gock.New(utils.Docker.DaemonHost()).
		Delete("/v" + utils.Docker.ClientVersion() + "/containers/" + containerID).
		Reply(http.StatusOK)

	out, err := DiffSQL(context.Background(), "supabase/.temp/source.json", "supabase/.temp/target.json", nil)

	require.NoError(t, err)
	assert.Equal(t, "create table test();\n", out)
	assert.Empty(t, apitest.ListUnmatchedRequests())
}

func TestWorkspacePathRefRejectsOutsideWorkspace(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	tempDir := t.TempDir()
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, _, err = workspacePathRef("/tmp/outside.sql")

	require.ErrorContains(t, err, "must be inside the current project")
}

func TestBuildPlanArgsDefaultsToSupabaseIntegration(t *testing.T) {
	args, err := buildPlanArgs("postgres://source", "postgres://target", nil)

	require.NoError(t, err)
	assert.Contains(t, args, "--integration")
	assert.Contains(t, args, "supabase")
}

func TestBuildPlanArgsRespectsExplicitIntegrationOverride(t *testing.T) {
	args, err := buildPlanArgs("postgres://source", "postgres://target", []string{"--integration", "custom"})

	require.NoError(t, err)
	assert.NotContains(t, strings.Join(args, " "), "--integration supabase")
	assert.Contains(t, args, "custom")
}

func TestBuildDeclarativeExportArgsDefaultsToSupabaseIntegration(t *testing.T) {
	args, err := buildDeclarativeExportArgs("", "postgres://target", "/workspace/supabase/declarative", nil)

	require.NoError(t, err)
	assert.Contains(t, args, "--integration")
	assert.Contains(t, args, "supabase")
}

func TestBuildDeclarativeExportArgsUsesConfiguredFormatOptions(t *testing.T) {
	original := utils.Config.Experimental
	t.Cleanup(func() {
		utils.Config.Experimental = original
	})
	utils.Config.Experimental.Pgdelta.FormatOptions = `{"keywordCase":"lower"}`

	args, err := buildDeclarativeExportArgs("", "postgres://target", "/workspace/supabase/declarative", nil)

	require.NoError(t, err)
	assert.Contains(t, args, "--format-options")
	assert.Contains(t, args, `{"keywordCase":"lower"}`)
}

func TestFlagValueReadsSeparatedAndEqualsForms(t *testing.T) {
	assert.Equal(t, "postgres://source", flagValue([]string{"--source", "postgres://source"}, "--source", "-s"))
	assert.Equal(t, "postgres://target", flagValue([]string{"--target=postgres://target"}, "--target", "-t"))
}

func TestNormalizePassthroughPathFlagsRewritesWorkspaceFiles(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	tempDir := t.TempDir()
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	args, err := normalizePassthroughPathFlags([]string{
		"--source", "supabase/.temp/source.json",
		"--output", "supabase/.temp/out.sql",
		"--integration", "./integration.json",
	})

	require.NoError(t, err)
	assert.Equal(t, []string{
		"--source", "/workspace/supabase/.temp/source.json",
		"--output", "/workspace/supabase/.temp/out.sql",
		"--integration", "/workspace/integration.json",
	}, args)
}

func TestNormalizePassthroughPathFlagsRejectsOutsideWorkspace(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	tempDir := t.TempDir()
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	_, err = normalizePassthroughPathFlags([]string{"--output", "../outside.sql"})

	require.ErrorContains(t, err, "must be inside the current project")
}
