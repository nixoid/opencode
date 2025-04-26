package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/opencode-ai/opencode/internal/llm/models"
	"github.com/opencode-ai/opencode/internal/logging"
	"github.com/spf13/viper"
)

type MCPType string

const (
	MCPStdio MCPType = "stdio"
	MCPSse   MCPType = "sse"
)

type MCPServer struct {
	Command string            `json:"command"`
	Env     []string          `json:"env"`
	Args    []string          `json:"args"`
	Type    MCPType           `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type AgentName string

const (
	AgentCoder AgentName = "coder"
	AgentTask  AgentName = "task"
	AgentTitle AgentName = "title"
)

type Agent struct {
	Model           models.ModelID `json:"model"`
	MaxTokens       int64          `json:"maxTokens"`
	ReasoningEffort string         `json:"reasoningEffort"`
}

type Provider struct {
	APIKey   string `json:"apiKey"`
	Disabled bool   `json:"disabled"`
}

type Data struct {
	Directory string `json:"directory"`
}

type LSPConfig struct {
	Disabled bool     `json:"enabled"`
	Command  string   `json:"command"`
	Args     []string `json:"args"`
	Options  any      `json:"options"`
}

type Config struct {
	Data       Data                              `json:"data"`
	WorkingDir string                            `json:"wd,omitempty"`
	MCPServers map[string]MCPServer              `json:"mcpServers,omitempty"`
	Providers  map[models.ModelProvider]Provider `json:"providers,omitempty"`
	LSP        map[string]LSPConfig              `json:"lsp,omitempty"`
	Agents     map[AgentName]Agent               `json:"agents"`
	Debug      bool                              `json:"debug,omitempty"`
	DebugLSP   bool                              `json:"debugLSP,omitempty"`
}

const (
	defaultDataDirectory = ".opencode"
	defaultLogLevel      = "info"
	appName              = "opencode"
)

var cfg *Config

func Load(workingDir string, debug bool) (*Config, error) {
	if cfg != nil {
		return cfg, nil
	}

	cfg = &Config{
		WorkingDir: workingDir,
		MCPServers: make(map[string]MCPServer),
		Providers:  make(map[models.ModelProvider]Provider),
		LSP:        make(map[string]LSPConfig),
	}

	configureViper()
	setDefaults(debug)
	setProviderDefaults()

	if err := readConfig(viper.ReadInConfig()); err != nil {
		return cfg, err
	}

	mergeLocalConfig(workingDir)

	if err := viper.Unmarshal(cfg); err != nil {
		return cfg, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	applyDefaultValues()
	defaultLevel := slog.LevelInfo
	if cfg.Debug {
		defaultLevel = slog.LevelDebug
	}
	if os.Getenv("OPENCODE_DEV_DEBUG") == "true" {
		loggingFile := fmt.Sprintf("%s/%s", cfg.Data.Directory, "debug.log")

		if _, err := os.Stat(loggingFile); os.IsNotExist(err) {
			if err := os.MkdirAll(cfg.Data.Directory, 0o755); err != nil {
				return cfg, fmt.Errorf("failed to create directory: %w", err)
			}
			if _, err := os.Create(loggingFile); err != nil {
				return cfg, fmt.Errorf("failed to create log file: %w", err)
			}
		}

		sloggingFileWriter, err := os.OpenFile(loggingFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			return cfg, fmt.Errorf("failed to open log file: %w", err)
		}
		logger := slog.New(slog.NewTextHandler(sloggingFileWriter, &slog.HandlerOptions{
			Level: defaultLevel,
		}))
		slog.SetDefault(logger)
	} else {
		logger := slog.New(slog.NewTextHandler(logging.NewWriter(), &slog.HandlerOptions{
			Level: defaultLevel,
		}))
		slog.SetDefault(logger)
	}

	if err := Validate(); err != nil {
		return cfg, fmt.Errorf("config validation failed: %w", err)
	}

	if cfg.Agents == nil {
		cfg.Agents = make(map[AgentName]Agent)
	}

	cfg.Agents[AgentTitle] = Agent{
		Model:     cfg.Agents[AgentTitle].Model,
		MaxTokens: 80,
	}

	initializeCurrentProvider()

	return cfg, nil
}

func configureViper() {
	viper.SetConfigName(fmt.Sprintf(".%s", appName))
	viper.SetConfigType("json")
	viper.AddConfigPath("$HOME")
	viper.AddConfigPath(fmt.Sprintf("$XDG_CONFIG_HOME/%s", appName))
	viper.AddConfigPath(fmt.Sprintf("$HOME/.config/%s", appName))
	viper.SetEnvPrefix(strings.ToUpper(appName))
	viper.AutomaticEnv()
}

func setDefaults(debug bool) {
	viper.SetDefault("data.directory", defaultDataDirectory)

	if debug {
		viper.SetDefault("debug", true)
		viper.Set("log.level", "debug")
	} else {
		viper.SetDefault("debug", false)
		viper.SetDefault("log.level", defaultLogLevel)
	}
}

func setProviderDefaults() {
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.anthropic.apiKey", apiKey)
		viper.SetDefault("agents.coder.model", models.Claude37Sonnet)
		viper.SetDefault("agents.task.model", models.Claude37Sonnet)
		viper.SetDefault("agents.title.model", models.Claude37Sonnet)
		return
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.openai.apiKey", apiKey)
		viper.SetDefault("agents.coder.model", models.GPT41)
		viper.SetDefault("agents.task.model", models.GPT41Mini)
		viper.SetDefault("agents.title.model", models.GPT41Mini)
		return
	}

	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.gemini.apiKey", apiKey)
		viper.SetDefault("agents.coder.model", models.Gemini25)
		viper.SetDefault("agents.task.model", models.Gemini25Flash)
		viper.SetDefault("agents.title.model", models.Gemini25Flash)
		return
	}

	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		viper.SetDefault("providers.groq.apiKey", apiKey)
		viper.SetDefault("agents.coder.model", models.QWENQwq)
		viper.SetDefault("agents.task.model", models.QWENQwq)
		viper.SetDefault("agents.title.model", models.QWENQwq)
		return
	}

	if hasAWSCredentials() {
		viper.SetDefault("agents.coder.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.task.model", models.BedrockClaude37Sonnet)
		viper.SetDefault("agents.title.model", models.BedrockClaude37Sonnet)
		return
	}
}

func hasAWSCredentials() bool {
	if os.Getenv("AWS_ACCESS_KEY_ID") != "" && os.Getenv("AWS_SECRET_ACCESS_KEY") != "" {
		return true
	}

	if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_DEFAULT_PROFILE") != "" {
		return true
	}

	if os.Getenv("AWS_REGION") != "" || os.Getenv("AWS_DEFAULT_REGION") != "" {
		return true
	}

	if os.Getenv("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI") != "" ||
		os.Getenv("AWS_CONTAINER_CREDENTIALS_FULL_URI") != "" {
		return true
	}

	return false
}

func readConfig(err error) error {
	if err == nil {
		return nil
	}

	if _, ok := err.(viper.ConfigFileNotFoundError); ok {
		return nil
	}

	return fmt.Errorf("failed to read config: %w", err)
}

func mergeLocalConfig(workingDir string) {
	local := viper.New()
	local.SetConfigName(fmt.Sprintf(".%s", appName))
	local.SetConfigType("json")
	local.AddConfigPath(workingDir)

	if err := local.ReadInConfig(); err == nil {
		viper.MergeConfigMap(local.AllSettings())
	}
}

func applyDefaultValues() {
	for k, v := range cfg.MCPServers {
		if v.Type == "" {
			v.Type = MCPStdio
			cfg.MCPServers[k] = v
		}
	}
}

func Validate() error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	for name, agent := range cfg.Agents {
		model, modelExists := models.SupportedModels[agent.Model]
		if !modelExists {
			logging.Warn("unsupported model configured, reverting to default",
				"agent", name,
				"configured_model", agent.Model)

			if setDefaultModelForAgent(name) {
				logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
			} else {
				return fmt.Errorf("no valid provider available for agent %s", name)
			}
			continue
		}

		provider := model.Provider
		providerCfg, providerExists := cfg.Providers[provider]

		if !providerExists {
			apiKey := getProviderAPIKey(provider)
			if apiKey == "" {
				logging.Warn("provider not configured for model, reverting to default",
					"agent", name,
					"model", agent.Model,
					"provider", provider)

				if setDefaultModelForAgent(name) {
					logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
				} else {
					return fmt.Errorf("no valid provider available for agent %s", name)
				}
			} else {
				cfg.Providers[provider] = Provider{
					APIKey: apiKey,
				}
				logging.Info("added provider from environment", "provider", provider)
			}
		} else if providerCfg.Disabled || providerCfg.APIKey == "" {
			logging.Warn("provider is disabled or has no API key, reverting to default",
				"agent", name,
				"model", agent.Model,
				"provider", provider)

			if setDefaultModelForAgent(name) {
				logging.Info("set default model for agent", "agent", name, "model", cfg.Agents[name].Model)
			} else {
				return fmt.Errorf("no valid provider available for agent %s", name)
			}
		}

		if agent.MaxTokens <= 0 {
			logging.Warn("invalid max tokens, setting to default",
				"agent", name,
				"model", agent.Model,
				"max_tokens", agent.MaxTokens)

			updatedAgent := cfg.Agents[name]
			if model.DefaultMaxTokens > 0 {
				updatedAgent.MaxTokens = model.DefaultMaxTokens
			} else {
				updatedAgent.MaxTokens = 4096
			}
			cfg.Agents[name] = updatedAgent
		} else if model.ContextWindow > 0 && agent.MaxTokens > model.ContextWindow/2 {
			logging.Warn("max tokens exceeds half the context window, adjusting",
				"agent", name,
				"model", agent.Model,
				"max_tokens", agent.MaxTokens,
				"context_window", model.ContextWindow)

			updatedAgent := cfg.Agents[name]
			updatedAgent.MaxTokens = model.ContextWindow / 2
			cfg.Agents[name] = updatedAgent
		}

		if model.CanReason && provider == models.ProviderOpenAI {
			if agent.ReasoningEffort == "" {
				logging.Info("setting default reasoning effort for model that supports reasoning",
					"agent", name,
					"model", agent.Model)

				updatedAgent := cfg.Agents[name]
				updatedAgent.ReasoningEffort = "medium"
				cfg.Agents[name] = updatedAgent
			} else {
				effort := strings.ToLower(agent.ReasoningEffort)
				if effort != "low" && effort != "medium" && effort != "high" {
					logging.Warn("invalid reasoning effort, setting to medium",
						"agent", name,
						"model", agent.Model,
						"reasoning_effort", agent.ReasoningEffort)

					updatedAgent := cfg.Agents[name]
					updatedAgent.ReasoningEffort = "medium"
					cfg.Agents[name] = updatedAgent
				}
			}
		} else if !model.CanReason && agent.ReasoningEffort != "" {
			logging.Warn("model doesn't support reasoning but reasoning effort is set, ignoring",
				"agent", name,
				"model", agent.Model,
				"reasoning_effort", agent.ReasoningEffort)

			updatedAgent := cfg.Agents[name]
			updatedAgent.ReasoningEffort = ""
			cfg.Agents[name] = updatedAgent
		}
	}

	for provider, providerCfg := range cfg.Providers {
		if providerCfg.APIKey == "" && !providerCfg.Disabled {
			logging.Warn("provider has no API key, marking as disabled", "provider", provider)
			providerCfg.Disabled = true
			cfg.Providers[provider] = providerCfg
		}
	}

	for language, lspConfig := range cfg.LSP {
		if lspConfig.Command == "" && !lspConfig.Disabled {
			logging.Warn("LSP configuration has no command, marking as disabled", "language", language)
			lspConfig.Disabled = true
			cfg.LSP[language] = lspConfig
		}
	}

	return nil
}

func getProviderAPIKey(provider models.ModelProvider) string {
	switch provider {
	case models.ProviderAnthropic:
		return os.Getenv("ANTHROPIC_API_KEY")
	case models.ProviderOpenAI:
		return os.Getenv("OPENAI_API_KEY")
	case models.ProviderGemini:
		return os.Getenv("GEMINI_API_KEY")
	case models.ProviderGROQ:
		return os.Getenv("GROQ_API_KEY")
	case models.ProviderBedrock:
		if hasAWSCredentials() {
			return "aws-credentials-available"
		}
	}
	return ""
}

func setDefaultModelForAgent(agent AgentName) bool {
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}
		cfg.Agents[agent] = Agent{
			Model:     models.Claude37Sonnet,
			MaxTokens: maxTokens,
		}
		return true
	}

	if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)
		reasoningEffort := ""

		switch agent {
		case AgentTitle:
			model = models.GPT41Mini
			maxTokens = 80
		case AgentTask:
			model = models.GPT41Mini
		default:
			model = models.GPT41
		}

		if modelInfo, ok := models.SupportedModels[model]; ok && modelInfo.CanReason {
			reasoningEffort = "medium"
		}

		cfg.Agents[agent] = Agent{
			Model:           model,
			MaxTokens:       maxTokens,
			ReasoningEffort: reasoningEffort,
		}
		return true
	}

	if apiKey := os.Getenv("GEMINI_API_KEY"); apiKey != "" {
		var model models.ModelID
		maxTokens := int64(5000)

		if agent == AgentTitle {
			model = models.Gemini25Flash
			maxTokens = 80
		} else {
			model = models.Gemini25
		}

		cfg.Agents[agent] = Agent{
			Model:     model,
			MaxTokens: maxTokens,
		}
		return true
	}

	if apiKey := os.Getenv("GROQ_API_KEY"); apiKey != "" {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}

		cfg.Agents[agent] = Agent{
			Model:     models.QWENQwq,
			MaxTokens: maxTokens,
		}
		return true
	}

	if hasAWSCredentials() {
		maxTokens := int64(5000)
		if agent == AgentTitle {
			maxTokens = 80
		}

		cfg.Agents[agent] = Agent{
			Model:           models.BedrockClaude37Sonnet,
			MaxTokens:       maxTokens,
			ReasoningEffort: "medium",
		}
		return true
	}

	return false
}

func Get() *Config {
	return cfg
}

func WorkingDirectory() string {
	if cfg == nil {
		panic("config not loaded")
	}
	return cfg.WorkingDir
}

func SwitchProvider(newProvider string) error {
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	var newModel models.ModelID
	switch newProvider {
	case "openai":
		newModel = models.GPT41
	case "anthropic":
		newModel = models.Claude37Sonnet
	case "gemini":
		newModel = models.Gemini25
	case "bedrock":
		newModel = models.BedrockClaude37Sonnet
	case "groq":
		newModel = models.QWENQwq
	default:
		return fmt.Errorf("unsupported provider: %s", newProvider)
	}

	cfg.Agents[AgentCoder] = Agent{
		Model:     newModel,
		MaxTokens: models.SupportedModels[newModel].DefaultMaxTokens,
	}

	return nil
}

func initializeCurrentProvider() {
	currentProvider := cfg.Agents[AgentCoder].Model.Provider
	switch currentProvider {
	case models.ProviderOpenAI:
		if cfg.Providers[models.ProviderOpenAI].APIKey == "" {
			cfg.Providers[models.ProviderOpenAI] = Provider{
				APIKey: os.Getenv("OPENAI_API_KEY"),
			}
		}
	case models.ProviderAnthropic:
		if cfg.Providers[models.ProviderAnthropic].APIKey == "" {
			cfg.Providers[models.ProviderAnthropic] = Provider{
				APIKey: os.Getenv("ANTHROPIC_API_KEY"),
			}
		}
	case models.ProviderGemini:
		if cfg.Providers[models.ProviderGemini].APIKey == "" {
			cfg.Providers[models.ProviderGemini] = Provider{
				APIKey: os.Getenv("GEMINI_API_KEY"),
			}
		}
	case models.ProviderBedrock:
		if cfg.Providers[models.ProviderBedrock].APIKey == "" {
			cfg.Providers[models.ProviderBedrock] = Provider{
				APIKey: "aws-credentials-available",
			}
		}
	case models.ProviderGROQ:
		if cfg.Providers[models.ProviderGROQ].APIKey == "" {
			cfg.Providers[models.ProviderGROQ] = Provider{
				APIKey: os.Getenv("GROQ_API_KEY"),
			}
		}
	}
}
