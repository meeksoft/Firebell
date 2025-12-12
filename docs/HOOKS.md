# Firebell Hook/Integration Solutions

This document describes methods for external applications to receive notifications from Firebell.

## Overview

Firebell supports multiple integration methods for delivering notifications to custom applications, scripts, or services. Choose the method that best fits your use case.

## Available Integration Methods

### 1. Webhooks (HTTP POST)

**Status**: Available in v1.1.0

Firebell sends HTTP POST requests with JSON payloads to configured URLs when events occur.

**Configuration**:
```yaml
notify:
  webhooks:
    - url: "http://localhost:8080/firebell"
      events: ["all"]  # or specific events
    - url: "https://my-server.com/hooks/firebell"
      events: ["cooling", "awaiting", "holding"]
      headers:
        Authorization: "Bearer my-token"
```

**Payload Format**:
```json
{
  "event": "cooling",
  "timestamp": "2025-01-15T10:30:00Z",
  "agent": "Claude Code",
  "title": "Cooling",
  "message": "No activity for 20 seconds",
  "snippet": "optional log context...",
  "metadata": {
    "cpu_percent": 2.5,
    "pid": 12345
  }
}
```

**Use Cases**:
- Custom notification services (Pushover, Ntfy, Telegram bots)
- Home automation (Home Assistant, Node-RED)
- Monitoring dashboards
- Mobile app integrations
- Cross-machine notifications

---

### 2. Unix Domain Socket

**Status**: Available in v1.1.0

Firebell daemon listens on a local Unix socket for client connections. Clients receive a stream of JSON events.

**Socket Path**: `~/.firebell/firebell.sock`

**Configuration**:
```yaml
daemon:
  socket: true  # Enable socket listener
```

**Protocol**:
- Connect to socket
- Receive newline-delimited JSON events
- Optionally send commands (status, subscribe filters)

**Example Client (bash)**:
```bash
nc -U ~/.firebell/firebell.sock
```

**Example Client (Python)**:
```python
import socket
import json

sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
sock.connect(os.path.expanduser("~/.firebell/firebell.sock"))

for line in sock.makefile():
    event = json.loads(line)
    print(f"Event: {event['event']} from {event['agent']}")
```

**Event Format**:
```json
{"event": "cooling", "agent": "Claude Code", "timestamp": "2025-01-15T10:30:00Z", "message": "..."}
```

**Use Cases**:
- Local GUI applications
- Terminal multiplexer integrations (tmux, screen)
- Custom CLI tools
- Real-time dashboards
- Editor/IDE plugins

---

### 3. Event File (JSONL)

**Status**: Available in v1.1.0

Firebell appends events to a JSON Lines file that can be tailed by other processes.

**Event File Path**: `~/.firebell/events.jsonl`

**Configuration**:
```yaml
daemon:
  event_file: true  # Enable event file output
  event_file_max_size: 10485760  # 10MB, rotates when exceeded
```

**Format**: One JSON object per line (JSONL/NDJSON)
```json
{"event":"activity","agent":"Claude Code","timestamp":"2025-01-15T10:30:00Z","title":"Activity Detected"}
{"event":"cooling","agent":"Claude Code","timestamp":"2025-01-15T10:30:20Z","title":"Cooling"}
```

**Example Usage (bash)**:
```bash
# Follow events in real-time
tail -f ~/.firebell/events.jsonl | jq -r '.agent + ": " + .event'

# Filter cooling events only
tail -f ~/.firebell/events.jsonl | jq -c 'select(.event == "cooling")'

# Process with custom script
tail -f ~/.firebell/events.jsonl | while read line; do
  event=$(echo "$line" | jq -r '.event')
  if [ "$event" = "cooling" ]; then
    notify-send "AI Finished" "$(echo "$line" | jq -r '.agent')"
  fi
done
```

**Use Cases**:
- Shell script integrations
- Log aggregation (Loki, Elasticsearch)
- Simple monitoring setups
- Debugging and auditing
- Offline analysis

---

## Comparison Matrix

| Feature | Webhook | Unix Socket | Event File |
|---------|---------|-------------|------------|
| Real-time | Yes | Yes | Near real-time |
| Multiple consumers | Yes | Yes | Yes |
| Remote delivery | Yes | No | No |
| Bidirectional | No | Yes | No |
| Persistence | No | No | Yes |
| Shell-friendly | No | Moderate | Excellent |
| Implementation complexity | Low | Medium | Very low |
| Dependencies | None | None | None |

---

## Event Types

All integration methods use the same event types:

| Event | Description |
|-------|-------------|
| `activity` | AI agent output detected |
| `cooling` | Quiet period elapsed after completion cue (turn finished) |
| `awaiting` | Quiet period elapsed without completion cue (may be waiting for input) |
| `holding` | AI requested tool permission (immediate notification) |
| `process_exit` | Monitored process terminated |
| `daemon_start` | Firebell daemon started |
| `daemon_stop` | Firebell daemon stopping |

### Notification Logic

```
Activity detected → Record cue type
                    ↓
            [Completion cue?]
           /              \
         Yes               No
          ↓                 ↓
    (wait quiet       (wait quiet
     period)           period)
          ↓                 ↓
       Cooling          Awaiting
```

**Completion cues** (trigger "Cooling" after quiet):
- Claude Code: `stop_reason: "end_turn"`
- Codex: `output_text` in content
- Gemini: `type: "gemini"`
- Copilot: `chat/completions succeeded`

**Immediate notifications** (no quiet period):
- `holding`: Tool permission request (`tool_use`, `function_call`)

---

## Security Considerations

### Webhooks
- Use HTTPS for remote endpoints
- Configure authentication headers for sensitive endpoints
- Validate webhook signatures if supported by receiver
- Be cautious with sensitive data in snippets

### Unix Socket
- Socket permissions default to user-only (0600)
- Only processes running as the same user can connect
- No network exposure

### Event File
- File permissions default to user-only (0600)
- Contains notification history; may include code snippets
- Consider log rotation and cleanup

---

## Future Considerations

The following integration methods were evaluated but not implemented:

| Method | Reason Not Implemented |
|--------|----------------------|
| D-Bus | Linux desktop-specific; overkill for CLI tool |
| gRPC | Heavy dependency; complexity not justified |
| MQTT | Requires external broker |
| Redis Pub/Sub | Requires external service |
| Named Pipe (FIFO) | Single consumer limitation; blocking issues |

These may be reconsidered based on user demand.
