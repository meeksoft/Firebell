# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`firebell` is a Go-based log monitoring tool that watches AI CLI outputs and sends real-time notifications. Single binary, no dependencies, runs on Linux.

**Core Purpose**: Answer "Is my AI assistant working/idle/finished?" by monitoring log files and process state.

## Build & Test Commands

```bash
make build              # Build binary to bin/firebell
make test               # Run all tests
make install            # Install to ~/.firebell/bin/firebell
make clean              # Clean artifacts

go test -run TestCodexMatcher ./internal/detect   # Run specific test
go test -v ./internal/...                         # Tests with verbose output
go test -cover ./internal/...                     # Tests with coverage

./bin/firebell --check   # Health check
./bin/firebell --stdout  # Test monitoring output
```

## Architecture

### Directory Structure

```
cmd/firebell/main.go     # Entry point, flag dispatch, signal handling

internal/
├── config/              # Config types, YAML loading, CLI flags, setup wizard
├── monitor/             # Agent registry, state, fsnotify watcher, tailer, process tracking
├── detect/              # Matcher interface + agent-specific implementations
├── notify/              # Notifier interface, Slack webhook, stdout output
├── wrap/                # PTY handling, command wrapping with output monitoring
├── daemon/              # flock singleton, daemonization, log management, cleanup
└── util/                # sync.Pool buffer reuse
```

### Key Design Decisions

1. **Event-driven architecture**: Uses fsnotify for <50ms latency (vs 800ms polling in v1)
2. **Consolidated state**: Single `AgentState` struct replaces 6 scattered maps
3. **Centralized registry**: One place to add new AI agents (`monitor/agent.go`)
4. **5 CLI flags**: Down from 23 in v1, config file for advanced options

### Adding a New AI Agent

Add to the registry in `monitor/agent.go`:

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

Then add a matcher in `detect/matcher.go` if custom parsing is needed.

### Pattern Matching (detect/matcher.go)

```go
type Matcher interface {
    Match(line string) *Match
}

type MatchType int
const (
    MatchActivity // Normal activity (no completion signal)
    MatchComplete // Turn complete (triggers Cooling after quiet)
    MatchAwaiting // Explicit waiting for user input
    MatchHolding  // Waiting for tool approval (immediate notification)
)

// Implementations:
// - ClaudeMatcher: JSONL parsing with stop_reason detection
// - CodexMatcher: JSONL parsing for function_call/output_text
// - GeminiMatcher: JSON pattern matching for type/tool names
// - CopilotMatcher: API event detection
// - RegexMatcher: General pattern matching
// - ComboMatcher: Try multiple matchers in sequence
```

### Event Loop (monitor/watcher.go)

The main monitoring loop handles: fsnotify events, process exit detection, file refresh, quiet period checks, and process sampling.

### Notification Logic (monitor/watcher.go, monitor/state.go)

Firebell tracks the last cue type per agent to determine notification type after quiet period:

- **MatchComplete** → After quiet period → "Cooling" notification
- **MatchActivity** → After quiet period → "Awaiting" notification (inferred)
- **MatchHolding** → Immediate "Holding" notification

State tracks: `LastCue` (timestamp), `LastCueType` (MatchType), `QuietNotified` (bool)

## Configuration

Config file: `~/.firebell/config.yaml`

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
```

### CLI Flags & Subcommands

```
--config PATH    Config file path
--setup          Interactive wizard
--check          Health check
--agent NAME     Filter to specific agent
--stdout         Output to stdout
--verbose        Show all activity (default: only 'cooling')

wrap             Wrap command and monitor output
start/stop       Daemon control
status/logs      Daemon info
```

## Coding Style

- Go 1.21+, format with `gofmt`, keep imports tidy
- Keep packages focused and side-effect free
- Config structs and CLI flags should use explicit defaults
- User-facing strings should include dynamic version (set via `-ldflags` in Makefile)
- Table-driven tests for matchers and detectors

## Constraints

- **Linux only**: Uses `/proc` filesystem for PID monitoring
- **Text logs only**: Binary logs not supported
- **Max depth**: 4 levels of directory traversal

## Commit Guidelines

- Short, present-tense summaries
- Tag releases with `vX.Y.Z: <change>` when bumping versions
- Do not commit `bin/` artifacts
