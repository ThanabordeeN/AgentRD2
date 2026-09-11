package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go/v3/option"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/genai"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// openCodeAuthPath mirrors OPENCODE_AUTH_PATH in runtime/agent/openai_compat.py.
var openCodeAuthPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}()

// ADKOptions configures the Google ADK backend.
type ADKOptions struct {
	// Model is the OpenAI-compatible model id, e.g. "deepseek-v4.1-flash".
	Model string
	// APIBase is the OpenAI-compatible base URL, e.g.
	// "https://opencode.ai/zen/go/v1".
	APIBase string
	// APIKey wins over every environment variable when set.
	APIKey string
	// APIKeyEnv names the environment variable holding the key.
	APIKeyEnv string
	// ExtraHeaders are sent with every request (for example a session header
	// required by a gateway).
	ExtraHeaders map[string]string
	// Timeout bounds a single model call. Zero means 120s.
	Timeout time.Duration
	// MaxTokens bounds the completion. Zero means 4096.
	MaxTokens int
	// Temperature defaults to 0.3, matching the Python backend.
	Temperature float64
	// PromptBuilder renders the agent prompt. The wiring layer injects
	// agent.BuildAgentPrompt here; the backend does not import that package so
	// the two stay decoupled.
	PromptBuilder func(*domain.AgentContext) string
}

// ADKBackend asks a model for the next NPC decision through Google's Agent
// Development Kit for Go (google.golang.org/adk/v2).
//
// It is the Go counterpart of GoogleADKBackend in runtime/agent/backends.py,
// and speaks the OpenAI Responses API through ADK's openaimodel adapter.
type ADKBackend struct {
	opts      ADKOptions
	llm       model.LLM
	sessionID string

	mu        sync.Mutex
	callCount int
}

// NewADKBackend constructs the backend and resolves credentials.
func NewADKBackend(ctx context.Context, opts ADKOptions) (*ADKBackend, error) {
	if opts.Model == "" {
		return nil, fmt.Errorf("adk backend: model is required")
	}
	if opts.APIBase == "" {
		opts.APIBase = "https://opencode.ai/zen/go/v1"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 120 * time.Second
	}
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 4096
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.3
	}

	key := ResolveAPIKey(opts.APIKey, opts.APIKeyEnv)
	if key == "" {
		return nil, fmt.Errorf(
			"adk backend: no API key found (set %s / OPENCODE_API_KEY / OPENAI_API_KEY, "+
				"pass --api-key, or log in with OpenCode)",
			firstNonEmpty(opts.APIKeyEnv, "OPENCODE_API_KEY"),
		)
	}

	clientCfg := &openaimodel.ClientConfig{
		APIKey:  key,
		BaseURL: opts.APIBase,
	}

	requestOptions := []option.RequestOption{
		// Several gateways route on a session header; the Python runtime sends
		// one on every request, so keep the same behaviour.
		option.WithHeader("x-opencode-session", newSessionID()),
		option.WithRequestTimeout(opts.Timeout),
	}
	for name, value := range opts.ExtraHeaders {
		requestOptions = append(requestOptions, option.WithHeader(name, value))
	}
	clientCfg.Options = requestOptions

	llm, err := openaimodel.NewModel(ctx, opts.Model, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("adk backend: cannot construct model: %w", err)
	}

	return &ADKBackend{opts: opts, llm: llm, sessionID: newSessionID()}, nil
}

// Name identifies the backend in logs.
func (b *ADKBackend) Name() string { return "adk" }

// SupportsWaitGestures is true: a model call has real latency, so the runtime
// should play a short thinking gesture while it waits.
func (b *ADKBackend) SupportsWaitGestures() bool { return true }

// Calls reports how many model calls this backend has made.
func (b *ADKBackend) Calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.callCount
}

// Decide renders the prompt, calls the model, and parses the structured
// decision. A reasoning-only response is retried with more output room, then
// once more with a stricter instruction, mirroring the Python backend.
func (b *ADKBackend) Decide(ctx context.Context, agentCtx *domain.AgentContext) (*domain.AgentDecision, error) {
	if b.opts.PromptBuilder == nil {
		return nil, fmt.Errorf("adk backend: no prompt builder configured")
	}
	prompt := b.opts.PromptBuilder(agentCtx)

	maxTokens := b.opts.MaxTokens
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		contents := []*genai.Content{genai.NewContentFromText(prompt, genai.RoleUser)}
		if attempt > 0 {
			contents = append(contents, genai.NewContentFromText(
				"Return only the final JSON decision object now. The assistant message must contain the JSON object and nothing else.",
				genai.RoleUser,
			))
		}

		temperature := float32(b.opts.Temperature)
		request := &model.LLMRequest{
			Model:    b.opts.Model,
			Contents: contents,
			Config: &genai.GenerateContentConfig{
				MaxOutputTokens: int32(maxTokens),
				Temperature:     &temperature,
			},
		}

		text, err := b.generate(ctx, request)
		if err != nil {
			return nil, err
		}
		decision, err := ParseDecision(text)
		if err == nil {
			return decision, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, err
		}
		maxTokens = max(maxTokens*2, 8192)
	}
	return nil, lastErr
}

// generate runs one non-streaming completion and concatenates its text parts.
func (b *ADKBackend) generate(ctx context.Context, request *model.LLMRequest) (string, error) {
	b.mu.Lock()
	b.callCount++
	b.mu.Unlock()

	var builder strings.Builder
	stream := b.llm.GenerateContent(ctx, request, false)
	if err := consume(stream, func(resp *model.LLMResponse) {
		if resp == nil || resp.Content == nil {
			return
		}
		for _, part := range resp.Content.Parts {
			if part != nil && part.Text != "" {
				builder.WriteString(part.Text)
			}
		}
	}); err != nil {
		return "", fmt.Errorf("adk backend: model call failed: %w", err)
	}
	return builder.String(), nil
}

