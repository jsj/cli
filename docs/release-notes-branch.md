# Branch release notes

## Added

- Added a new experimental `supabase db declarative` command group:
  - `supabase db declarative generate`
  - `supabase db declarative sync`
- Added declarative workflow support for:
  - generating structured SQL files under `supabase/declarative`
  - syncing from migrations to declarative files
  - syncing from declarative files back to a migration
- Added pg-delta catalog/declarative export templates used by new declarative flows.

## Changed

- `supabase db pull` now supports `--use-pg-delta` to export declarative schema (experimental mode).
- `supabase db diff` accepts `--use-pg-delta` and can be enabled branch-wide with `SUPABASE_EXPERIMENTAL_PG_DELTA=1`.
- `db remote commit` now follows the same pg-delta selection logic as `db pull`.
- Experimental gating now treats `SUPABASE_EXPERIMENTAL_PG_DELTA=1` as enabling experimental mode for pg-delta command paths.

## Experimental notes

- Declarative and pg-delta paths are experimental.
- Use `--experimental` for experimental commands.
- For repeated pg-delta workflows in this branch, set:

```bash
SUPABASE_EXPERIMENTAL_PG_DELTA=1
```

## Migration guidance

- Use migration files when you need a chronological SQL history for deployment pipelines.
- Use declarative files when you want current-state schema representation in `supabase/declarative`.
- Recommended loop for declarative-first work:
  1. `supabase db declarative generate --experimental`
  2. edit declarative SQL files
  3. `supabase db declarative sync --to-migrations --experimental`
  4. validate with `supabase db diff` / `supabase db reset`

## Compatibility considerations

- Teams already using migration-only workflows can continue unchanged.
- Declarative output updates schema paths in config to reference declarative SQL files; review config diffs when adopting the declarative workflow.
