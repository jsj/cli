package pgdelta

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/db/start"
	"github.com/supabase/cli/internal/db/shadow"
	"github.com/supabase/cli/internal/gen/types"
	migrationnew "github.com/supabase/cli/internal/migration/new"
	"github.com/supabase/cli/internal/utils"
)

const (
	BunImage           = "oven/bun:1.2-alpine"
	PgDeltaPackage     = "@supabase/pg-delta@1.0.0-alpha.7"
	pgdeltaWorkspace   = "/workspace"
	pgdeltaCacheDir    = "/bun-cache"
	pgdeltaCachePrefix = "supabase_bun_cache_"
)

func buildCLIArgs(args ...string) []string {
	return append([]string{"x", PgDeltaPackage}, args...)
}

func isPostgresURL(ref string) bool {
	return strings.HasPrefix(ref, "postgres://") || strings.HasPrefix(ref, "postgresql://")
}

func containerRef(ref string) string {
	if ref == "" || isPostgresURL(ref) {
		return ref
	}
	return filepath.ToSlash(filepath.Join(pgdeltaWorkspace, ref))
}

func bunCacheVolume() string {
	return pgdeltaCachePrefix + utils.Config.ProjectId
}

func workspaceBind() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", errors.Errorf("failed to resolve workspace: %w", err)
	}
	return cwd + ":" + pgdeltaWorkspace, nil
}

func workspacePathRef(path string) (string, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", "", errors.Errorf("failed to resolve workspace: %w", err)
	}
	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, path)
	}
	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return "", "", errors.Errorf("failed to resolve project-relative path: %w", err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", "", errors.Errorf("path must be inside the current project: %s", path)
	}
	return absPath, filepath.ToSlash(filepath.Join(pgdeltaWorkspace, relPath)), nil
}

func runCLI(ctx context.Context, args, env, binds []string, stdout, stderr io.Writer) (int, error) {
	hostBinds := append([]string{}, binds...)
	hostBinds = append(hostBinds, bunCacheVolume()+":"+pgdeltaCacheDir)
	config := container.Config{
		Image:      BunImage,
		Env: append([]string{
			"BUN_INSTALL_CACHE_DIR=" + pgdeltaCacheDir,
		}, env...),
		Cmd:        buildCLIArgs(args...),
		WorkingDir: pgdeltaWorkspace,
	}
	hostConfig := container.HostConfig{
		Binds:       hostBinds,
		NetworkMode: network.NetworkHost,
	}
	containerID, err := utils.DockerStart(ctx, config, hostConfig, network.NetworkingConfig{}, "")
	if err != nil {
		return 0, err
	}
	defer utils.DockerRemove(containerID)

	logs, err := utils.Docker.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	if err != nil {
		return 0, errors.Errorf("failed to read docker logs: %w", err)
	}
	defer logs.Close()

	if _, err := stdcopy.StdCopy(stdout, stderr, logs); err != nil {
		return 0, errors.Errorf("failed to copy docker logs: %w", err)
	}
	resp, err := utils.Docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, errors.Errorf("failed to inspect docker container: %w", err)
	}
	return int(resp.State.ExitCode), nil
}

func RunCLI(ctx context.Context, args, env, binds []string, stdout, stderr io.Writer) error {
	exitCode, err := runCLI(ctx, args, env, binds, stdout, stderr)
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return errors.Errorf("error running pg-delta cli: exit %d", exitCode)
	}
	return nil
}

func hasAnyFlag(args []string, names ...string) bool {
	for i, arg := range args {
		for _, name := range names {
			if arg == name || strings.HasPrefix(arg, name+"=") {
				return true
			}
			if i > 0 && args[i-1] == name {
				return true
			}
		}
	}
	return false
}

func flagValue(args []string, names ...string) string {
	for i, arg := range args {
		for _, name := range names {
			if arg == name && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(arg, name+"=") {
				return strings.TrimPrefix(arg, name+"=")
			}
		}
	}
	return ""
}

