package diff

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/supabase/cli/internal/gen/types"
	"github.com/supabase/cli/internal/pgdelta"
	"github.com/supabase/cli/internal/utils"
)

//go:embed templates/pgdelta_declarative_export.ts
var pgDeltaDeclarativeExportScript string

//go:embed templates/pgdelta_catalog_export.ts
var pgDeltaCatalogExportScript string

// DeclarativeFile mirrors the per-file payload returned by pg-delta declarative
// export so the CLI can materialize structured SQL files on disk.
type DeclarativeFile struct {
	Path       string `json:"path"`
	Order      int    `json:"order"`
	Statements int    `json:"statements"`
	SQL        string `json:"sql"`
}

// DeclarativeOutput is the top-level declarative export envelope emitted by the
// pg-delta script and consumed by db/declarative workflows.
type DeclarativeOutput struct {
	Version int               `json:"version"`
	Mode    string            `json:"mode"`
	Files   []DeclarativeFile `json:"files"`
}

func isPostgresURL(ref string) bool {
	return strings.HasPrefix(ref, "postgres://") || strings.HasPrefix(ref, "postgresql://")
}

// containerRef translates a host-relative catalog file path into the absolute
// path where it appears inside the edge runtime container (CWD mounted at
// /workspace). Postgres URLs and empty strings pass through unchanged.
func containerRef(ref string) string {
	if ref == "" || isPostgresURL(ref) {
		return ref
	}
	return "/workspace/" + ref
}

// DiffPgDelta diffs source and target Postgres configs via pg-delta.
//
// This wrapper preserves the old config-based interface while delegating to
// DiffPgDeltaRef, which also supports catalog-file references.
func DiffPgDelta(ctx context.Context, source, target pgconn.Config, schema []string, options ...func(*pgx.ConnConfig)) (string, error) {
	return DiffPgDeltaRef(ctx, utils.ToPostgresURL(source), utils.ToPostgresURL(target), schema, options...)
}

// DiffPgDeltaRef supports pg-delta diffing across both live database URLs and
// on-disk catalog references used by declarative sync commands.
func DiffPgDeltaRef(ctx context.Context, sourceRef, targetRef string, schema []string, options ...func(*pgx.ConnConfig)) (string, error) {
	args := []string{}
	if len(schema) == 0 {
		args = append(args, "--integration", "supabase")
	} else if integration, err := schemaScopedIntegrationArg(schema); err != nil {
		return "", err
	} else {
		args = append(args, "--integration", integration)
	}
	return pgdelta.DiffSQL(ctx, sourceRef, targetRef, args, options...)
}

// DeclarativeExportPgDelta exports target schema as declarative file payloads
// while keeping a config-based API for existing call sites.
func DeclarativeExportPgDelta(ctx context.Context, source, target pgconn.Config, schema []string, options ...func(*pgx.ConnConfig)) (DeclarativeOutput, error) {
	return DeclarativeExportPgDeltaRef(ctx, utils.ToPostgresURL(source), utils.ToPostgresURL(target), schema, options...)
}

// DeclarativeExportPgDeltaRef exports declarative file payloads using either
// live URLs or catalog references as source/target inputs.
func DeclarativeExportPgDeltaRef(ctx context.Context, sourceRef, targetRef string, schema []string, options ...func(*pgx.ConnConfig)) (DeclarativeOutput, error) {
	env := []string{
		"TARGET=" + containerRef(targetRef),
	}
	if len(sourceRef) > 0 {
		env = append(env, "SOURCE="+containerRef(sourceRef))
	}
	if isPostgresURL(targetRef) {
		if ca, err := types.GetRootCA(ctx, targetRef, options...); err != nil {
			return DeclarativeOutput{}, err
		} else if len(ca) > 0 {
			env = append(env, "PGDELTA_TARGET_SSLROOTCERT="+ca)
		}
	}
	if len(schema) > 0 {
		env = append(env, "INCLUDED_SCHEMAS="+strings.Join(schema, ","))
	}
	binds := []string{utils.EdgeRuntimeId + ":/root/.cache/deno:rw"}
	if cwd, err := os.Getwd(); err == nil {
		binds = append(binds, cwd+":/workspace")
	}
	var stdout, stderr bytes.Buffer
	if err := utils.RunEdgeRuntimeScript(ctx, env, pgDeltaDeclarativeExportScript, binds, "error diffing schema", &stdout, &stderr); err != nil {
		return DeclarativeOutput{}, err
	}
	var result DeclarativeOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return DeclarativeOutput{}, errors.Errorf("failed to parse declarative export output: %w", err)
	}
	return result, nil
}

