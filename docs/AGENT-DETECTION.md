# Agent Detection Reference

This document describes how Firebell monitors each AI CLI agent, including log locations, formats, and detection patterns. Use this as a reference for understanding the implementation or building similar tools.

## Overview

Firebell detects three types of activity:

| MatchType | Description | Notification |
|-----------|-------------|--------------|
| `MatchActivity` | Agent is actively working | None (unless verbose mode) |
| `MatchComplete` | Agent finished its turn | "Cooling" after quiet period |
| `MatchHolding` | Agent waiting for tool approval | "Holding" after quiet period |

**Quiet Period**: Notifications are sent after a configurable silence duration (default: 20s) to avoid spam during rapid activity.

### Per-Instance vs Aggregated Tracking

By default, Firebell aggregates state by agent type (e.g., all Claude instances share one state). Enable per-instance mode to track each log file separately:

```yaml
monitor:
  per_instance: true
```

**Aggregated (default):**
- State keyed by agent name: `map[string]*AgentState` → `"claude"`
- Multiple log files → single notification when all are quiet

**Per-Instance:**
- State keyed by file path: `map[string]*InstanceState` → `/path/to/log.jsonl`
- Each log file → independent notifications
- Display names include identifiers: "Claude Code (abc12345)"

For Claude, the project hash from the path is used. For other agents, the filename is used.

---

## Claude Code

**Matcher**: `ClaudeMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.claude/projects` |
| Log Pattern | `*.jsonl` |
| Process Names | `claude`, `claude-code` |
| Format | JSONL (one JSON object per line) |

### Log Format

Claude Code writes structured JSONL logs with conversation data:

```json
{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"Done!"}]}}
```

### Detection Logic

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| `type == "assistant"` AND `message.stop_reason == "end_turn"` | Complete | Turn finished |
| `type == "assistant"` AND `message.stop_reason == "tool_use"` | Holding | Tool permission needed |
| `type == "assistant"` (other cases) | Activity | Working |
| `type != "assistant"` | No match | - |

### Key Fields

- `type`: Must be `"assistant"` to match
- `message.stop_reason`: Determines completion state
  - `"end_turn"` → Complete
  - `"tool_use"` → Holding (extracts tool name from `message.content[].name`)
  - Other/missing → Activity

### Example Log Lines

```json
// Complete (end_turn)
{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"I've finished the task."}]}}

// Holding (tool_use)
{"type":"assistant","message":{"stop_reason":"tool_use","content":[{"type":"tool_use","name":"Bash","id":"toolu_123"}]}}

// Activity (streaming)
{"type":"assistant","message":{"content":[{"type":"text","text":"Working on..."}]}}
```

---

## OpenAI Codex

**Matcher**: `CodexMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.codex/sessions` |
| Log Pattern | `*.jsonl`, `*.json` |
| Process Names | `codex` |
| Format | JSONL |

### Log Format

Codex uses `response_item` type with nested payload:

```json
{"type":"response_item","payload":{"type":"function_call","name":"shell_command","call_id":"call_123"}}
```

### Detection Logic

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| `type == "response_item"` AND `payload.type == "function_call"` | Holding | Function call pending |
| `type == "response_item"` AND `payload.type == "message"` AND `payload.role == "assistant"` AND has `output_text` | Complete | Response finished |
| `type == "response_item"` AND `payload.role == "assistant"` (no output_text) | Activity | Streaming |

### Key Fields

- `type`: Must be `"response_item"`
- `payload.type`: `"function_call"` or `"message"`
- `payload.role`: `"assistant"` for AI responses
- `payload.content[].type`: Look for `"output_text"` to detect completion

### Example Log Lines

```json
// Holding (function_call)
{"type":"response_item","payload":{"type":"function_call","name":"shell_command","call_id":"call_123"}}

// Complete (output_text)
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Done!"}]}}

// Activity (streaming)
{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"reasoning","text":"thinking..."}]}}
```

---

## GitHub Copilot

**Matcher**: `CopilotMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.copilot/session-state` |
| Log Pattern | `*.jsonl` |
| Process Names | `copilot` |
| Format | JSONL |

### Log Format

Copilot writes session state with typed events:

```json
{"type":"assistant.turn_end","data":{"turnId":"0"},"id":"abc123"}
```