func normalizePassthroughPathFlags(args []string) ([]string, error) {
	normalized := append([]string{}, args...)
	for i := 0; i < len(normalized); i++ {
		arg := normalized[i]
		value := ""
		valueIdx := -1
		switch {
		case arg == "--output" || arg == "-o" || arg == "--path" || arg == "-p" || arg == "--integration":
			if i+1 < len(normalized) {
				valueIdx = i + 1
				value = normalized[valueIdx]
			}
		case arg == "--source" || arg == "-s" || arg == "--target" || arg == "-t":
			if i+1 < len(normalized) {
				valueIdx = i + 1
				value = normalized[valueIdx]
			}
		case strings.HasPrefix(arg, "--output="):
			valueIdx = i
			value = strings.TrimPrefix(arg, "--output=")
		case strings.HasPrefix(arg, "--path="):
			valueIdx = i
			value = strings.TrimPrefix(arg, "--path=")
		case strings.HasPrefix(arg, "--integration="):
			valueIdx = i
			value = strings.TrimPrefix(arg, "--integration=")
		case strings.HasPrefix(arg, "--source="):
			valueIdx = i
			value = strings.TrimPrefix(arg, "--source=")
		case strings.HasPrefix(arg, "--target="):
			valueIdx = i
			value = strings.TrimPrefix(arg, "--target=")
		}
		if valueIdx < 0 || len(value) == 0 {
			continue
		}
		normalizedValue := value
		switch {
		case arg == "--source" || arg == "-s" || strings.HasPrefix(arg, "--source="),
			arg == "--target" || arg == "-t" || strings.HasPrefix(arg, "--target="):
			if !isPostgresURL(value) {
				if _, ref, err := workspacePathRef(value); err != nil {
					return nil, err
				} else {
					normalizedValue = ref
				}
			}
		case arg == "--integration" || strings.HasPrefix(arg, "--integration="):
			if strings.HasSuffix(value, ".json") || strings.Contains(value, "/") || strings.HasPrefix(value, ".") {
				if _, ref, err := workspacePathRef(value); err != nil {
					return nil, err
				} else {
					normalizedValue = ref
				}
			}
		default:
			if _, ref, err := workspacePathRef(value); err != nil {
				return nil, err
			} else {
				normalizedValue = ref
			}
		}
		if valueIdx == i {
			parts := strings.SplitN(arg, "=", 2)
			normalized[i] = parts[0] + "=" + normalizedValue
		} else {
			normalized[valueIdx] = normalizedValue
		}
	}
	return normalized, nil
}

func buildPlanArgs(sourceRef, targetRef string, extraArgs []string) ([]string, error) {
	extraArgs, err := normalizePassthroughPathFlags(extraArgs)
	if err != nil {
		return nil, err
	}
	args := []string{"plan"}
	if len(sourceRef) > 0 && !hasAnyFlag(extraArgs, "--source", "-s") {
		args = append(args, "--source", containerRef(sourceRef))
	}
	if len(targetRef) > 0 && !hasAnyFlag(extraArgs, "--target", "-t") {
		args = append(args, "--target", containerRef(targetRef))
	}
	if !hasAnyFlag(extraArgs, "--integration", "--filter", "--serialize") {
		args = append(args, "--integration", "supabase")
	}
	return append(args, extraArgs...), nil
}

func buildDeclarativeExportArgs(sourceRef, targetRef, outputPath string, extraArgs []string) ([]string, error) {
	extraArgs, err := normalizePassthroughPathFlags(extraArgs)
	if err != nil {
		return nil, err
	}
	args := []string{"declarative", "export"}
	if len(sourceRef) > 0 && !hasAnyFlag(extraArgs, "--source", "-s") {
		args = append(args, "--source", containerRef(sourceRef))
	}
	if len(targetRef) > 0 && !hasAnyFlag(extraArgs, "--target", "-t") {
		args = append(args, "--target", containerRef(targetRef))
	}
	if len(outputPath) > 0 && !hasAnyFlag(extraArgs, "--output", "-o") {
		args = append(args, "--output", outputPath)
	}
	if formatOptions := utils.GetPgdeltaFormatOptions(); len(formatOptions) > 0 && !hasAnyFlag(extraArgs, "--format-options") {
		args = append(args, "--format-options", formatOptions)
	}
	if !hasAnyFlag(extraArgs, "--integration", "--filter", "--serialize") {
		args = append(args, "--integration", "supabase")
	}
	return append(args, extraArgs...), nil
}

func buildCatalogExportArgs(targetRef, outputPath string, extraArgs []string) ([]string, error) {
	extraArgs, err := normalizePassthroughPathFlags(extraArgs)
	if err != nil {
		return nil, err
	}
	args := []string{"catalog-export"}
	if len(targetRef) > 0 && !hasAnyFlag(extraArgs, "--target", "-t") {
		args = append(args, "--target", containerRef(targetRef))
	}
	if len(outputPath) > 0 && !hasAnyFlag(extraArgs, "--output", "-o") {
		args = append(args, "--output", outputPath)
	}
	return append(args, extraArgs...), nil
}

