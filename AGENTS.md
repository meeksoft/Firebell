# Repository Guidelines

## Project Structure & Module Organization
- `cmd/firebell/` holds the CLI entrypoint (`main.go`) that wires config, monitoring, and notifications.
- `internal/` contains domain packages: `config` (defaults and versioning), `detect` (agent discovery), `monitor` (fsnotify + parsing), `notify` (delivery), `daemon` (background lifecycle), `wrap` (PTY command wrapper), and `util`.
- `bin/` is created by builds; do not commit its contents.
- `Makefile` exposes the standard tasks; `install.sh` is the one-line installer; see `CLAUDE.md` for the deeper architecture notes.

## Build, Test, and Development Commands
- `make build` — compile `firebell` into `bin/firebell` with the current version.
- `make test` — run Go tests in `./internal/...`.
- `make install` — build then copy the binary to `~/.firebell/bin`.
- `make clean` — remove build artifacts.
- Quick smoke run: `./bin/firebell --setup` then `./bin/firebell --check` to verify agent detection; `./bin/firebell --stdout` for local notifications.

## Coding Style & Naming Conventions
- Go 1.21+; format with `gofmt` and keep imports tidy. Tabs are standard per Go style.
- Keep packages focused and side-effect free; prefer small, composable helpers in `internal/*`.
- Config structs and CLI flags should use explicit defaults; surface them in `config.Version` and CLI help strings.
- Log and user-facing strings should include the dynamic version when relevant (set via `-ldflags` in the Makefile).
- File and branch names: lowercase-kebab for branches (e.g., `feature/agent-detect`); Go test files end with `_test.go`.

## Testing Guidelines
- Use Go’s `testing` package; name tests `TestXxx` per standard.
- Favor table-driven tests for matchers and detectors in `internal/detect` and `internal/monitor`.
- When touching parsing or notification changes, add regression tests that cover expected log patterns and edge cases (quiet periods, process exit).
- Run `make test` (or `go test ./...`) before opening a PR.

## Commit & Pull Request Guidelines
- Commit messages follow the existing history: short, present-tense summaries; tag releases with `vX.Y.Z: <change>` when bumping versions.
- PRs should describe the behavior change, affected agents/paths, and test evidence (`make test` output or manual steps).
- Link issues when applicable and include screenshots or sample CLI output for user-visible changes (`firebell --check`, `firebell logs -f`, etc.).
- Avoid committing generated artifacts (`bin/`); ensure new config options are documented in `README.md` and `CLAUDE.md` when they affect architecture or setup.