### Detection Logic

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| `type == "assistant.turn_end"` | Complete | Turn finished |
| `type == "assistant.message"` AND `data.toolRequests` exists and non-empty | Holding | Tool request pending |
| `type == "assistant.message"` (no tool requests) | Activity | Message |
| `type == "tool.execution_start"` | Activity | Tool running |
| `type == "user.message"` | Activity | User input |
| Contains `"chat/completions succeeded"` (legacy) | Complete | API success |

### Key Fields

- `type`: Event type string
- `data.toolRequests`: Array of pending tool requests
- `data.toolRequests[0].name`: Tool name for metadata

### Example Log Lines

```json
// Complete (turn_end)
{"type":"assistant.turn_end","data":{"turnId":"0"},"id":"abc123"}

// Holding (tool request)
{"type":"assistant.message","data":{"toolRequests":[{"name":"bash","arguments":{}}]}}

// Activity (message)
{"type":"assistant.message","data":{"content":"Hello"}}

// Activity (tool execution)
{"type":"tool.execution_start","data":{"toolName":"view"}}
```

---

## Google Gemini CLI

**Matcher**: `GeminiMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.gemini/tmp` |
| Log Pattern | `*.json` |
| Process Names | `gemini` |
| Format | Pretty-printed JSON (not JSONL) |

### Log Format

Gemini uses pretty-printed JSON files, so we match individual lines:

```json
      "type": "gemini",
```

### Detection Logic

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| Line contains `"type": "gemini"` or `"type":"gemini"` | Complete | Gemini response |
| Line contains `"toolCalls"` | Activity | Tool calls array |
| Line contains `"name":` AND known tool pattern | Holding | Tool call detected |

### Known Tool Patterns

- `shell_command`, `run_shell_command`
- `read_file`, `write_file`, `edit_file`
- `list_dir`

### Example Log Lines

```json
// Complete
      "type": "gemini",

// Holding (tool call)
          "name": "run_shell_command",

// Activity
      "toolCalls": [
```

---

## Qwen Code

**Matcher**: `QwenMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.qwen/logs/openai` |
| Log Pattern | `*.jsonl`, `*.json` |
| Process Names | `qwen`, `qwen-code` |
| Format | JSONL (OpenAI API format) |

### Log Format

Qwen Code is a Gemini CLI fork that logs OpenAI-compatible API calls:

```json
{"choices":[{"finish_reason":"stop","message":{"content":"Done!"}}]}
```

### Detection Logic

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| `choices[0].finish_reason == "stop"` | Complete | Response finished |
| `choices[0].finish_reason == "tool_calls"` or `"function_call"` | Holding | Tool call |
| `choices` exists but no `finish_reason` | Activity | Streaming |
| `messages` array exists | Activity | Request logged |

### Key Fields

- `choices[0].finish_reason`: Completion indicator
- `choices[0].message.tool_calls[0].function.name`: Tool name
- `messages`: Present in request logs

### Example Log Lines

```json
// Complete
{"choices":[{"finish_reason":"stop","message":{"content":"Done!"}}]}

// Holding (tool_calls)
{"choices":[{"finish_reason":"tool_calls","message":{"tool_calls":[{"function":{"name":"shell_exec"}}]}}]}

// Activity (streaming)
{"choices":[{"delta":{"content":"Hello"}}]}

// Activity (request)
{"messages":[{"role":"user","content":"hi"}],"model":"qwen3-coder"}
```

---

## OpenCode (SST)

**Matcher**: `OpenCodeMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.local/share/opencode/log` |
| Log Pattern | `*.log` |
| Process Names | `opencode` |
| Format | Timestamped text logs |

### Log Format

OpenCode writes timestamped log files with structured messages:

```
2025-01-09T10:00:00 turn.complete duration=5s
```

### Detection Logic

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| Contains `tool.execute` or `executing tool` | Activity | Tool running |
| Contains `tool.confirm` or `awaiting confirmation` or `permission` | Holding | Permission needed |
| Contains `turn.complete` or `response.complete` or `assistant.done` | Complete | Turn finished |
| Contains `assistant` or `response` or `message` | Activity | General activity |

### Example Log Lines

```
// Complete
2025-01-09T10:00:00 turn.complete duration=5s

// Holding
2025-01-09T10:00:00 tool.confirm name=bash awaiting confirmation

// Activity
2025-01-09T10:00:00 tool.execute name=bash
2025-01-09T10:00:00 assistant response received
```

---

## Crush (Charmbracelet)

**Matcher**: `CrushMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.local/share/crush` |
| Log Pattern | `*.log`, `*.jsonl` |
| Process Names | `crush` |
| Format | slog JSON or text |

