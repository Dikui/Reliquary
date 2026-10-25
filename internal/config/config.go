package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"

	"github.com/spf13/viper"
)

// Config 应用程序总配置
type Config struct {
	FactGrouping      *FactGroupingConfig      `yaml:"fact_grouping" json:"fact_grouping"`
	IngestDiagnostics *IngestDiagnosticsConfig `yaml:"ingest_diagnostics" json:"ingest_diagnostics"`
	Vault             *VaultConfig             `yaml:"vault" json:"vault"`
	ArticleProcessing *ArticleProcessingConfig `yaml:"article_processing" json:"article_processing"`
	Server            *ServerConfig            `yaml:"server"           json:"server"`
	Library           *LibraryConfig           `yaml:"library"   json:"library"`

	Models []ModelConfig `yaml:"models"           json:"models"`
}

// FactGroupingConfig controls candidate recall and high-confidence verification.
type FactGroupingConfig struct {
	RecallThreshold float64 `yaml:"recall_threshold" json:"recall_threshold"`
	ReviewThreshold float64 `yaml:"review_threshold" json:"review_threshold"`
}

// GroupingThresholds supplies defaults for omitted or zero configuration.
func (cfg *Config) GroupingThresholds() (float64, float64) {
	recall, review := 0.6, 0.85
	if cfg != nil && cfg.FactGrouping != nil {
		if cfg.FactGrouping.RecallThreshold != 0 {
			recall = cfg.FactGrouping.RecallThreshold
		}
		if cfg.FactGrouping.ReviewThreshold != 0 {
			review = cfg.FactGrouping.ReviewThreshold
		}
	}
	return recall, review
}

// ArticleProcessingConfig bounds review, processing and segmented input.
type ArticleProcessingConfig struct {
	MaxBodyChars          int `yaml:"max_body_chars" json:"max_body_chars"`
	MaxImages             int `yaml:"max_images" json:"max_images"`
	MaxCompletionTokens   int `yaml:"max_completion_tokens" json:"max_completion_tokens"`
	MaxSegments           int `yaml:"max_segments" json:"max_segments"`
	SegmentOverlapChars   int `yaml:"segment_overlap_chars" json:"segment_overlap_chars"`
	ContextReservePercent int `yaml:"context_reserve_percent" json:"context_reserve_percent"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port            int           `yaml:"port"             json:"port"`
	Host            string        `yaml:"host"             json:"host"`
	LogPath         string        `yaml:"log_path"         json:"log_path"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout" default:"30s"`
}

// LibraryConfig 库配置
type LibraryConfig struct {
	ChunkSize              int           `yaml:"chunk_size"       json:"chunk_size"`
	ChunkOverlap           int           `yaml:"chunk_overlap"    json:"chunk_overlap"`
	SplitMarkers           []string      `yaml:"split_markers"    json:"split_markers"`
	KeepSeparator          bool          `yaml:"keep_separator"   json:"keep_separator"`
	DocumentProcessTimeout time.Duration `yaml:"document_process_timeout"  json:"document_process_timeout"`
}

// DefaultDocumentProcessTimeout is the ceiling for a single document:process
// queue task when document_process_timeout is unset or non-positive.
const DefaultDocumentProcessTimeout = 2 * time.Hour

// DocumentProcessTimeout returns the effective document-process task timeout.
// Partial configs (e.g. unit tests) receive the default when unset.
func DocumentProcessTimeout(cfg *Config) time.Duration {
	if cfg != nil && cfg.Library != nil && cfg.Library.DocumentProcessTimeout > 0 {
		return cfg.Library.DocumentProcessTimeout
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
	fmt.Fprintf(os.Stderr, "Using configuration file: %s\n", viper.ConfigFileUsed())

	resolvedConfigDir = filepath.Dir(viper.ConfigFileUsed())

	// Validate configuration values
	applyLibraryEnvOverrides(&cfg)
	if err := applyVaultEnvOverrides(&cfg); err != nil {
		return nil, err
	}

	if err := ValidateConfig(&cfg); err != nil {
		return nil, err
	}

	if err := LoadPromptTemplates(ConfigDir()); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "Using prompt templates: %s\n", filepath.Join(ConfigDir(), "prompt_templates"))

	return &cfg, nil
}

// ValidateConfig performs basic validation of the loaded configuration.
// It checks for obviously invalid or missing values that would cause runtime failures.
func ValidateConfig(cfg *Config) error {
	var errs []string
	recall, review := cfg.GroupingThresholds()
	if !(recall > 0 && recall <= 1 && review > 0 && review <= 1) {
		errs = append(errs, "fact_grouping thresholds must be in (0,1]")
	}
	if cfg.ArticleProcessing != nil && (cfg.ArticleProcessing.MaxBodyChars < 0 || cfg.ArticleProcessing.MaxImages < 0 || cfg.ArticleProcessing.MaxCompletionTokens < 0 || cfg.ArticleProcessing.MaxSegments < 0 || cfg.ArticleProcessing.SegmentOverlapChars < 0 || cfg.ArticleProcessing.ContextReservePercent < 0 || cfg.ArticleProcessing.ContextReservePercent > 90) {
		errs = append(errs, "article_processing limits must be nonnegative")
	}

	if cfg.Library != nil {
		if cfg.Library.ChunkSize <= 0 {
			errs = append(errs, "library.chunk_size must be > 0")
		}
		if cfg.Library.ChunkOverlap < 0 {
			errs = append(errs, "library.chunk_overlap must be >= 0")
		}
		if cfg.Library.ChunkOverlap >= cfg.Library.ChunkSize {
			errs = append(errs, "library.chunk_overlap must be less than chunk_size")
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

func applyLibraryEnvOverrides(cfg *Config) {
	if cfg.Library == nil {
		cfg.Library = &LibraryConfig{}
	}
	if cfg.Library.DocumentProcessTimeout <= 0 {
		cfg.Library.DocumentProcessTimeout = DefaultDocumentProcessTimeout
	}
	if value := strings.TrimSpace(os.Getenv("RELIQUARY_DOCUMENT_PROCESS_TIMEOUT")); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			cfg.Library.DocumentProcessTimeout = d
		}
	}
}