func buildDeclarativeApplyArgs(targetRef, schemaPath string, extraArgs []string) ([]string, error) {
	extraArgs, err := normalizePassthroughPathFlags(extraArgs)
	if err != nil {
		return nil, err
	}
	args := []string{"declarative", "apply"}
	if len(schemaPath) > 0 && !hasAnyFlag(extraArgs, "--path", "-p") {
		args = append(args, "--path", schemaPath)
	}
	if len(targetRef) > 0 && !hasAnyFlag(extraArgs, "--target", "-t") {
		args = append(args, "--target", containerRef(targetRef))
	}
	return append(args, extraArgs...), nil
}

func DiffSQL(ctx context.Context, sourceRef, targetRef string, extraArgs []string, options ...func(*pgx.ConnConfig)) (string, error) {
	env, err := pgdeltaEnv(ctx, sourceRef, targetRef, options...)
	if err != nil {
		return "", err
	}
	workspace, err := workspaceBind()
	if err != nil {
		return "", err
	}
	args, err := buildPlanArgs(sourceRef, targetRef, extraArgs)
	if err != nil {
		return "", err
	}
	if !hasAnyFlag(args, "--format") {
		args = append(args, "--format", "sql")
	}
	if !hasAnyFlag(args, "--role") {
		args = append(args, "--role", "postgres")
	}
	var stdout, stderr bytes.Buffer
	exitCode, err := runCLI(ctx, args, env, []string{workspace}, &stdout, &stderr)
	if err != nil {
		return "", err
	}
	if exitCode == 0 {
		return "", nil
	}
	if exitCode != 2 {
		return "", errors.Errorf("error diffing schema: exit %d:\n%s", exitCode, stderr.String())
	}
	return stdout.String(), nil
}

func pgdeltaEnv(ctx context.Context, sourceRef, targetRef string, options ...func(*pgx.ConnConfig)) ([]string, error) {
	var env []string
	if isPostgresURL(sourceRef) {
		if ca, err := types.GetRootCA(ctx, sourceRef, options...); err != nil {
			return nil, err
		} else if len(ca) > 0 {
			env = append(env, "PGDELTA_SOURCE_SSLROOTCERT="+ca)
		}
	}
	if isPostgresURL(targetRef) {
		if ca, err := types.GetRootCA(ctx, targetRef, options...); err != nil {
			return nil, err
		} else if len(ca) > 0 {
			env = append(env, "PGDELTA_TARGET_SSLROOTCERT="+ca)
		}
	}
	return env, nil
}

func PlanMigration(ctx context.Context, name string, config pgconn.Config, extraArgs []string, stdout, stderr io.Writer, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	if len(strings.TrimSpace(name)) == 0 {
		name = "pgdelta_plan"
	}
	outputPath := migrationnew.GetMigrationPath(utils.GetCurrentTimestamp(), name)
	hostOutputPath, containerOutputPath, err := workspacePathRef(outputPath)
	if err != nil {
		return err
	}
	if err := utils.MkdirIfNotExistFS(fsys, filepath.Dir(hostOutputPath)); err != nil {
		return err
	}
	sourceRef := flagValue(extraArgs, "--source", "-s")
	if len(sourceRef) == 0 {
		sourceShadow, err := shadow.CreateDatabase(ctx, utils.Config.Db.ShadowPort)
		if err != nil {
			return err
		}
		defer utils.DockerRemove(sourceShadow)
		if err := start.WaitForHealthyService(ctx, utils.Config.Db.HealthTimeout, sourceShadow); err != nil {
			return err
		}
		if err := shadow.Migrate(ctx, sourceShadow, fsys, options...); err != nil {
			return err
		}
		sourceConfig := pgconn.Config{
			Host:     utils.Config.Hostname,
			Port:     utils.Config.Db.ShadowPort,
			User:     "postgres",
			Password: utils.Config.Db.Password,
			Database: "postgres",
		}
		sourceRef = utils.ToPostgresURL(sourceConfig)
	}
	targetRef := flagValue(extraArgs, "--target", "-t")
	if len(targetRef) == 0 {
		targetRef = utils.ToPostgresURL(config)
	}
	env, err := pgdeltaEnv(ctx, sourceRef, targetRef, options...)
	if err != nil {
		return err
	}
	workspace, err := workspaceBind()
	if err != nil {
		return err
	}
	args, err := buildPlanArgs(sourceRef, targetRef, extraArgs)
	if err != nil {
		return err
	}
	if hasAnyFlag(args, "--output", "-o", "--format") {
		return RunCLI(ctx, args, env, []string{workspace}, stdout, stderr)
	}
	previewExitCode, err := runCLI(ctx, args, env, []string{workspace}, stdout, stderr)
	if err != nil {
		return err
	}
	if previewExitCode == 0 {
		return nil
	}
	if previewExitCode != 2 {
		return errors.Errorf("error running pg-delta cli: exit %d", previewExitCode)
	}
	sqlArgs, err := buildPlanArgs(sourceRef, targetRef, extraArgs)
	if err != nil {
		return err
	}
	sqlArgs = append(sqlArgs,
		"--format", "sql",
		"--output", containerOutputPath,
	)
	if !hasAnyFlag(sqlArgs, "--role") {
		sqlArgs = append(sqlArgs, "--role", "postgres")
	}
	var planStdout bytes.Buffer
	if err := RunCLI(ctx, sqlArgs, env, []string{workspace}, &planStdout, stderr); err != nil {
		return err
	}
	return nil
}

