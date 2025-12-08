package config

import (
	"flag"
	"fmt"
	"os"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Flags holds parsed command-line flags.
type Flags struct {
	ConfigPath string
	Setup      bool
	Check      bool
	Agent      string
	Stdout     bool
	Version    bool
	Migrate    bool
	Wrap       bool     // Wrap a command
	WrapArgs   []string // Command and arguments to wrap
	WrapName   string   // Display name for wrapped command

	// Daemon subcommands
	DaemonStart   bool // Start daemon
	DaemonStop    bool // Stop daemon
	DaemonRestart bool // Restart daemon
	DaemonStatus  bool // Show daemon status
	DaemonLogs    bool // Show/tail logs
	DaemonFollow  bool // Follow log output (-f)
}

// ParseFlags parses command-line flags and returns the result.
// Returns nil if help was requested or setup/check commands should run.
func ParseFlags() *Flags {
	flags := &Flags{}

	// Check for subcommands first
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "wrap":
			return parseWrapFlags(flags)
		case "start":
			return parseDaemonFlags(flags, "start")
		case "stop":
			return parseDaemonFlags(flags, "stop")
		case "restart":
			return parseDaemonFlags(flags, "restart")
		case "status":
			return parseDaemonFlags(flags, "status")
		case "logs":
			return parseDaemonFlags(flags, "logs")
		}
	}

	flag.StringVar(&flags.ConfigPath, "config", "", "Config file path (default: ~/.firebell/config.yaml)")
	flag.BoolVar(&flags.Setup, "setup", false, "Run interactive configuration wizard")
	flag.BoolVar(&flags.Check, "check", false, "Run health check and exit")
	flag.StringVar(&flags.Agent, "agent", "", "Filter to specific agent (codex|copilot|claude|gemini|opencode)")
	flag.BoolVar(&flags.Stdout, "stdout", false, "Output to stdout instead of Slack (for testing)")
	flag.BoolVar(&flags.Version, "version", false, "Print version and exit")
	flag.BoolVar(&flags.Migrate, "migrate", false, "Migrate v1 config to v2 YAML format")

	flag.Usage = customUsage
	flag.Parse()

	return flags
}

// parseWrapFlags parses flags for the wrap subcommand.
func parseWrapFlags(flags *Flags) *Flags {
	flags.Wrap = true

	// Create a new flagset for wrap subcommand
	wrapFlags := flag.NewFlagSet("wrap", flag.ExitOnError)
	wrapFlags.StringVar(&flags.ConfigPath, "config", "", "Config file path")
	wrapFlags.StringVar(&flags.WrapName, "name", "", "Display name for the wrapped command")
	wrapFlags.BoolVar(&flags.Stdout, "stdout", false, "Output notifications to stdout")

	wrapFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, `firebell wrap - Run a command with ai-chime monitoring

USAGE:
  firebell wrap [flags] -- <command> [args...]

FLAGS:
  --config PATH    Config file (default: ~/.firebell/config.yaml)
  --name NAME      Display name for notifications (default: command name)
  --stdout         Output notifications to stdout instead of Slack

EXAMPLES:
  # Wrap Claude Code
  firebell wrap -- claude

  # Wrap with custom name
  firebell wrap --name "My Claude" -- claude --debug

  # Wrap with stdout notifications
  firebell wrap --stdout -- codex

  # Wrap any command
  firebell wrap --name "GPT Script" -- python my_gpt_script.py

`)
	}

	// Find the "--" separator
	dashIdx := -1
	for i, arg := range os.Args[2:] {
		if arg == "--" {
			dashIdx = i + 2
			break
		}
	}

	if dashIdx == -1 {
		// No "--" found, try to parse all args after "wrap"
		wrapFlags.Parse(os.Args[2:])
		flags.WrapArgs = wrapFlags.Args()
	} else {
		// Parse flags before "--"
		wrapFlags.Parse(os.Args[2:dashIdx])
		// Everything after "--" is the command
		flags.WrapArgs = os.Args[dashIdx+1:]
	}

	// Default name to command name
	if flags.WrapName == "" && len(flags.WrapArgs) > 0 {
		flags.WrapName = flags.WrapArgs[0]
	}

	return flags
}

