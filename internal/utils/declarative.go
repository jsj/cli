package utils

import (
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"
)

const DefaultDeclarativeDirConfigPath = "./declarative-schemas"

func GetDeclarativeDirConfigPath() string {
	path := strings.TrimSpace(Config.Experimental.Pgdelta.DeclarativeDirPath)
	if len(path) == 0 {
		return DefaultDeclarativeDirConfigPath
	}
	return path
}

func GetDeclarativeDirPath() (string, error) {
	path := filepath.Clean(GetDeclarativeDirConfigPath())
	if filepath.IsAbs(path) {
		return "", errors.New("declarative_dir_path must be relative to the supabase project directory")
	}
	fullPath := filepath.Join(SupabaseDirPath, path)
	relPath, err := filepath.Rel(SupabaseDirPath, fullPath)
	if err != nil {
		return "", errors.Errorf("failed to resolve declarative_dir_path: %w", err)
	}
	if relPath == "." {
		return "", errors.New("declarative_dir_path must point to a subdirectory within the supabase project directory")
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", errors.New("declarative_dir_path must stay within the supabase project directory")
	}
	return filepath.Join(SupabaseDirPath, relPath), nil
}

func GetDeclarativeSchemaPathsEntry() (string, error) {
	dirPath, err := GetDeclarativeDirPath()
	if err != nil {
		return "", err
	}
	relPath, err := filepath.Rel(SupabaseDirPath, dirPath)
	if err != nil {
		return "", errors.Errorf("failed to resolve schema_paths entry: %w", err)
	}
	return "./" + filepath.ToSlash(relPath), nil
}

func GetPgdeltaFormatOptions() string {
	return strings.TrimSpace(Config.Experimental.Pgdelta.FormatOptions)
}

func cloneArgs(args []string) []string {
	return append([]string{}, args...)
}

func GetPgdeltaGenerateArgs() []string {
	return cloneArgs(Config.Experimental.Pgdelta.GenerateArgs)
}

func GetPgdeltaMigrateArgs() []string {
	return cloneArgs(Config.Experimental.Pgdelta.MigrateArgs)
}

func GetPgdeltaApplyArgs() []string {
	return cloneArgs(Config.Experimental.Pgdelta.ApplyArgs)
}
