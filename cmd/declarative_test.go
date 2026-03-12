package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeclarativeCommandRegistration(t *testing.T) {
	declarativeCmd, _, err := rootCmd.Find([]string{"declarative"})
	require.NoError(t, err)
	require.NotNil(t, declarativeCmd)

	assert.Equal(t, "declarative", declarativeCmd.Name())
	assert.Equal(t, groupLocalDev, declarativeCmd.GroupID)
	assert.True(t, IsExperimental(declarativeCmd))
}

func TestDeclarativeCommandShape(t *testing.T) {
	declarativeCmd, _, err := rootCmd.Find([]string{"declarative"})
	require.NoError(t, err)

	generateCmd, _, err := rootCmd.Find([]string{"declarative", "generate"})
	require.NoError(t, err)
	assert.Same(t, declarativeCmd, generateCmd.Parent())

	migrateCmd, _, err := rootCmd.Find([]string{"declarative", "migrate"})
	require.NoError(t, err)
	assert.Same(t, declarativeCmd, migrateCmd.Parent())

	applyCmd, _, err := rootCmd.Find([]string{"declarative", "apply"})
	require.NoError(t, err)
	assert.Same(t, declarativeCmd, applyCmd.Parent())

	statusCmd, _, err := rootCmd.Find([]string{"declarative", "status"})
	require.NoError(t, err)
	assert.Same(t, declarativeCmd, statusCmd.Parent())
}

func TestDeclarativeGenerateRequiresExplicitSource(t *testing.T) {
	err := declarativeGenerateCmd.RunE(declarativeGenerateCmd, nil)

	require.ErrorContains(t, err, "must set exactly one")
}

func TestLegacyDbDeclarativeCommandIsHidden(t *testing.T) {
	assert.True(t, dbDeclarativeCmd.Hidden)
}