### Log Format

Crush uses Go's slog package, which can output JSON:

```json
{"level":"info","msg":"turn complete","duration":"5s"}
```

### Detection Logic

**JSON Mode** (if line parses as JSON):

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| `msg` contains `tool` AND `confirm` | Holding | Tool confirmation |
| `msg` contains `complete` or `done` | Complete | Turn complete |
| Any other `msg` | Activity | General activity |

**Text Mode** (fallback):

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| Contains `tool` AND (`confirm` or `permission`) | Holding | Tool confirmation |
| Contains `complete` or `finished` | Complete | Finished |
| Contains `assistant` or `response` | Activity | Activity |

### Example Log Lines

```json
// Complete (JSON)
{"level":"info","msg":"turn complete","duration":"5s"}

// Holding (JSON)
{"level":"info","msg":"tool confirm required","tool":"bash"}

// Activity (JSON)
{"level":"info","msg":"processing request"}

// Holding (text)
time=2025-01-09 tool permission requested for bash

// Complete (text)
time=2025-01-09 task complete
```

---

## Amazon Q CLI

**Matcher**: `AmazonQMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.local/state/amazonq/logs` |
| Log Pattern | `*.log` |
| Process Names | `q`, `amazonq` |
| Format | JSON or text |

### Detection Logic

**JSON Mode**:

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| `type == "tool_use"` or `"tool_call"` | Holding | Tool use |
| `type == "response_complete"` or `"turn_complete"` | Complete | Complete |
| Any other JSON | Activity | Activity |

**Text Mode**:

| Condition | MatchType | Reason |
|-----------|-----------|--------|
| Contains `tool` AND (`permission` or `confirm`) | Holding | Permission |
| Contains `complete` or `finished` or `done` | Complete | Complete |
| Contains `response` or `message` or `chat` | Activity | Activity |

### Example Log Lines

```json
// Holding
{"type":"tool_use","name":"bash","input":{}}

// Complete
{"type":"response_complete","content":"Done!"}

// Activity
{"event":"processing","data":{}}

// Holding (text)
[INFO] tool permission required for bash

// Complete (text)
[INFO] response complete
```

---

## Plandex

**Matcher**: `PlandexMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.plandex-home` |
| Log Pattern | `*.log`, `*.json` |
| Process Names | `plandex` |
| Format | JSON or text |

### Detection Logic

**JSON Mode** (if `status` field exists):

| Status Values | MatchType |
|---------------|-----------|
| `building`, `running`, `streaming` | Activity |
| `complete`, `finished`, `done` | Complete |
| `pending`, `waiting`, `blocked` | Holding |

**Text Mode**:

| Pattern | MatchType |
|---------|-----------|
| `plan complete`, `changes applied`, `finished`, `done building` | Complete |
| `waiting for`, `confirm`, `review changes`, `pending approval` | Holding |
| `building`, `planning`, `streaming`, `processing`, `loading`, `running` | Activity |

### Example Log Lines

```json
// Activity (JSON)
{"status":"running","task":"planning"}

// Complete (JSON)
{"status":"complete","changes":5}

// Holding (JSON)
{"status":"waiting","reason":"review"}

// Complete (text)
Plan complete! Applied 5 changes.

// Holding (text)
Please confirm the changes before applying

// Activity (text)
Building plan for task...
```

---

## Aider

**Matcher**: `AiderMatcher` (`internal/detect/matcher.go`)

### Configuration

| Property | Value |
|----------|-------|
| Log Path | `~/.aider` |
| Log Pattern | `*.history`, `*.md`, `*.log` |
| Process Names | `aider` |
| Format | Markdown history or JSON LLM logs |

### Important Note

Aider writes logs **per-project** by default (`.aider.chat.history.md` in each project directory). The `~/.aider` path requires user configuration via:
- `AIDER_CHAT_HISTORY_FILE` environment variable
- `.aider.conf.yml` configuration

**Recommended**: Use `firebell wrap -- aider` for reliable monitoring.

### Detection Logic

**Markdown Markers**:

| Pattern | MatchType | Reason |
|---------|-----------|--------|
| Line starts with `####` or `---` | Activity | Section marker |

**Text Patterns**:

| Pattern | MatchType |
|---------|-----------|
| `applied edit`, `wrote`, `created`, `updated` | Complete |
| `y/n`, `confirm`, `proceed?`, `allow?` | Holding |
| `thinking`, `searching`, `analyzing`, `generating` | Activity |