// parseDaemonFlags parses flags for daemon subcommands.
func parseDaemonFlags(flags *Flags, cmd string) *Flags {
	switch cmd {
	case "start":
		flags.DaemonStart = true
	case "stop":
		flags.DaemonStop = true
	case "restart":
		flags.DaemonRestart = true
	case "status":
		flags.DaemonStatus = true
	case "logs":
		flags.DaemonLogs = true
	}

	daemonFlags := flag.NewFlagSet(cmd, flag.ExitOnError)
	daemonFlags.StringVar(&flags.ConfigPath, "config", "", "Config file path")

	if cmd == "logs" {
		daemonFlags.BoolVar(&flags.DaemonFollow, "f", false, "Follow log output")
	}

	if cmd == "start" || cmd == "restart" {
		daemonFlags.StringVar(&flags.Agent, "agent", "", "Filter to specific agent")
	}

	daemonFlags.Usage = func() {
		switch cmd {
		case "start":
			fmt.Fprintf(os.Stderr, `firebell start - Start the daemon

USAGE:
  firebell start [flags]

FLAGS:
  --config PATH    Config file (default: ~/.firebell/config.yaml)
  --agent NAME     Filter to specific agent

EXAMPLES:
  firebell start
  firebell start --agent claude

`)
		case "stop":
			fmt.Fprintf(os.Stderr, `firebell stop - Stop the running daemon

USAGE:
  firebell stop

`)
		case "restart":
			fmt.Fprintf(os.Stderr, `firebell restart - Restart the daemon

USAGE:
  firebell restart [flags]

FLAGS:
  --config PATH    Config file (default: ~/.firebell/config.yaml)
  --agent NAME     Filter to specific agent

`)
		case "status":
			fmt.Fprintf(os.Stderr, `firebell status - Show daemon status

USAGE:
  firebell status

`)
		case "logs":
			fmt.Fprintf(os.Stderr, `firebell logs - View daemon logs

USAGE:
  firebell logs [flags]

FLAGS:
  -f               Follow log output (like tail -f)

EXAMPLES:
  firebell logs
  firebell logs -f

`)
		}
	}

	daemonFlags.Parse(os.Args[2:])
	return flags
}

// customUsage provides user-friendly help text for v2.0.
func customUsage() {
	fmt.Fprintf(os.Stderr, `firebell v2.0 - Real-time AI CLI activity monitor

USAGE:
  firebell [flags]                              Run in foreground (stdout mode)
  firebell start                                Start daemon in background
  firebell stop                                 Stop running daemon
  firebell restart                              Restart daemon
  firebell status                               Show daemon status
  firebell logs [-f]                            View daemon logs
  firebell wrap [flags] -- <command> [args...]  Wrap a command

GETTING STARTED:
  firebell --setup     Run interactive configuration wizard
  firebell --check     Verify log paths and show status
  firebell --stdout    Test without Slack (prints to terminal)

DAEMON COMMANDS:
  start               Start monitoring daemon in background
  stop                Stop running daemon
  restart             Restart daemon
  status              Show daemon status (running/stopped, PID, uptime)
  logs                View daemon log file (use -f to follow)

OTHER COMMANDS:
  wrap                Wrap a command and monitor its output

FLAGS:
  --config PATH       Config file (default: ~/.firebell/config.yaml)
  --setup             Interactive configuration wizard
  --check             Health check and exit
  --agent NAME        Filter to specific agent: codex, copilot, claude, gemini, opencode
  --stdout            Output to stdout instead of Slack (for testing)
  --version           Print version and exit
  --migrate           Migrate v1 config to v2 YAML format

EXAMPLES:
  # First-time setup
  firebell --setup

  # Run in foreground (default)
  firebell --stdout

  # Start daemon in background
  firebell start
  firebell start --agent claude

  # Check daemon status
  firebell status

  # View logs
  firebell logs
  firebell logs -f

  # Stop daemon
  firebell stop

  # Wrap a command (real-time output monitoring)
  firebell wrap -- claude
  firebell wrap --name "My AI" -- python ai_script.py

CONFIGURATION:
  Config file: ~/.firebell/config.yaml
  Edit this file to customize monitoring behavior, output verbosity, and advanced settings.

  To reconfigure, run: firebell --setup

MORE INFO:
  Documentation: https://github.com/yourusername/firebell
  Report issues: https://github.com/yourusername/firebell/issues

`)
}
