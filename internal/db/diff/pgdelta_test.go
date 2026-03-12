package diff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaScopedIntegrationArgWritesMergedIntegration(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	tempDir := t.TempDir()
	require.NoError(t, os.Chdir(tempDir))
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	arg, err := schemaScopedIntegrationArg([]string{"public"})

	require.NoError(t, err)
	assert.Equal(t, "/workspace/supabase/.temp/pgdelta/integration.json", arg)

	body, err := os.ReadFile(filepath.Join(tempDir, "supabase/.temp/pgdelta/integration.json"))
	require.NoError(t, err)
	assert.Contains(t, string(body), `"schema":["public"]`)
	assert.Contains(t, string(body), `"skipAuthorization":true`)
}