// consume drains an ADK response iterator.
func consume(stream iter.Seq2[*model.LLMResponse, error], visit func(*model.LLMResponse)) error {
	for response, err := range stream {
		if err != nil {
			return err
		}
		visit(response)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Decision parsing (parity with OpenAICompatibleBackend._parse_decision)
// ---------------------------------------------------------------------------

var (
	jsonFenceOpen  = regexp.MustCompile("^```(?:json)?\\s*")
	jsonFenceClose = regexp.MustCompile("\\s*```$")
	jsonObjectRe   = regexp.MustCompile(`(?s)\{.*\}`)
)

// ErrNoDecisionContent marks a response that held no usable assistant content,
// which is worth retrying.
type ErrNoDecisionContent struct{ Detail string }

func (e ErrNoDecisionContent) Error() string {
	return "adk backend: model response contained no final message content" + e.Detail
}

func isRetryable(err error) bool {
	_, ok := err.(ErrNoDecisionContent)
	return ok
}

// ParseDecision extracts an AgentDecision from raw model text.
//
// It mirrors the Python parser: markdown fences are stripped, a JSON object is
// located inside surrounding prose as a fallback, double-encoded payloads are
// unwrapped, and per-tool argument defaults are applied.
func ParseDecision(text string) (*domain.AgentDecision, error) {
	candidate := strings.TrimSpace(text)
	if candidate == "" {
		return nil, ErrNoDecisionContent{}
	}
	if strings.HasPrefix(candidate, "```") {
		candidate = jsonFenceOpen.ReplaceAllString(candidate, "")
		candidate = jsonFenceClose.ReplaceAllString(candidate, "")
	}

	var payload any
	if err := json.Unmarshal([]byte(candidate), &payload); err != nil {
		match := jsonObjectRe.FindString(candidate)
		if match == "" {
			return nil, ErrNoDecisionContent{Detail: fmt.Sprintf(" (unparseable: %.120q)", truncate(text, 300))}
		}
		if err := json.Unmarshal([]byte(match), &payload); err != nil {
			return nil, fmt.Errorf("adk backend: model returned malformed JSON: %.300q", text)
		}
	}

	// A model occasionally double-encodes the object as a JSON string.
	if asString, ok := payload.(string); ok {
		var inner any
		if err := json.Unmarshal([]byte(asString), &inner); err != nil {
			return nil, fmt.Errorf("adk backend: model returned double-encoded non-JSON: %.300q", text)
		}
		payload = inner
	}

	object, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("adk backend: model JSON decision was not an object: %.300q", text)
	}

	internal := map[string]any{}
	if raw, ok := object["internal"].(map[string]any); ok {
		internal = raw
	}

	decision := domain.NewAgentDecision()
	if goal, ok := internal["goal"].(string); ok && goal != "" {
		decision.Goal = &goal
	}
	if mood, ok := internal["mood"].(string); ok && mood != "" {
		decision.Mood = &mood
	}
	decision.Internal = internal

	switch speech := object["speech"].(type) {
	case map[string]any:
		if text, ok := speech["text"].(string); ok && text != "" {
			entry := &domain.AgentSpeech{Text: text}
			if target, ok := speech["target"].(string); ok {
				entry.Target = target
			}
			if emotion, ok := speech["emotion"].(string); ok {
				entry.Emotion = emotion
			}
			decision.Speech = entry
		}
	case string:
		if speech != "" {
			decision.Speech = &domain.AgentSpeech{Text: speech, Target: "player"}
		}
	}

	rawActions, _ := object["actions"].([]any)
	for _, rawAction := range rawActions {
		action, ok := rawAction.(map[string]any)
		if !ok {
			continue
		}
		tool, _ := action["tool"].(string)
		if tool == "" {
			continue
		}
		arguments := map[string]any{}
		if given, ok := action["arguments"].(map[string]any); ok {
			arguments = given
		}
		for key, value := range targetDefaults[tool] {
			if _, exists := arguments[key]; !exists {
				arguments[key] = value
			}
		}
		decision.Actions = append(decision.Actions, domain.AgentAction{Tool: tool, Arguments: arguments})
	}
	return decision, nil
}

// targetDefaults mirrors the Python parser's entity defaults.
var targetDefaults = map[string]map[string]any{
	"look_at":   {"entity": "player"},
	"face":      {"entity": "player"},
	"follow":    {"entity": "player"},
	"flee_from": {"entity": "player"},
}

// ---------------------------------------------------------------------------
// Credentials
// ---------------------------------------------------------------------------

// ResolveAPIKey mirrors resolve_api_key in runtime/agent/openai_compat.py:
// explicit value, then the configured env var, then the well-known ones, then
// the local OpenCode credentials file.
func ResolveAPIKey(apiKey, apiKeyEnv string) string {
	if apiKey != "" {
		return apiKey
	}
	candidates := []string{apiKeyEnv, "OPENCODE_API_KEY", "OPENAI_API_KEY"}
	seen := map[string]bool{}
	for _, name := range candidates {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return apiKeyFromOpenCodeAuth()
}

func apiKeyFromOpenCodeAuth() string {
	if openCodeAuthPath == "" {
		return ""
	}
	raw, err := os.ReadFile(openCodeAuthPath)
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	entry, ok := payload["opencode-go"].(map[string]any)
	if !ok {
		return ""
	}
	key, _ := entry["key"].(string)
	return key
}

func newSessionID() string { return "ses_" + domain.RandomHex(32) }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