func ExportDeclarative(ctx context.Context, config pgconn.Config, outputPath string, extraArgs []string, stdout, stderr io.Writer, options ...func(*pgx.ConnConfig)) error {
	targetRef := flagValue(extraArgs, "--target", "-t")
	if len(targetRef) == 0 {
		targetRef = utils.ToPostgresURL(config)
	}
	return ExportDeclarativeRef(ctx, "", targetRef, outputPath, extraArgs, stdout, stderr, options...)
}

func ExportDeclarativeRef(ctx context.Context, sourceRef, targetRef, outputPath string, extraArgs []string, stdout, stderr io.Writer, options ...func(*pgx.ConnConfig)) error {
	hostOutputPath, containerOutputPath, err := workspacePathRef(outputPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hostOutputPath), 0755); err != nil {
		return errors.Errorf("failed to create declarative output parent: %w", err)
	}
	env, err := pgdeltaEnv(ctx, sourceRef, targetRef, options...)
	if err != nil {
		return err
	}
	workspace, err := workspaceBind()
	if err != nil {
		return err
	}
	args, err := buildDeclarativeExportArgs(sourceRef, targetRef, containerOutputPath, extraArgs)
	if err != nil {
		return err
	}
	return RunCLI(ctx, args, env, []string{workspace}, stdout, stderr)
}

func ExportCatalog(ctx context.Context, config pgconn.Config, outputPath string, extraArgs []string, stdout, stderr io.Writer, options ...func(*pgx.ConnConfig)) error {
	hostOutputPath, containerOutputPath, err := workspacePathRef(outputPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(hostOutputPath), 0755); err != nil {
		return errors.Errorf("failed to create catalog output parent: %w", err)
	}
	targetRef := flagValue(extraArgs, "--target", "-t")
	if len(targetRef) == 0 {
		targetRef = utils.ToPostgresURL(config)
	}
	env, err := pgdeltaEnv(ctx, "", targetRef, options...)
	if err != nil {
		return err
	}
	workspace, err := workspaceBind()
	if err != nil {
		return err
	}
	args, err := buildCatalogExportArgs(targetRef, containerOutputPath, extraArgs)
	if err != nil {
		return err
	}
	return RunCLI(ctx, args, env, []string{workspace}, stdout, stderr)
}

func ApplyDeclarativePath(ctx context.Context, config pgconn.Config, schemaPath string, stdout, stderr io.Writer, extraArgs ...string) error {
	_, containerSchemaPath, err := workspacePathRef(schemaPath)
	if err != nil {
		return err
	}
	workspace, err := workspaceBind()
	if err != nil {
		return err
	}
	targetRef := flagValue(extraArgs, "--target", "-t")
	if len(targetRef) == 0 {
		targetRef = utils.ToPostgresURL(config)
	}
	env, err := pgdeltaEnv(ctx, "", targetRef)
	if err != nil {
		return err
	}
	args, err := buildDeclarativeApplyArgs(targetRef, containerSchemaPath, extraArgs)
	if err != nil {
		return err
	}
	return RunCLI(ctx, args, env, []string{workspace}, stdout, stderr)
}