**JSON Mode** (for LLM history files):

| Condition | MatchType |
|-----------|-----------|
| `role == "assistant"` AND has `tool_calls` | Holding |
| `role == "assistant"` (no tool_calls) | Complete |
| Any other JSON | Activity |

**Fallback**: Lines > 20 characters are treated as Activity.

### Example Log Lines

```markdown
// Activity (markdown)
#### Changes to file.go

// Complete (text)
Applied edit to src/main.go

// Holding (text)
Apply these changes? (y/n)

// Activity (text)
Thinking about the best approach...
```

```json
// Complete (JSON)
{"role":"assistant","content":"Here is the solution"}

// Holding (JSON)
{"role":"assistant","tool_calls":[{"name":"edit"}]}
```

---

## Fallback Matcher

**Matcher**: `FallbackMatcher` (`internal/detect/matcher.go`)

Used for unknown agents not in the registry. Provides intelligent pattern matching.

### JSON Detection

Checks fields in priority order:

1. **Tool Detection** (Holding):
   - `tool_calls` array exists
   - `function_call` object exists
   - `type` is `tool_use`, `tool_call`, or `function_call`

2. **Completion Detection**:
   - `stop_reason`: `end_turn`, `stop` → Complete; `tool_use` → Holding
   - `finish_reason`: `stop`, `end` → Complete; `tool_calls`, `function_call` → Holding
   - `choices[0].finish_reason` (OpenAI format): Same as above

3. **Status Field**:
   - `complete`, `completed`, `done`, `finished`, `success` → Complete
   - `pending`, `waiting`, `blocked`, `paused` → Holding
   - `running`, `processing`, `streaming`, `generating` → Activity

4. **Event Field**:
   - `complete`, `turn_complete`, `response_complete`, `turn_end` → Complete
   - `tool_request`, `permission_required`, `confirmation_needed` → Holding

5. **Type/Role Fields** (Activity):
   - `type`: `assistant`, `gemini`, `ai`, `model`
   - `role`: `assistant`, `ai`, `model`

6. **Content Fields** (Activity):
   - Has `message`, `content`, or `response` field

### Text Detection

**Holding Patterns**:
```
waiting for, confirm, permission, approve, allow,
y/n, yes/no, proceed?, continue?, accept?,
review changes, pending approval, requires confirmation
```

**Complete Patterns**:
```
complete, completed, finished, done, success,
applied edit, changes applied, wrote file, created file,
task complete, turn complete, response complete
```

**Activity Patterns**:
```
thinking, generating, processing, analyzing, searching,
loading, streaming, running, executing, building,
assistant, response, message, output
```

---

## Adding a New Agent

1. **Add to Registry** (`internal/monitor/agent.go`):

```go
"newagent": {
    Name:         "newagent",
    DisplayName:  "New Agent",
    LogPath:      "~/.newagent/logs",
    LogPatterns:  []string{"*.jsonl"},
    ProcessNames: []string{"newagent"},
},
```

2. **Create Matcher** (`internal/detect/matcher.go`):

```go
type NewAgentMatcher struct {
    agent string
}

func NewNewAgentMatcher() *NewAgentMatcher {
    return &NewAgentMatcher{agent: "newagent"}
}

func (m *NewAgentMatcher) Match(line string) *Match {
    // Parse line and return appropriate Match
    // Return nil for no match
}
```

3. **Register Matcher** in `CreateMatcher()`:

```go
case "newagent":
    return NewNewAgentMatcher()
```

4. **Add Tests** (`internal/detect/matcher_test.go`):

```go
func TestNewAgentMatcher(t *testing.T) {
    m := NewNewAgentMatcher()
    tests := []struct {
        name      string
        line      string
        wantMatch bool
        wantType  MatchType
    }{
        // Add test cases
    }
    // Run tests
}
```

5. **Update Documentation**:
   - Add to README.md supported agents table
   - Add section to this document
   - Update CLAUDE.md matcher list

---

## Testing Matchers

Run matcher tests:

```bash
# All matcher tests
go test -v ./internal/detect/...

# Specific matcher
go test -run TestClaudeMatcher ./internal/detect

# With coverage
go test -cover ./internal/detect/...
```

## Debugging

Enable verbose mode to see all matches:

```bash
firebell --verbose --stdout
```

Check which agents are detected:

```bash
firebell --check
```
