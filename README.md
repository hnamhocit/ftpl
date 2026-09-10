# ftpl

CLI for the Fiber template: feature scaffolding + database migrations (Atlas + sqlc), with zero setup.
`ftpl` auto-downloads and caches `atlas` and `sqlc` on first use — you only need Go and git.

- Generate a full feature (handler / service / repository / dto / tests) + wire file in one command
- Prisma-style migration workflow: `migrate dev`, `diff`, `apply`, `deploy`, `down`, `reset`, `status`, `lint`, `validate`
- Direct-db escape hatches: `db push`, `db seed`
- Shadow database for diff (postgres / mysql / sqlite) — zero-config, auto-cleanup
- Config via environment variables only (12-factor), `.env` for dev, real env vars for prod

## Install

One command (macOS / Linux):

```bash
curl -sSf https://raw.githubusercontent.com/hnamhocit/ftpl/main/install.sh | sh
```

Windows (PowerShell):

```powershell
powershell -c "irm https://raw.githubusercontent.com/hnamhocit/ftpl/main/install.ps1 | iex"
```

The installer clones the repo into a temp dir, builds from source, installs to
`~/.local/bin` (`%USERPROFILE%\.local\bin` on Windows), fixes your PATH, then deletes the
temp dir — even if the build fails halfway. Already inside a checkout? `./install.sh` /
`.\install.ps1` detects it and builds in place instead.

Overrides: `FTPL_REPO`, `FTPL_BRANCH`, `FTPL_INSTALL_DIR`.

Verify:

```bash
ftpl doctor
```

## Requirements

| Tool | Required | Notes |
|---|---|---|
| Go | yes | https://go.dev/doc/install |
| git | yes | also used by the installer |
| atlas | auto | downloaded & cached on first use (min v0.30.0) |
| sqlc | auto | downloaded & cached on first use |

## Configuration

No config files. Environment variables only:

| Variable | Meaning | Default |
|---|---|---|
| `DATABASE_URL` | target database (apply / deploy / down / reset / seed / push) | **required** |
| `DB_SCHEMA` | path to the desired-state schema file | `schema.sql` |
| `MIGRATIONS_DIR` | migrations directory | `migrations` |
| `APP_ENV` | `dev` (default) or `production` | `dev` |
| `DEV_URL` | override the dev-db URL (skip shadow db) | *auto* |

- In **dev**, a `.env` file in the working directory is loaded. Real env vars always win over `.env`.
- In **production** (`APP_ENV=production`), `.env` is ignored completely — only real env vars count.
- Any value can be overridden per invocation with flags: `--dev-url`, `--schema`, `--dir`.

```bash
# .env.example
DATABASE_URL=postgres://${DB_USERNAME}:${DB_PASSWORD_URLENC}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable
APP_ENV=dev
# optional (defaults shown):
DB_SCHEMA=schema.sql
MIGRATIONS_DIR=migrations
```

```bash
cp .env.example .env   # in your project, then edit values
```

## Shadow database

`migrate diff`, `migrate lint` and `migrate down` need a **disposable scratch database** to
replay the migration directory onto. ftpl creates one automatically, on the same server as
`DATABASE_URL`, using the same credentials — you don't need to think about it.

```
ftpl migrate diff ...
  │
  ├─ connect to DATABASE_URL server
  ├─ CREATE DATABASE ftpl_shadow_<random>
  ├─ atlas replays migrations/ + diffs against schema.sql
  └─ DROP DATABASE ftpl_shadow_<random> WITH (FORCE)
```

Supported drivers: **postgres**, **mysql/mariadb**, **sqlite**. For each driver ftpl uses the
right syntax; the database is created, used once, then dropped. Nothing is left behind in
the real database.

| Situation | What ftpl does |
|---|---|
| normal run | auto shadow, auto cleanup |
| you pass `--dev-url` or set `DEV_URL` | ftpl uses that URL directly, no shadow |
| user lacks `CREATE DATABASE` privilege | falls back to `docker://postgres/16/dev` (postgres only) |

The only thing ftpl/atlas leaves in the real database is `atlas_schema_revisions` — the
migration-history table, equivalent to Prisma's `_prisma_migrations`. It is created by
`migrate apply`, not by diff.

