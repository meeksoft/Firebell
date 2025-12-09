# CLAUDE.md

Development guide for Firebell v2.0.

## Project Overview

`firebell` is a Go-based log monitoring tool that watches AI CLI outputs and sends real-time notifications. Single binary, no dependencies, runs on Linux.

**Core Purpose**: Answer "Is my AI assistant working/idle/finished?" by monitoring log files and process state.

## Build & Test Commands

```bash
# Build binary to bin/firebell
make build

# Install to ~/.firebell/bin/firebell
make install

# Run all tests
make test

# Run specific test
go test -run TestCodexMatcher ./internal/detect

# Run tests with verbose output
go test -v ./internal/...

# Clean artifacts
make clean
```

## Architecture

### Directory Structure

```
cmd/firebell/main.go              # Entry point (~150 lines)

internal/
├── config/
│   ├── config.go       # Config type definitions
│   ├── loader.go       # YAML/JSON loading with v1 migration
│   ├── flags.go        # CLI flags + subcommands
│   └── setup.go        # Interactive --setup wizard
│
├── monitor/
│   ├── agent.go        # Centralized agent registry
│   ├── state.go        # Consolidated AgentState (replaces 6 v1 maps)
│   ├── watcher.go      # Event-driven fsnotify watcher
│   ├── tailer.go       # Log tailing with buffer pool
│   └── process.go      # PID monitoring with caching
│
├── detect/
│   └── matcher.go      # Matcher interface + implementations
│
├── notify/
│   ├── notifier.go     # Notifier interface + formatting
│   ├── slack.go        # Slack webhook
│   └── stdout.go       # Terminal output
│
├── wrap/
│   ├── pty.go          # Pseudo-terminal handling
│   └── runner.go       # Command wrapping + monitoring
│
├── daemon/
│   ├── lock.go         # flock-based singleton
│   ├── daemon.go       # Daemonization logic
│   ├── logger.go       # Log file management
│   └── cleanup.go      # Log retention cleanup
│
└── util/
    └── buffers.go      # sync.Pool buffer reuse
```

### Key Design Decisions

1. **Event-driven architecture**: Uses fsnotify for <50ms latency (vs 800ms polling in v1)
2. **Consolidated state**: Single `AgentState` struct replaces 6 scattered maps
3. **Centralized registry**: One place to add new AI agents
4. **5 CLI flags**: Down from 23 in v1, config file for advanced options

### Agent Registry (monitor/agent.go)

Adding a new AI agent:

```go
var Registry = map[string]Agent{
    "newagent": {
        Name:         "newagent",
        DisplayName:  "New Agent",
        LogPath:      "~/.newagent/logs",
        ProcessNames: []string{"newagent"},
    },
}
```

### Pattern Matching (detect/matcher.go)

```go
type Matcher interface {
    Match(line string) *Match
}

// Implementations:
// - RegexMatcher: General pattern matching
// - CodexMatcher: JSONL parsing for Codex
// - CopilotMatcher: API event detection
// - ComboMatcher: Try multiple matchers in sequence
```

### Event Loop (monitor/watcher.go)

```go
func (w *Watcher) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case <-w.pidDone:
            w.handleProcessExit(ctx)
        case event := <-w.fsw.Events:
            w.handleFSEvent(ctx, event)
        case <-refreshTicker.C:
            w.refreshFiles()
        case <-quietTicker.C:
            w.checkQuietPeriods(ctx)
        case <-procTicker.C:
            w.sampleProcess(ctx)
        }
    }
}
```

## Configuration

### Config File (~/.firebell/config.yaml)

```yaml
version: "2"
notify:
  type: slack
  slack:
    webhook: "https://hooks.slack.com/..."
agents:
  enabled: []  # Empty = auto-detect
monitor:
  process_tracking: true
  completion_detection: true
  quiet_seconds: 20
output:
  verbosity: normal
  include_snippets: true
advanced:
  poll_interval_ms: 800
  max_recent_files: 3
  force_polling: false
```

### CLI Flags

```
--config PATH    Config file path
--setup          Interactive wizard
--check          Health check
--agent NAME     Filter to specific agent
--stdout         Output to stdout
--verbose        Show all activity (default: only 'likely finished')
--version        Print version
--migrate        Migrate v1 config
```

