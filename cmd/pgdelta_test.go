package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/h2non/gock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/supabase/cli/internal/pgdelta"
	"github.com/supabase/cli/internal/testing/apitest"
	"github.com/supabase/cli/internal/utils"
)

const mutuallyExclusiveAnnotation = "cobra_annotation_mutually_exclusive"

func TestPgdeltaCommandRegistration(t *testing.T) {
	pgdeltaCmd, _, err := rootCmd.Find([]string{"pgdelta"})
	require.NoError(t, err)
	require.NotNil(t, pgdeltaCmd)

	assert.Equal(t, "pgdelta", pgdeltaCmd.Name())
	assert.Equal(t, groupLocalDev, pgdeltaCmd.GroupID)
	assert.True(t, IsExperimental(pgdeltaCmd))
}

func TestPgdeltaCommandShape(t *testing.T) {
	pgdeltaCmd, _, err := rootCmd.Find([]string{"pgdelta"})
	require.NoError(t, err)

	planCmd, _, err := rootCmd.Find([]string{"pgdelta", "plan"})
	require.NoError(t, err)
	assert.Same(t, pgdeltaCmd, planCmd.Parent())

	catalogExportCmd, _, err := rootCmd.Find([]string{"pgdelta", "catalog-export"})
	require.NoError(t, err)
	assert.Same(t, pgdeltaCmd, catalogExportCmd.Parent())

	declarativeCmd, _, err := rootCmd.Find([]string{"pgdelta", "declarative"})
	require.NoError(t, err)
	assert.Same(t, pgdeltaCmd, declarativeCmd.Parent())

	declarativeExportCmd, _, err := rootCmd.Find([]string{"pgdelta", "declarative", "export"})
	require.NoError(t, err)
	assert.Same(t, declarativeCmd, declarativeExportCmd.Parent())

	declarativeApplyCmd, _, err := rootCmd.Find([]string{"pgdelta", "declarative", "apply"})
	require.NoError(t, err)
	assert.Same(t, declarativeCmd, declarativeApplyCmd.Parent())
}

func TestPgdeltaDbURLConflictsWithPassword(t *testing.T) {
	checkConflict := func(t *testing.T, cmdPath []string) {
		t.Helper()

		cmd, _, err := rootCmd.Find(cmdPath)
		require.NoError(t, err)

		dbURLGroups := strings.Join(cmd.Flag("db-url").Annotations[mutuallyExclusiveAnnotation], "\n")
		passwordGroups := strings.Join(cmd.Flag("password").Annotations[mutuallyExclusiveAnnotation], "\n")
		assert.Contains(t, dbURLGroups, "db-url password")
		assert.Contains(t, passwordGroups, "db-url password")
	}

	checkConflict(t, []string{"pgdelta", "plan"})
	checkConflict(t, []string{"pgdelta", "catalog-export"})
	checkConflict(t, []string{"pgdelta", "declarative", "export"})
	checkConflict(t, []string{"pgdelta", "declarative", "apply"})
}

func TestPassthroughArgsReturnsOnlyArgsAfterDash(t *testing.T) {
	rootCmd.SetArgs([]string{"pgdelta", "plan", "--local", "--", "--help", "--format", "sql"})
	defer rootCmd.SetArgs(nil)

	cmd, _, err := rootCmd.Find([]string{"pgdelta", "plan"})
	require.NoError(t, err)
	require.NoError(t, cmd.ParseFlags([]string{"--local", "--", "--help", "--format", "sql"}))

	assert.Equal(t, []string{"--help", "--format", "sql"}, passthroughArgs(cmd, []string{"--help", "--format", "sql"}))
}

func TestPgdeltaPlanHelpProxiesUpstreamHelp(t *testing.T) {
	utils.Config.ProjectId = "test-project"
	require.NoError(t, apitest.MockDocker(utils.Docker))
	defer gock.OffAll()

	const containerID = "pgdelta-help"
	apitest.MockDockerStart(utils.Docker, pgdelta.BunImage, containerID)
	require.NoError(t, apitest.MockDockerLogs(utils.Docker, containerID, "Compute schema diff and preview changes\n"))

	var stdout, stderr bytes.Buffer
	pgdeltaPlanCmd.SetContext(context.Background())
	pgdeltaPlanCmd.SetOut(&stdout)
	pgdeltaPlanCmd.SetErr(&stderr)

	require.NoError(t, pgdeltaPlanCmd.Help())
	assert.Contains(t, stdout.String(), "Compute schema diff and preview changes")
}

func TestPgdeltaRootHelpAppendsUpstreamHelp(t *testing.T) {
	utils.Config.ProjectId = "test-project"
	require.NoError(t, apitest.MockDocker(utils.Docker))
	defer gock.OffAll()

	const containerID = "pgdelta-root-help"
	apitest.MockDockerStart(utils.Docker, pgdelta.BunImage, containerID)
	require.NoError(t, apitest.MockDockerLogs(utils.Docker, containerID, "Usage: pg-delta [command]\n"))

	var stdout, stderr bytes.Buffer
	pgdeltaCmd.SetContext(context.Background())
	pgdeltaCmd.SetOut(&stdout)
	pgdeltaCmd.SetErr(&stderr)

	require.NoError(t, pgdeltaCmd.Help())
	assert.Contains(t, stdout.String(), "Usage: pg-delta [command]")
}
