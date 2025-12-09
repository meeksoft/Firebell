# Firebell

**Real-time activity monitoring for AI CLI tools**

Firebell watches log files from AI coding assistants (Claude Code, GitHub Codex, GitHub Copilot, Gemini, OpenCode) and sends notifications when activity is detected. Know when your AI assistant is working, idle, or finished—without checking the terminal.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/meeksoft/Firebell/main/install.sh | bash
```

Requires: Go 1.21+ and Git

## Features

- **Multi-CLI Support** - Monitors Claude Code, GitHub Codex, Copilot, Gemini, and OpenCode
- **Event-Driven** - Uses fsnotify for instant notifications (<50ms latency)
- **Daemon Mode** - Run as background service with singleton enforcement and log management
- **Command Wrapping** - Wrap any command and monitor its output in real-time
- **Process Monitoring** - Tracks CPU usage and detects when processes exit
- **Smart Detection** - Format-aware pattern matching (JSONL for Codex, API events for Copilot)
- **Completion Detection** - "Likely finished" notifications after quiet periods
- **Simple Setup** - Interactive wizard, auto-detection, minimal configuration
- **Zero Dependencies** - Single Go binary, no runtime requirements

## Quick Start

```bash
# Option 1: One-line install
curl -fsSL https://raw.githubusercontent.com/meeksoft/Firebell/main/install.sh | bash

# Option 2: Manual build
git clone https://github.com/meeksoft/Firebell.git
cd Firebell
make install

# Add to PATH (if not already done)
export PATH="$HOME/.firebell/bin:$PATH"

# Run interactive setup
firebell --setup

# Start monitoring
firebell
```

## Usage

```bash
# Interactive setup (first time)
firebell --setup

# Health check - see which agents are active
firebell --check

# Monitor all auto-detected agents
firebell

# Monitor specific agent
firebell --agent claude

# Test mode (print to terminal instead of Slack)
firebell --stdout
```

## Commands

| Command | Description |
|---------|-------------|
| `firebell` | Start monitoring in foreground (auto-detects active agents) |
| `firebell start` | Start daemon in background |
| `firebell stop` | Stop running daemon |
| `firebell restart` | Restart daemon |
| `firebell status` | Show daemon status (running/stopped, PID, uptime) |
| `firebell logs` | View daemon logs |
| `firebell logs -f` | Follow daemon logs (like tail -f) |
| `firebell wrap -- CMD` | Wrap a command and monitor its output |
| `firebell --setup` | Interactive configuration wizard |
| `firebell --check` | Health check and status |
| `firebell --agent NAME` | Monitor specific agent |
| `firebell --stdout` | Output to terminal (testing) |
| `firebell --migrate` | Migrate v1 config to v2 |
| `firebell --version` | Print version |

## Command Wrapping

Wrap any command to monitor its output in real-time:

```bash
# Wrap Claude Code
firebell wrap -- claude

# Wrap with custom display name
firebell wrap --name "My AI Assistant" -- claude --debug

# Wrap any AI script
firebell wrap --name "GPT Script" -- python my_gpt_script.py

# Test with stdout notifications
firebell wrap --stdout -- codex
```

**How it works:**
- Creates a pseudo-terminal (PTY) for full interactivity
- Monitors stdout/stderr in real-time
- Applies the same pattern matchers as log monitoring
- Sends notifications when AI activity is detected
- Preserves colors and interactive features

## Daemon Mode

Run Firebell as a background service:

```bash
# Start daemon
firebell start

# Start with specific agent
firebell start --agent claude

# Check status
firebell status

# View logs
firebell logs
firebell logs -f  # Follow mode

# Stop daemon
firebell stop

# Restart daemon
firebell restart
```

**Features:**
- **Singleton enforcement** - Only one daemon can run at a time (uses flock)
- **Automatic logging** - Logs to `~/.firebell/logs/firebell-YYYY-MM-DD.log`
- **Log retention** - Automatically cleans up old logs (configurable)
- **Graceful shutdown** - Responds to SIGTERM/SIGINT

**Log format:**
Logs are written in both human-readable and JSON format:
```
2025-12-07 10:30:00 [INFO] firebell daemon starting
  JSON: {"timestamp":"2025-12-07T10:30:00Z","level":"INFO","message":"firebell daemon starting"}
```

## Supported AI Agents

| Agent | Log Path | Detection Method |
|-------|----------|------------------|
| Claude Code | `~/.claude/debug` | Regex pattern matching |
| GitHub Codex | `~/.codex/sessions` | JSONL parsing |
| GitHub Copilot | `~/.copilot/logs` | API event detection |
| Google Gemini | `~/.gemini/tmp` | Regex pattern matching |
| OpenCode | `~/.opencode/logs` | Regex pattern matching |

## Configuration

Configuration is stored in `~/.firebell/config.yaml`:

```yaml
version: "2"

notify:
  type: slack  # or "stdout"
  slack:
    webhook: "https://hooks.slack.com/services/YOUR/WEBHOOK/URL"

agents:
  enabled: []  # Empty = auto-detect

monitor:
  process_tracking: true
  completion_detection: true
  quiet_seconds: 20

output:
  verbosity: normal  # minimal, normal, or verbose
  include_snippets: true
  snippet_lines: 12

daemon:
  log_retention_days: 7  # Days to keep logs (0 = forever)
```

Run `firebell --setup` to configure interactively.

## Slack Webhook Setup

1. Go to https://api.slack.com/apps
2. Create a new app or select existing
3. Navigate to "Incoming Webhooks"
4. Activate webhooks and create a new one
5. Select the channel for notifications
6. Copy the webhook URL
7. Run `firebell --setup` and paste the URL

## How It Works

### Event-Driven Monitoring

Firebell uses `fsnotify` for instant file change detection:
- Watches agent log directories
- Triggers on file writes
- Falls back to polling if fsnotify unavailable

### Pattern Matching

Format-aware detection for each agent:
- **Codex**: Parses JSONL, detects `type:"response_item"` with `role:"assistant"`
- **Copilot**: Matches `"chat/completions succeeded"` API events
- **Others**: Regex matching for `agent_message|assistant_message`

### Completion Detection

"Likely finished" notifications after quiet periods:
- Triggers after configurable silence duration (default: 20s)
- Includes CPU usage if process tracking enabled

### Process Monitoring

When enabled:
- Auto-detects AI CLI processes
- Samples CPU usage every 5 seconds
- Sends notification when process exits

## Migrating from v1

If you have a v1 config (`~/.firebell/config.json`):

```bash
# Automatic migration
firebell --migrate

# Or run setup wizard
firebell --setup
```

## Development

```bash
# Build
make build

# Run tests
make test

# Install locally
make install

# Clean
make clean
```

See [CLAUDE.md](CLAUDE.md) for architecture documentation.

## Troubleshooting

### No notifications

1. Check config: `cat ~/.firebell/config.yaml`
2. Verify agents: `firebell --check`
3. Test with stdout: `firebell --stdout`

### Missing events

1. Ensure AI CLI is writing logs
2. Check log directory exists
3. Try specific agent: `firebell --agent claude`

### Process monitoring not working

1. Linux only (uses `/proc` filesystem)
2. Verify process is running: `ps aux | grep claude`

## License

MIT

## Contributing

Contributions welcome! Please add tests and update documentation.
