# Firebell

**Real-time activity monitoring for AI CLI tools**

Firebell watches log files from AI coding assistants (Claude Code, Codex, GitHub Copilot, Gemini CLI, OpenCode, Crush, Qwen Code, Amazon Q) and sends notifications when activity is detected. Know when your AI assistant is working, idle, or finished—without checking the terminal.

## Quick Install

```bash
curl -fsSL https://raw.githubusercontent.com/meeksoft/Firebell/main/install.sh | bash
```

Requires: Go 1.21+ and Git

## Features

- **Multi-CLI Support** - Monitors Claude Code, Codex, Copilot, Gemini, OpenCode, Crush, Qwen Code, Amazon Q
- **Event-Driven** - Uses fsnotify for instant notifications (<50ms latency)
- **Daemon Mode** - Run as background service with singleton enforcement and log management
- **Command Wrapping** - Wrap any command and monitor its output in real-time
- **Process Monitoring** - Tracks CPU usage and detects when processes exit
- **Smart Detection** - Format-aware pattern matching (JSONL for Claude/Codex, API events for Copilot)
- **Completion Detection** - "Cooling" notifications after quiet periods
- **Awaiting Detection** - Notifications when AI is waiting for input or tool approval
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
| `firebell events` | View event file for external integrations |
| `firebell events -f` | Follow event file (like tail -f) |
| `firebell listen` | Connect to daemon socket for real-time events |
| `firebell webhook test URL` | Test a webhook endpoint |
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

## External Integrations

Firebell provides multiple ways for external applications to receive notifications:

### Event File (Default)

Events are automatically written to `~/.firebell/events.jsonl`:

```bash
# View recent events
firebell events

# Follow in real-time
firebell events -f

# Process with jq
tail -f ~/.firebell/events.jsonl | jq -r '.agent + ": " + .event'
```

### Webhooks

Send events to HTTP endpoints:

```yaml
# In ~/.firebell/config.yaml
notify:
  webhooks:
    - url: "http://localhost:8080/firebell"
      events: ["all"]  # or ["cooling", "activity"]
```

Test a webhook: `firebell webhook test http://localhost:8080/webhook`

### Unix Socket

Connect to the daemon socket for real-time events:

```bash
# Enable in config
daemon:
  socket: true

# Listen for events
firebell listen
firebell listen --json  # Raw JSON output
```

See [docs/HOOKS.md](docs/HOOKS.md) for complete integration documentation.

## Supported AI Agents

| Agent | Log Path | Detection Method |
|-------|----------|------------------|
| Claude Code | `~/.claude/projects` | JSONL parsing (`stop_reason`) |
| Codex | `~/.codex/sessions` | JSONL parsing (`function_call`, `output_text`) |
| GitHub Copilot | `~/.copilot/session-state` | JSONL parsing (`assistant.turn_end`, `toolRequests`) |
| Google Gemini | `~/.gemini/tmp` | JSON pattern matching |
| OpenCode | `~/.local/share/opencode/log` | Pattern matching |
| Crush | `~/.local/share/crush` | slog/JSON parsing |
| Qwen Code | `~/.qwen/logs/openai` | OpenAI API JSONL parsing |
| Amazon Q | `~/.local/state/amazonq/logs` | Pattern matching |

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
- **Claude Code**: Parses JSONL, detects `stop_reason: "end_turn"` (completion) and `stop_reason: "tool_use"` (holding)
- **Codex**: Parses JSONL, detects `output_text` (completion) and `function_call` (holding)
- **Copilot**: Parses session-state JSONL, detects `assistant.turn_end` (completion) and `toolRequests` (holding)
- **Gemini**: Pattern matches `type: "gemini"` (completion) and tool names (holding)
- **Qwen Code**: Parses OpenAI API logs, detects `finish_reason: "stop"` (completion) and `tool_calls` (holding)
- **OpenCode**: Pattern matches `turn.complete` (completion) and `tool.confirm` (holding)
- **Crush**: Parses slog JSON, detects completion and tool confirmation patterns
- **Amazon Q**: Pattern matches response/chat events and tool permissions
- **Others**: Regex matching for `agent_message|assistant_message`

### Notification Types

Firebell sends different notifications based on detected state:

| Notification | Trigger | Description |
|-------------|---------|-------------|
| **Cooling** | Quiet period after completion cue | AI finished its turn, likely waiting for your input |
| **Awaiting** | Quiet period after activity (no completion) | AI stopped mid-task, may be waiting for input |
| **Holding** | Tool permission request detected | AI needs permission to run a tool (immediate) |
| **Activity** | AI output detected | AI is actively working (verbose mode only) |
| **Process Exit** | Monitored process terminated | AI CLI process has exited |

### Completion Detection

"Cooling" notifications after quiet periods:
- Triggers after configurable silence duration (default: 20s)
- Includes CPU usage if process tracking enabled
- Requires a "completion cue" (e.g., `end_turn`, `output_text`) before quiet period

### Awaiting & Holding Detection

- **Holding** (immediate): When agent explicitly requests tool permission (`tool_use`, `function_call`)
- **Awaiting** (inferred): When activity stops without a completion cue (quiet period elapsed)

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