## Commands

### Code generation

```bash
ftpl generate resource users            # interactive prompts (transport, CRUD, tests)
ftpl g res users -y                     # accept defaults: restful + CRUD + tests
ftpl g res users --no-crud --no-tests   # wiring skeleton only
ftpl dr users                           # delete a feature (aliases: rm, delete)
```

Creates `internal/features/<name>/` and regenerates `cmd/server/features_gen.go` (wire file).
See [Generated layout](#generated-layout).

### Migrations — daily workflow

```bash
# 1. edit schema.sql to the state you want
# 2. one shot: diff -> apply -> sqlc generate
ftpl migrate dev add_users_table

# the individual pieces, when you need them:
ftpl migrate diff add_users_table   # create the migration file only, no apply
ftpl migrate apply                  # apply pending migrations
ftpl migrate status                 # applied vs pending
ftpl migrate lint                   # destructive-change analysis on the latest file
ftpl migrate validate               # atlas.sum integrity + SQL semantics check
```

### Production & rollback

```bash
ftpl migrate deploy     # apply pending migrations only (no diff, no codegen)
ftpl migrate down       # revert the last migration (reviewed plan; -y to skip confirm)
ftpl migrate reset      # DROP everything, re-apply all, regenerate structs (-y to skip confirm)
```

### Direct database (no migration history)

```bash
ftpl db push    # apply schema.sql straight to the DB, dev-only (-y to skip confirm)
ftpl db seed    # runs seed/main.go (go run) or seed/seed.sql (psql), whichever exists
```

### Health check

```bash
ftpl doctor     # go, git, atlas, sqlc + the active preset
```

Global flags: `-v/--verbose` (debug output), `--dev-url`, `--schema`, `--dir`.

## Typical day

1. `ftpl migrate dev add_x` after editing `schema.sql`
2. commit `migrations/` **and** the sqlc-generated structs
3. CI / production runs `ftpl migrate deploy` with `DATABASE_URL` + `APP_ENV=production`
4. something went wrong? `ftpl migrate down`

## How migrations are generated

State-based diff, not text diff:

```
migrations/*  --replay onto shadow DB-->  state A
schema.sql    --parse----------------->  state B
diff(A, B) -> ordered, dialect-correct SQL -> <version>_<name>.sql + atlas.sum
```

- Generated files contain **up** statements only.
- `migrate down` computes the reverse plan with the same engine (diff B→A on the shadow DB),
  or uses a hand-written `<version>_<name>.down.sql` from the directory if one exists.
- `atlas.sum` hashes the migration directory; manual edits break `migrate validate`
  (repair with `atlas migrate hash`).

## Generated layout

```
internal/features/<name>/
├── feature.go          # module wiring (wire provider)
├── handler.go          # HTTP handlers (Fiber)
├── service.go          # business logic
├── repository.go       # sqlc-backed data access
├── dto.go              # request/response types
├── handler_test.go     # (unless --no-tests)
├── service_test.go     # (unless --no-tests)
└── repository_test.go  # (unless --no-tests)

cmd/server/features_gen.go   # regenerated wire file listing all features
```

## Cache & uninstall

`atlas` / `sqlc` binaries are cached in `~/.cache/ftpl` (Linux) or
`~/Library/Caches/ftpl` (macOS). Delete the directory to force a re-download.

```bash
rm ~/.local/bin/ftpl        # uninstall the CLI
rm -rf ~/.cache/ftpl        # uninstall cached atlas/sqlc
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `permission denied` on `CREATE DATABASE` | ftpl falls back to `docker://` automatically. To use shadow: grant the DB user the `CREATEDB` role. |
| diff/lint/down fails with `docker://` (the fallback) | Docker isn't running → pass `--dev-url` pointing at an empty scratch DB you control. |
| `DATABASE_URL not set in environment (production mode...)` | export it for real; `.env` is ignored when `APP_ENV=production`. |
| weird atlas/sqlc behaviour after an update | `rm -rf ~/.cache/ftpl` and retry. |
| `unknown flag "--tset"` | ftpl suggests the closest flag; run `ftpl <cmd> --help` to list them. |
| anything else | `ftpl doctor`, then re-run with `-v` for debug output. |
