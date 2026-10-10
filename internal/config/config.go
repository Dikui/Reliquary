package config

import (
	"fmt"
	"github.com/go-viper/mapstructure/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 应用程序总配置
type Config struct {
	Vault                  *VaultConfig             `yaml:"vault" json:"vault"`
	ArticleProcessing      *ArticleProcessingConfig `yaml:"article_processing" json:"article_processing"`
	TopicBackfillBatchSize int                      `yaml:"topic_backfill_batch_size" json:"topic_backfill_batch_size"`
	Server                 *ServerConfig            `yaml:"server"           json:"server"`
	KnowledgeBase          *KnowledgeBaseConfig     `yaml:"knowledge_base"   json:"knowledge_base"`

	Models []ModelConfig `yaml:"models"           json:"models"`
}

// ArticleProcessingConfig bounds the single article processing call.
type ArticleProcessingConfig struct {
	MaxBodyChars        int `yaml:"max_body_chars" json:"max_body_chars"`
	MaxImages           int `yaml:"max_images" json:"max_images"`
	MaxCompletionTokens int `yaml:"max_completion_tokens" json:"max_completion_tokens"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port            int           `yaml:"port"             json:"port"`
	Host            string        `yaml:"host"             json:"host"`
	LogPath         string        `yaml:"log_path"         json:"log_path"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout" default:"30s"`
}

// KnowledgeBaseConfig 知识库配置
type KnowledgeBaseConfig struct {
	ChunkSize              int           `yaml:"chunk_size"       json:"chunk_size"`
	ChunkOverlap           int           `yaml:"chunk_overlap"    json:"chunk_overlap"`
	SplitMarkers           []string      `yaml:"split_markers"    json:"split_markers"`
	KeepSeparator          bool          `yaml:"keep_separator"   json:"keep_separator"`
	DocumentProcessTimeout time.Duration `yaml:"document_process_timeout"  json:"document_process_timeout"`
	// ParserCallTimeout caps a single parser call. Without this the
	// gRPC call inherits the asynq task context (whole DocumentProcessTimeout,
	// default 2h+), so a hung parser would block a worker for hours and
	// leave knowledge in "processing". Default 30 minutes is generous enough
	// for OCR-heavy large PDFs while ensuring forward progress.
	ParserCallTimeout time.Duration `yaml:"parser_call_timeout"   json:"parser_call_timeout"`
}

// DefaultDocumentProcessTimeout is the ceiling for a single document:process
// Asynq task when document_process_timeout is unset or non-positive.
const DefaultDocumentProcessTimeout = 2 * time.Hour

// DocumentProcessTimeout returns the effective document-process task timeout.
// Partial configs (e.g. unit tests) receive the default when unset.
func DocumentProcessTimeout(cfg *Config) time.Duration {
	if cfg != nil && cfg.KnowledgeBase != nil && cfg.KnowledgeBase.DocumentProcessTimeout > 0 {
		return cfg.KnowledgeBase.DocumentProcessTimeout
	}
	return DefaultDocumentProcessTimeout
}

type ModelConfig struct {
	Type       string                 `yaml:"type"       json:"type"`
	Source     string                 `yaml:"source"     json:"source"`
	ModelName  string                 `yaml:"model_name" json:"model_name"`
	Parameters map[string]interface{} `yaml:"parameters" json:"parameters"`
}

// resolvedConfigDir holds the directory of the loaded config file. Populated by
// LoadConfig and read by ConfigDir(); empty until LoadConfig has run.
var resolvedConfigDir string

// ConfigDir returns the directory containing the loaded config.yaml. Other
// startup code (e.g. builtin model loader) uses this to locate sibling config
// files like builtin_models.yaml without re-implementing viper search rules.
// Falls back to "./config" when LoadConfig has not been called yet.
func ConfigDir() string {
	if resolvedConfigDir != "" {
		return resolvedConfigDir
	}
	if f := viper.ConfigFileUsed(); f != "" {
		return filepath.Dir(f)
	}
	return "./config"
}

// LoadConfig 从配置文件加载配置
func LoadConfig() (*Config, error) {
	// 设置配置文件名和路径
	viper.SetConfigName("config")         // 配置文件名称(不带扩展名)
	viper.SetConfigType("yaml")           // 配置文件类型
	viper.AddConfigPath(".")              // 当前目录
	viper.AddConfigPath("./config")       // config子目录
	viper.AddConfigPath("$HOME/.appname") // 用户目录
	viper.AddConfigPath("/etc/appname/")  // etc目录

	// 启用环境变量替换
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	// 替换配置中的环境变量引用
	configFileContent, err := os.ReadFile(viper.ConfigFileUsed())
	if err != nil {
		return nil, fmt.Errorf("error reading config file content: %w", err)
	}

	// 替换${ENV_VAR}格式的环境变量引用
	re := regexp.MustCompile(`\${([^}]+)}`)
	result := re.ReplaceAllStringFunc(string(configFileContent), func(match string) string {
		// 提取环境变量名称（去掉${}部分）
		envVar := match[2 : len(match)-1]
		// 获取环境变量值，如果不存在则保持原样
		if value := os.Getenv(envVar); value != "" {
			return value
		}
		return match
	})

	// 使用处理后的配置内容
	viper.ReadConfig(strings.NewReader(result))

	// 解析配置到结构体
	var cfg Config
	if err := viper.Unmarshal(&cfg, func(dc *mapstructure.DecoderConfig) {
		dc.TagName = "yaml"
	}); err != nil {
		return nil, fmt.Errorf("unable to decode config into struct: %w", err)
	}
	fmt.Printf("Using configuration file: %s\n", viper.ConfigFileUsed())

	resolvedConfigDir = filepath.Dir(viper.ConfigFileUsed())

	// Validate configuration values
	applyKnowledgeBaseEnvOverrides(&cfg)
	if raw := os.Getenv("TOPIC_BACKFILL_BATCH_SIZE"); raw != "" {
		if _, err := fmt.Sscan(raw, &cfg.TopicBackfillBatchSize); err != nil {
			return nil, fmt.Errorf("invalid TOPIC_BACKFILL_BATCH_SIZE: %w", err)
		}
	}
	if err := applyVaultEnvOverrides(&cfg); err != nil {
		return nil, err
	}

	if err := ValidateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ValidateConfig performs basic validation of the loaded configuration.
// It checks for obviously invalid or missing values that would cause runtime failures.
func ValidateConfig(cfg *Config) error {
	var errs []string
	if cfg.TopicBackfillBatchSize < 0 || cfg.TopicBackfillBatchSize > 1000 {
		errs = append(errs, "topic_backfill_batch_size must be between 0 and 1000")
	}
	if cfg.ArticleProcessing != nil && (cfg.ArticleProcessing.MaxBodyChars < 0 || cfg.ArticleProcessing.MaxImages < 0 || cfg.ArticleProcessing.MaxCompletionTokens < 0) {
		errs = append(errs, "article_processing limits must be nonnegative")
	}

	if cfg.KnowledgeBase != nil {
		if cfg.KnowledgeBase.ChunkSize <= 0 {
			errs = append(errs, "knowledge_base.chunk_size must be > 0")
		}
		if cfg.KnowledgeBase.ChunkOverlap < 0 {
			errs = append(errs, "knowledge_base.chunk_overlap must be >= 0")
		}
		if cfg.KnowledgeBase.ChunkOverlap >= cfg.KnowledgeBase.ChunkSize {
			errs = append(errs, "knowledge_base.chunk_overlap must be less than chunk_size")
		}
	}

	if cfg.Server != nil {
		if cfg.Server.Port <= 0 || cfg.Server.Port > 65535 {
			errs = append(errs, "server.port must be between 1 and 65535")
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("config validation errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

func applyKnowledgeBaseEnvOverrides(cfg *Config) {
	if cfg.KnowledgeBase == nil {
		cfg.KnowledgeBase = &KnowledgeBaseConfig{}
	}
	if cfg.KnowledgeBase.DocumentProcessTimeout <= 0 {
		cfg.KnowledgeBase.DocumentProcessTimeout = DefaultDocumentProcessTimeout
	}
	if value := strings.TrimSpace(os.Getenv("RELIQUARY_DOCUMENT_PROCESS_TIMEOUT")); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			cfg.KnowledgeBase.DocumentProcessTimeout = d
		}
	}
	if cfg.KnowledgeBase.ParserCallTimeout <= 0 {
		cfg.KnowledgeBase.ParserCallTimeout = 30 * time.Minute
	}
	if value := strings.TrimSpace(os.Getenv("PARSER_CALL_TIMEOUT")); value != "" {
		if d, err := time.ParseDuration(value); err == nil && d > 0 {
			cfg.KnowledgeBase.ParserCallTimeout = d
		}
	}
}