// ExportCatalogPgDelta snapshots a database/catalog into serialized pg-delta
// catalog JSON so later operations can diff without reconnecting.
func ExportCatalogPgDelta(ctx context.Context, targetRef, role string, options ...func(*pgx.ConnConfig)) (string, error) {
	env := []string{
		"TARGET=" + targetRef,
	}
	if len(role) > 0 {
		env = append(env, "ROLE="+role)
	}
	if isPostgresURL(targetRef) {
		if ca, err := types.GetRootCA(ctx, targetRef, options...); err != nil {
			return "", err
		} else if len(ca) > 0 {
			env = append(env, "PGDELTA_TARGET_SSLROOTCERT="+ca)
		}
	}
	binds := []string{
		utils.EdgeRuntimeId + ":/root/.cache/deno:rw",
	}
	if cwd, err := os.Getwd(); err == nil {
		binds = append(binds, cwd+":/workspace")
	}
	var stdout, stderr bytes.Buffer
	if err := utils.RunEdgeRuntimeScript(ctx, env, pgDeltaCatalogExportScript, binds, "error diffing schema", &stdout, &stderr); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// TODO: Remove this once pg-delta support an `extend` fields within integrations, to simply
// extend the base supabase with additional schemas filters
func schemaScopedIntegrationArg(schema []string) (string, error) {
	filter := map[string]any{
		"and": []any{
			map[string]any{
				"or": []any{
					map[string]any{
						"and": []any{
							map[string]any{"type": "schema", "operation": "create", "scope": "object"},
							map[string]any{"not": map[string]any{"schema": []string{
								"_analytics", "_realtime", "_supavisor", "auth", "cron", "extensions",
								"graphql", "graphql_public", "information_schema", "net", "pgbouncer",
								"pgmq", "pgmq_public", "pgsodium", "pgsodium_masks", "pgtle", "realtime",
								"storage", "supabase_functions", "supabase_migrations", "vault",
							}}},
						},
					},
					map[string]any{"type": "extension", "operation": "create", "scope": "object"},
					map[string]any{
						"not": map[string]any{
							"or": []any{
								map[string]any{"schema": []string{
									"_analytics", "_realtime", "_supavisor", "auth", "cron", "extensions",
									"graphql", "graphql_public", "information_schema", "net", "pgbouncer",
									"pgmq", "pgmq_public", "pgsodium", "pgsodium_masks", "pgtle", "realtime",
									"storage", "supabase_functions", "supabase_migrations", "vault",
								}},
								map[string]any{"owner": []string{
									"anon", "authenticated", "authenticator", "cli_login_postgres",
									"dashboard_user", "pgbouncer", "pgsodium_keyholder", "pgsodium_keyiduser",
									"pgsodium_keymaker", "pgtle_admin", "service_role", "supabase_admin",
									"supabase_auth_admin", "supabase_etl_admin", "supabase_functions_admin",
									"supabase_read_only_user", "supabase_realtime_admin",
									"supabase_replication_admin", "supabase_storage_admin", "supabase_superuser",
								}},
								map[string]any{
									"and": []any{
										map[string]any{"type": "role", "scope": "membership"},
										map[string]any{"member": []string{
											"anon", "authenticated", "authenticator", "cli_login_postgres",
											"dashboard_user", "pgbouncer", "pgsodium_keyholder", "pgsodium_keyiduser",
											"pgsodium_keymaker", "pgtle_admin", "service_role", "supabase_admin",
											"supabase_auth_admin", "supabase_etl_admin", "supabase_functions_admin",
											"supabase_read_only_user", "supabase_realtime_admin",
											"supabase_replication_admin", "supabase_storage_admin", "supabase_superuser",
										}},
									},
								},
							},
						},
					},
				},
			},
			map[string]any{"schema": schema},
		},
	}
	integration := map[string]any{
		"filter": filter,
		"serialize": []map[string]any{
			{
				"when": map[string]any{
					"type":      "schema",
					"operation": "create",
					"scope":     "object",
					"owner": []string{
						"anon", "authenticated", "authenticator", "cli_login_postgres",
						"dashboard_user", "pgbouncer", "pgsodium_keyholder", "pgsodium_keyiduser",
						"pgsodium_keymaker", "pgtle_admin", "service_role", "supabase_admin",
						"supabase_auth_admin", "supabase_etl_admin", "supabase_functions_admin",
						"supabase_read_only_user", "supabase_realtime_admin",
						"supabase_replication_admin", "supabase_storage_admin", "supabase_superuser",
					},
				},
				"options": map[string]any{"skipAuthorization": true},
			},
		},
	}
	body, err := json.Marshal(integration)
	if err != nil {
		return "", errors.Errorf("failed to encode pg-delta integration: %w", err)
	}
	relativePath := filepath.Join(utils.TempDir, "pgdelta", "integration.json")
	if err := os.MkdirAll(filepath.Dir(relativePath), 0755); err != nil {
		return "", errors.Errorf("failed to create pg-delta temp dir: %w", err)
	}
	if err := os.WriteFile(relativePath, body, 0644); err != nil {
		return "", errors.Errorf("failed to write pg-delta integration: %w", err)
	}
	return containerRef(relativePath), nil
}
