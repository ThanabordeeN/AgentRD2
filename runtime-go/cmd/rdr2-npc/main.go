// Command rdr2-npc is the standalone Go runtime for the RDR2 Living NPC agent.
//
// It is the counterpart of the Python entry point (runtime/main.py) and is
// built so the runtime can ship as a single executable with no Python
// installation. Both implementations speak the same IPC protocol and read the
// same config/, data/ and scenarios/ files.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/agent"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/backend"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/dotenv"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/ipc"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/lore"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/scenarios"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/state"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/tools"
)

// version is overridden at build time so a released binary reports the tag it
// came from:
//
//	go build -ldflags "-X main.version=1.2.3" ./cmd/rdr2-npc
//
// A plain `go build` reports the development placeholder rather than claiming
// to be a release.
var version = "0.0.0-dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

type options struct {
	host         string
	port         int
	settingsPath string
	backendName  string
	model        string
	apiBase      string
	apiKey       string
	apiKeyEnv    string
	thinkMode    string
	scenariosDir string
	asJSON       bool
	check        bool
	showVersion  bool
	verbose      bool
}

func run(args []string) int {
	// Pick up .env / .env.local / config/local.env before anything reads the
	// environment, mirroring the Python runtime. The applied values are kept so
	// the doctor can say where a key came from.
	envApplied, _ := dotenv.LoadEnv(nil, false)

	opts := options{}
	fs := flag.NewFlagSet("rdr2-npc", flag.ContinueOnError)
	fs.StringVar(&opts.host, "host", "", "IPC bind host (default: settings ipc.host)")
	fs.IntVar(&opts.port, "port", 0, "IPC bind port (default: settings ipc.port)")
	fs.StringVar(&opts.settingsPath, "settings", "", "path to settings.json")
	fs.StringVar(&opts.backendName, "backend", envOr("RDR2AI_BACKEND", "adk"), "model backend: rule or adk")
	fs.StringVar(&opts.model, "model", "", "OpenAI-compatible model id")
	fs.StringVar(&opts.apiBase, "api-base", "", "OpenAI-compatible base URL")
	fs.StringVar(&opts.apiKey, "api-key", "", "API key (otherwise read from .env / environment)")
	fs.StringVar(&opts.apiKeyEnv, "api-key-env", "", "environment variable holding the API key")
	fs.StringVar(&opts.thinkMode, "thinking", "", "thinking mode: low or disabled")
	fs.StringVar(&opts.scenariosDir, "scenarios", "", "run the scenario suite in this directory and exit")
	fs.BoolVar(&opts.asJSON, "json", false, "machine-readable output for --scenarios/--check")
	fs.BoolVar(&opts.check, "check", false, "verify the installation and exit")
	fs.BoolVar(&opts.showVersion, "version", false, "print the version and exit")
	fs.BoolVar(&opts.verbose, "verbose", false, "log every handled message")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if opts.showVersion {
		fmt.Printf("rdr2-npc %s (%s/%s, %s)\n", version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		return 0
	}

	settingsPath := opts.settingsPath
	if settingsPath == "" {
		settingsPath = project.Resolve(filepath.Join("config", "settings.json"))
	}
	settings, err := config.LoadSettings(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot load settings: %v\n", err)
		return 1
	}
	applyEnvOverrides(&settings, &opts)

	if opts.check {
		return runCheck(settings, opts, envApplied)
	}
	if opts.scenariosDir != "" {
		return runScenarios(settings, opts)
	}
	return runRuntime(settings, opts)
}

// applyEnvOverrides implements the documented precedence:
// CLI flag > environment (.env) > settings.json.
func applyEnvOverrides(settings *config.Settings, opts *options) {
	if opts.model == "" {
		opts.model = envOr("RDR2AI_MODEL", settings.ADK.Model)
	}
	if opts.apiBase == "" {
		opts.apiBase = envOr("RDR2AI_API_BASE", settings.ADK.APIBase)
	}
	if opts.apiKeyEnv == "" {
		opts.apiKeyEnv = envOr("RDR2AI_API_KEY_ENV", settings.ADK.APIKeyEnv)
	}
	if opts.apiKeyEnv == "" {
		opts.apiKeyEnv = "OPENCODE_API_KEY"
	}
	if opts.host == "" {
		opts.host = envOr("RDR2AI_HOST", settings.IPC.Host)
	}
	if opts.port == 0 {
		opts.port = settings.IPC.Port
	}
}

// ---------------------------------------------------------------------------
// Runtime mode
// ---------------------------------------------------------------------------

func buildBackend(ctx context.Context, settings config.Settings, opts options) (backend.Backend, error) {
	switch opts.backendName {
	case "rule", "":
		return backend.NewRuleBackend(backend.RuleOptions{
			SilenceProbability: settings.Speech.SilenceProbability,
		}), nil
	case "adk", "llm":
		return backend.NewADKBackend(ctx, backend.ADKOptions{
			Model:     opts.model,
			APIBase:   opts.apiBase,
			APIKey:    opts.apiKey,
			APIKeyEnv: opts.apiKeyEnv,
			// The wiring layer injects the prompt builder, so the backend does
			// not need to import the agent package.
			PromptBuilder: agent.BuildAgentPrompt,
		})
	default:
		return nil, fmt.Errorf("unknown backend %q (expected rule or adk)", opts.backendName)
	}
}

func runRuntime(settings config.Settings, opts options) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	modelBackend, err := buildBackend(ctx, settings, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	runtimeInstance, err := agent.NewRuntime(agent.Options{Settings: &settings, Backend: modelBackend})
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot start runtime: %v\n", err)
		return 1
	}

	server := ipc.NewServer(runtimeInstance, opts.host, opts.port)
	runtimeInstance.Dispatcher().SetSend(func(payload map[string]any) {
		npcID, _ := payload["npc_id"].(string)
		if err := server.Send(npcID, payload); err != nil {
			fmt.Fprintf(os.Stderr, "[ipc] send failed: %v\n", err)
		}
	})

	fmt.Printf("rdr2-npc %s: backend=%s model=%s\n", version, modelBackend.Name(), opts.model)
	if err := server.Serve(ctx); err != nil && ctx.Err() == nil {
		fmt.Fprintf(os.Stderr, "[ipc] server stopped: %v\n", err)
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Scenario parity harness
// ---------------------------------------------------------------------------

func runScenarios(settings config.Settings, opts options) int {
	started := time.Now()
	results, err := scenarios.RunDir(opts.scenariosDir, scenarios.Options{
		Settings: &settings,
		Verbose:  opts.verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot read scenarios: %v\n", err)
		return 1
	}
	if len(results) == 0 {
		fmt.Fprintf(os.Stderr, "no scenarios found in %s\n", opts.scenariosDir)
		return 1
	}

	passed := 0
	for _, result := range results {
		if result.Passed {
			passed++
		}
	}

	if opts.asJSON {
		payload := map[string]any{
			"results":  results,
			"passed":   passed,
			"total":    len(results),
			"duration": time.Since(started).Seconds(),
		}
		encoded, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Println(string(encoded))
	} else {
		for _, result := range results {
			status := "PASS"
			if !result.Passed {
				status = "FAIL"
			}
			detail := ""
			if !result.Passed {
				detail = " -- " + joinFailures(result.Failures)
			}
			fmt.Printf("[%s] %s: %s (%d events)%s\n",
				status, filepath.Base(result.Path), result.Name, len(result.EventNames), detail)
		}
		fmt.Printf("\n%d/%d scenarios passed\n", passed, len(results))
	}

	if passed != len(results) {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Doctor
// ---------------------------------------------------------------------------

type checkResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func runCheck(settings config.Settings, opts options, envApplied map[string]string) int {
	checks := []checkResult{}
	add := func(name, status, detail string) {
		checks = append(checks, checkResult{Name: name, Status: status, Detail: detail})
	}

	add("runtime", "PASS", fmt.Sprintf("go %s on %s/%s (standalone, no Python needed)", runtime.Version(), runtime.GOOS, runtime.GOARCH))
	add("project root", "PASS", project.Root())
	add("settings", "PASS", fmt.Sprintf("model=%s backend=%s", settings.ADK.Model, opts.backendName))

	if _, err := os.Stat(settings.ProfilesDir); err != nil {
		add("profiles", "WARN", "no profile directory; neutral profiles will be used")
	} else {
		add("profiles", "PASS", settings.ProfilesDir)
	}

	loreStore := lore.NewStore(settings.WikiContextPath, settings.CharacterContextPath)
	characters := len(loreStore.AvailableCharacters())
	topics := len(loreStore.AvailableTopics())
	if characters == 0 && topics == 0 {
		add("context packs", "WARN", "no wiki packs found; run scripts/fetch_character_context.py")
	} else {
		add("context packs", "PASS", fmt.Sprintf("%d topics, %d characters", topics, characters))
	}

	if err := os.MkdirAll(settings.TimelinesDir, 0o755); err != nil {
		add("timeline store", "FAIL", fmt.Sprintf("not writable: %v", err))
	} else {
		probe := filepath.Join(settings.TimelinesDir, ".rdr2-npc-probe")
		if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
			add("timeline store", "FAIL", fmt.Sprintf("not writable: %v", err))
		} else {
			_ = os.Remove(probe)
			add("timeline store", "PASS", settings.TimelinesDir)
		}
	}

	registry := tools.BuildDefaultRegistry()
	add("tool catalog", "PASS", fmt.Sprintf("%d agent tools", len(registry.Names())))

	// Use the same strict loader the runtime uses: the doctor must predict
	// whether the runtime will actually start, and eligibility fails closed
	// without a blacklist.
	if _, err := state.LoadBlacklistStrict(settings.StoryBlacklistPath); err != nil {
		add("story blacklist", "FAIL", fmt.Sprintf(
			"%v (extract rdr2-npc-portable-win64.zip, or copy config/ and data/ next to the executable)", err))
	} else {
		add("story blacklist", "PASS", settings.StoryBlacklistPath)
	}

	if backend.ResolveAPIKey(opts.apiKey, opts.apiKeyEnv) == "" {
		add("api key", "WARN",
			"not configured (only needed for --backend adk) — copy .env.example to .env and add OPENCODE_API_KEY")
	} else {
		add("api key", "PASS", describeAPIKeySource(opts, envApplied))
	}

	failed := false
	for _, check := range checks {
		if check.Status == "FAIL" {
			failed = true
		}
	}

	if opts.asJSON {
		encoded, _ := json.MarshalIndent(map[string]any{"checks": checks, "ok": !failed}, "", "  ")
		fmt.Println(string(encoded))
	} else {
		fmt.Printf("rdr2-npc %s doctor\n\n", version)
		for _, check := range checks {
			fmt.Printf("  %-4s %-16s %s\n", check.Status, check.Name, check.Detail)
		}
		fmt.Println()
	}
	if failed {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// describeAPIKeySource names where the credential came from, so "api key: PASS"
// is actionable when someone cannot work out which file is being read.
func describeAPIKeySource(opts options, envApplied map[string]string) string {
	if opts.apiKey != "" {
		return "provided on the command line"
	}
	seen := map[string]bool{}
	for _, name := range []string{opts.apiKeyEnv, "OPENCODE_API_KEY", "OPENAI_API_KEY"} {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if os.Getenv(name) == "" {
			continue
		}
		if _, fromFile := envApplied[name]; fromFile {
			return fmt.Sprintf("from .env (%s)", name)
		}
		return fmt.Sprintf("from the environment (%s)", name)
	}
	return "from local OpenCode credentials (~/.local/share/opencode/auth.json)"
}

func joinFailures(failures []string) string {
	out := ""
	for index, failure := range failures {
		if index > 0 {
			out += "; "
		}
		out += failure
	}
	return out
}