### Subcommands

```
wrap             Wrap a command and monitor its output
                 Usage: firebell wrap [flags] -- <command> [args...]
                 Flags: --config, --name, --stdout, --verbose

start            Start daemon in background
stop             Stop running daemon
restart          Restart daemon
status           Show daemon status
logs             View daemon logs (use -f to follow)
```

### Notification Behavior

By default, firebell sends **only "likely finished" notifications** (when AI activity stops for the quiet period). This prevents notification spam while still alerting you when your AI is done.

| Mode              | Behavior |
|-------------------|----------|
| Slack (default)   | Only "likely finished" notifications |
| stdout (normal)   | Only "likely finished" notifications |
| stdout --verbose  | All activity + "likely finished" |

Use `--verbose` when you want to see every AI response detection in real-time (useful for debugging or active monitoring).

## Testing

```bash
# All tests
go test ./internal/...

# Specific package
go test ./internal/detect -v

# Run with coverage
go test -cover ./internal/...
```

### Test Files

```
internal/config/config_test.go    # Config validation
internal/detect/matcher_test.go   # Pattern matching
internal/monitor/agent_test.go    # Agent registry
internal/monitor/state_test.go    # State management
internal/monitor/process_test.go  # Process monitoring
internal/notify/notifier_test.go  # Notification formatting
internal/util/buffers_test.go     # Buffer pool
internal/wrap/runner_test.go      # Command wrapping
internal/daemon/daemon_test.go    # Daemon functionality
```

## Key Files Reference

### cmd/firebell/main.go
- Entry point, flag dispatch, signal handling

### internal/config/config.go
- `Config` struct with all settings
- `Validate()` for config validation

### internal/config/loader.go
- `Load()`: YAML/JSON loading with v1 migration
- `Save()`: Write config to YAML
- `MigrateConfig()`: Convert v1 JSON to v2 YAML

### internal/monitor/agent.go
- `Registry`: Map of all supported agents
- `DetectActiveAgents()`: Find agents with recent logs
- `GetAgent()`: Look up by name

### internal/monitor/watcher.go
- `Watcher`: Main monitoring struct
- `Run()`: fsnotify event loop
- `RunPolling()`: Fallback polling mode

### internal/monitor/process.go
- `ProcessMonitor`: PID tracking with caching
- `ReadProcSample()`: Parse /proc/{pid}/stat
- `WatchPID()`: Background exit detection

### internal/detect/matcher.go
- `Matcher` interface
- `CreateMatcher()`: Factory for agent-specific matchers

### internal/wrap/pty.go
- `PTY`: Pseudo-terminal wrapper for interactive commands
- `Start()`: Start command with PTY
- `Wait()`: Wait for command exit

### internal/wrap/runner.go
- `Runner`: Command wrapper with output monitoring
- `Run()`: Execute command and monitor output
- `monitorOutput()`: Pattern matching on stdout

### internal/daemon/lock.go
- `Lock`: flock-based singleton lock
- `TryLock()`: Acquire exclusive lock
- `IsRunning()`: Check if daemon running

### internal/daemon/daemon.go
- `Daemon`: Daemon lifecycle manager
- `Start()`: Start daemon in background
- `Stop()`: Stop running daemon
- `Status()`: Get daemon status

### internal/daemon/logger.go
- `Logger`: Log file management with rotation
- `Log()`: Write log entry (text + JSON)
- `LogEvent()`: Write event with agent context

### internal/daemon/cleanup.go
- `CleanupLogs()`: Remove old log files
- `GetLogFiles()`: List log files

## Performance Notes

- **Idle CPU**: <0.5% (event-driven)
- **Notification latency**: <50ms with fsnotify
- **Memory**: ~15MB RSS
- **File handles**: One per watched file

## Constraints

- **Linux only**: Uses `/proc` filesystem for PID monitoring
- **Text logs only**: Binary logs not supported
- **Max depth**: 4 levels of directory traversal

## Development Workflow

1. Make changes
2. Run tests: `make test`
3. Build: `make build`
4. Test manually: `./bin/firebell --check`
5. Test monitoring: `./bin/firebell --stdout`
