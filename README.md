# ftpl

CLI for the Fiber template: feature scaffolding + database migrations (Atlas + sqlc), with zero setup.
`ftpl` auto-downloads and caches `atlas` and `sqlc` on first use — you only need Go and git on your machine.

- Generate a full feature (handler / service / repository / dto / tests) + wire file in one command
- Prisma-style migration workflow: `migrate dev`, `diff`, `apply`, `deploy`, `down`, `reset`, `status`, `lint`, `validate`
- Direct-db escape hatches: `db push`, `db seed`
- Config via environment variables only (12-factor), `.env` for dev, real env vars for prod
- `doctor` to check your toolchain in one shot

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
| Docker | no | only if you keep the default dev-db preset, see [Dev database](#dev-database) |

## Configuration

No config files. Environment variables only:

| Variable | Meaning | Default |
|---|---|---|
| `DATABASE_URL` | target database (apply / deploy / down / reset / seed / push) | **required** |
| `DB_SCHEMA` | path to the desired-state schema file | `schema/schema.sql` |
| `MIGRATIONS_DIR` | migrations directory | `migrations` |
| `APP_ENV` | `dev` (default) or `production` | `dev` |

- In **dev**, a `.env` file in the working directory is loaded. Real env vars always win over `.env`.
- In **production** (`APP_ENV=production`), `.env` is ignored completely — only real env vars count.
- Any value can be overridden per invocation with flags: `--dev-url`, `--schema`, `--dir`.

```bash
cp .env.example .env   # in your project, then edit DATABASE_URL
```

```bash
# .env.example
DATABASE_URL=postgres://postgres:postgres@localhost:5432/myapp?sslmode=disable
# optional (defaults shown):
DB_SCHEMA=schema/schema.sql
MIGRATIONS_DIR=migrations
APP_ENV=dev
```

### Dev database

`migrate diff`, `migrate lint` and `migrate down` need a **disposable scratch database**:
Atlas replays the whole migration directory onto it from scratch to compute the current
state. Never point it at your real database — existing objects would be dropped.

- Default preset: `docker://postgres/16/dev` — a throwaway container, created and destroyed automatically.
- No Docker? Create an empty scratch database once and pass it via `--dev-url`:

```bash
createdb scratch
ftpl migrate dev add_users --dev-url postgres://me@localhost:5432/scratch
```

How you run your actual database (docker compose, local postgres/mysql, remote…) is
entirely your choice — ftpl only cares about `DATABASE_URL`.

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
# 1. edit schema/schema.sql to the state you want
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

1. `ftpl migrate dev add_x` after editing `schema/schema.sql`
2. commit `migrations/` **and** the sqlc-generated structs
3. CI / production runs `ftpl migrate deploy` with `DATABASE_URL` + `APP_ENV=production`
4. something went wrong? `ftpl migrate down`

## How migrations are generated

State-based diff, not text diff:

```
migrations/*  --replay onto scratch dev DB-->  state A
schema.sql    --parse---------------------->  state B
diff(A, B) -> ordered, dialect-correct SQL -> <version>_<name>.sql + atlas.sum
```

- Generated files contain **up** statements only.
- `migrate down` computes the reverse plan with the same engine (diff B→A on the dev DB),
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
| `atlas X is too old (ftpl needs >= 0.30.0)` | update your system atlas, or remove it from PATH so ftpl uses its cached one |
| diff/lint/down fails with `docker://...` | Docker isn't running → pass `--dev-url` pointing at an empty scratch DB |
| `DATABASE_URL not set in environment (production mode...)` | export it for real; `.env` is ignored when `APP_ENV=production` |
| weird atlas/sqlc behaviour after an update | `rm -rf ~/.cache/ftpl` and retry |
| `unknown flag "--tset"` | ftpl suggests the closest flag; run `ftpl <cmd> --help` to list them |
| anything else | `ftpl doctor`, then re-run with `-v` for debug output |
