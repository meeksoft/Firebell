# Firebell v1.1.0 Roadmap: Hook/Integration System

**STATUS: COMPLETE**

This document outlines the implementation plan for external application hooks in Firebell v1.1.0.

## Release Goal

Enable external applications to receive Firebell notifications through three integration methods:
1. Webhooks (HTTP POST)
2. Event File (JSONL)
3. Unix Domain Socket

---

## Phase 1: Event File (JSONL)

**Priority**: Highest (simplest, unblocks other phases)
**Estimated Scope**: Small

### Milestone 1.1: Core Event File Implementation

#### Tasks

- [ ] **1.1.1** Add event file config options to `internal/config/config.go`
  - `daemon.event_file` (bool, default: true)
  - `daemon.event_file_path` (string, default: `~/.firebell/events.jsonl`)
  - `daemon.event_file_max_size` (int, default: 10MB)

- [ ] **1.1.2** Create `internal/notify/eventfile.go`
  - Implement `Notifier` interface
  - Write JSONL format with timestamp, event type, agent, message, snippet
  - Handle file rotation when max size exceeded

- [ ] **1.1.3** Create unified event structure in `internal/notify/event.go`
  - Define `Event` struct used by all hook methods
  - JSON serialization with consistent field names
  - Event type constants (activity, cooling, process_exit, daemon_start, daemon_stop)

- [ ] **1.1.4** Add tests for event file notifier
  - Test JSONL format output
  - Test file rotation
  - Test concurrent writes

- [ ] **1.1.5** Update daemon to emit lifecycle events
  - Emit `daemon_start` on startup
  - Emit `daemon_stop` on graceful shutdown

### Milestone 1.2: Event File Integration

#### Tasks

- [ ] **1.2.1** Wire event file notifier into main notification flow
  - Modify `NewNotifier` to support multiple outputs
  - Event file runs alongside primary notifier (Slack/stdout)

- [ ] **1.2.2** Add `--events` flag to show event file path
  - `firebell --events` prints path and tails if `-f` provided

- [ ] **1.2.3** Update documentation
  - Add event file section to README.md
  - Update CLAUDE.md with new config options

---

## Phase 2: Webhooks (HTTP POST)

**Priority**: High
**Estimated Scope**: Medium

### Milestone 2.1: Webhook Notifier

#### Tasks

- [ ] **2.1.1** Add webhook config options to `internal/config/config.go`
  - `notify.webhooks` (array of webhook configs)
  - Each webhook: `url`, `events`, `headers`, `timeout`

- [ ] **2.1.2** Create `internal/notify/webhook.go`
  - Implement `Notifier` interface
  - HTTP POST with JSON body
  - Custom headers support
  - Configurable timeout (default: 10s)
  - Event filtering per webhook

- [ ] **2.1.3** Implement retry logic
  - Retry failed webhooks with exponential backoff
  - Max 3 retries
  - Non-blocking (don't delay other notifications)

- [ ] **2.1.4** Add webhook tests
  - Test successful delivery
  - Test retry behavior
  - Test event filtering
  - Test custom headers

### Milestone 2.2: Webhook Configuration & CLI

#### Tasks

- [ ] **2.2.1** Update setup wizard for webhooks
  - Add webhook URL prompt in `--setup`
  - Test webhook connectivity during setup

- [ ] **2.2.2** Add `firebell webhook test <url>` subcommand
  - Send test event to specified URL
  - Report success/failure with response details

- [ ] **2.2.3** Update documentation
  - Add webhook section to README.md
  - Include payload examples
  - Document security best practices

---

## Phase 3: Unix Domain Socket

**Priority**: Medium
**Estimated Scope**: Medium-Large

### Milestone 3.1: Socket Server

#### Tasks

- [ ] **3.1.1** Add socket config options to `internal/config/config.go`
  - `daemon.socket` (bool, default: false)
  - `daemon.socket_path` (string, default: `~/.firebell/firebell.sock`)

- [ ] **3.1.2** Create `internal/daemon/socket.go`
  - Unix domain socket listener
  - Accept multiple concurrent connections
  - Broadcast events to all connected clients
  - Clean up socket file on shutdown

- [ ] **3.1.3** Implement client connection handling
  - Send newline-delimited JSON events
  - Handle client disconnects gracefully
  - Optional: command parsing for status/subscribe

- [ ] **3.1.4** Add socket tests
  - Test connection/disconnection
  - Test event broadcast
  - Test multiple clients
  - Test socket cleanup

### Milestone 3.2: Socket Integration & Client

#### Tasks

- [ ] **3.2.1** Wire socket server into daemon startup
  - Start socket listener when daemon starts
  - Graceful shutdown with client notification

- [ ] **3.2.2** Add `firebell listen` subcommand
  - Connect to daemon socket
  - Print events to stdout
  - Optional: `--json` for raw output, default is formatted

- [ ] **3.2.3** Update documentation
  - Add socket section to README.md
  - Include client examples (bash, Python)
  - Document socket protocol

---

## Phase 4: Release

**Priority**: Required
**Estimated Scope**: Small

### Milestone 4.1: Release Preparation

#### Tasks

- [ ] **4.1.1** Update version to 1.1.0
  - Update version constant
  - Update Makefile if needed

- [ ] **4.1.2** Final documentation review
  - Ensure all new features documented in README.md
  - Update CLAUDE.md architecture section
  - Review docs/HOOKS.md for accuracy

- [ ] **4.1.3** Create CHANGELOG entry
  - List all new features
  - Note any breaking changes (none expected)
  - Credit contributors

- [ ] **4.1.4** Final testing
  - Run full test suite
  - Manual testing of all integration methods
  - Test upgrade from v1.0.x

- [ ] **4.1.5** Tag release
  - `git tag v1.1.0`
  - Update install.sh if needed

---

## Implementation Order

```
Phase 1 (Event File)     Phase 2 (Webhooks)      Phase 3 (Socket)       Phase 4
├─ 1.1.3 Event struct    ├─ 2.1.1 Config         ├─ 3.1.1 Config        ├─ 4.1.1 Version
├─ 1.1.1 Config          ├─ 2.1.2 Notifier       ├─ 3.1.2 Server        ├─ 4.1.2 Docs
├─ 1.1.2 Notifier        ├─ 2.1.3 Retry          ├─ 3.1.3 Clients       ├─ 4.1.3 Changelog
├─ 1.1.4 Tests           ├─ 2.1.4 Tests          ├─ 3.1.4 Tests         ├─ 4.1.4 Testing
├─ 1.1.5 Lifecycle       ├─ 2.2.1 Setup          ├─ 3.2.1 Integration   └─ 4.1.5 Tag
├─ 1.2.1 Integration     ├─ 2.2.2 CLI            ├─ 3.2.2 CLI
├─ 1.2.2 CLI             └─ 2.2.3 Docs           └─ 3.2.3 Docs
└─ 1.2.3 Docs
```

---

## Dependencies

- Phase 2 and 3 depend on Phase 1 (Event struct definition)
- Phase 4 depends on all previous phases
- Within each phase, milestones are sequential

---

## Success Criteria

1. All three integration methods functional
2. Full test coverage for new code
3. Documentation complete with examples
4. No regressions in existing functionality
5. Clean upgrade path from v1.0.x
