package config

import (
	"fmt"
	"github.com/go-viper/mapstructure/v2"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config 应用程序总配置
type Config struct {
	ArticleProcessing *ArticleProcessingConfig `yaml:"article_processing" json:"article_processing"`
	Server            *ServerConfig            `yaml:"server"           json:"server"`
	KnowledgeBase     *KnowledgeBaseConfig     `yaml:"knowledge_base"   json:"knowledge_base"`

	Models          []ModelConfig          `yaml:"models"           json:"models"`
	PromptTemplates *PromptTemplatesConfig `yaml:"prompt_templates" json:"prompt_templates"`
}

// ArticleProcessingConfig controls the retained article summary and question indexing.
type ArticleProcessingConfig struct {
	Summary                   *SummaryConfig `yaml:"summary" json:"summary"`
	GenerateSummaryPromptID   string         `yaml:"generate_summary_prompt_id" json:"generate_summary_prompt_id"`
	GenerateQuestionsPromptID string         `yaml:"generate_questions_prompt_id" json:"generate_questions_prompt_id"`
	GenerateSummaryPrompt     string         `yaml:"-" json:"generate_summary_prompt"`
	GenerateQuestionsPrompt   string         `yaml:"-" json:"generate_questions_prompt"`
}

type SummaryConfig struct {
	MaxInputChars       int `yaml:"max_input_chars" json:"max_input_chars"`
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
	ChunkSize              int                    `yaml:"chunk_size"       json:"chunk_size"`
	ChunkOverlap           int                    `yaml:"chunk_overlap"    json:"chunk_overlap"`
	SplitMarkers           []string               `yaml:"split_markers"    json:"split_markers"`
	KeepSeparator          bool                   `yaml:"keep_separator"   json:"keep_separator"`
	ImageProcessing        *ImageProcessingConfig `yaml:"image_processing" json:"image_processing"`
	DocumentProcessTimeout time.Duration          `yaml:"document_process_timeout"  json:"document_process_timeout"`
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

// ImageProcessingConfig 图像处理配置
type ImageProcessingConfig struct {
	EnableMultimodal bool `yaml:"enable_multimodal" json:"enable_multimodal"`
}

// PromptTemplateI18n holds localized name and description for a prompt template.
type PromptTemplateI18n struct {
	Name        string `yaml:"name"        json:"name"`
	Description string `yaml:"description" json:"description"`
}

// PromptTemplate 提示词模板
//
// 摘要和检索问题生成模板使用 content 提供提示词正文。
//   - content: 主要内容 / 系统 Prompt（所有模板都使用此字段）
//   - i18n:    多语言 name/description，键为 locale（如 "zh-CN"、"en-US"），后端根据请求语言替换 Name/Description 再返回
type PromptTemplate struct {
	ID          string                        `yaml:"id"                 json:"id"`
	Name        string                        `yaml:"name"               json:"name"`
	Description string                        `yaml:"description"        json:"description"`
	Content     string                        `yaml:"content"            json:"content"`
	Default     bool                          `yaml:"default"            json:"default,omitempty"`
	I18n        map[string]PromptTemplateI18n `yaml:"i18n"               json:"-"`
}

// PromptTemplatesConfig 提示词模板配置
//
// 每种 Prompt 类型对应一个 YAML 文件，所有模板都在同一个字段（文件）中管理。
// 每个模板使用 content 提供提示词正文。
type PromptTemplatesConfig struct {
	GenerateSummary   []PromptTemplate `yaml:"generate_summary" json:"generate_summary,omitempty"`
	GenerateQuestions []PromptTemplate `yaml:"generate_questions" json:"generate_questions,omitempty"`
}

// DefaultTemplate returns the first template marked as default in the list,
// or the first template if none is marked, or nil if the list is empty.
func DefaultTemplate(templates []PromptTemplate) *PromptTemplate {
	for i := range templates {
		if templates[i].Default {
			return &templates[i]
		}
	}
	if len(templates) > 0 {
		return &templates[0]
	}
	return nil
}

// LocalizeTemplates returns a deep copy of the template list with Name and
// Description replaced according to the given locale.  Fallback chain:
//
//	locale → primary language (e.g. "zh" from "zh-CN") → original Name/Description.
//
// The returned slice is safe to serialise directly; it never mutates the original.
func LocalizeTemplates(templates []PromptTemplate, locale string) []PromptTemplate {
	if len(templates) == 0 {
		return templates
	}
	out := make([]PromptTemplate, len(templates))
	copy(out, templates)
	for i := range out {
		if len(out[i].I18n) == 0 {
			continue
		}
		// Try exact match first (e.g. "zh-CN"), then primary subtag (e.g. "zh")
		l10n, ok := out[i].I18n[locale]
		if !ok {
			if idx := strings.IndexByte(locale, '-'); idx > 0 {
				l10n, ok = out[i].I18n[locale[:idx]]
			}
		}
		if !ok {
			continue
		}
		if l10n.Name != "" {
			out[i].Name = l10n.Name
		}
		if l10n.Description != "" {
			out[i].Description = l10n.Description
		}
	}
	return out
}

// ModelConfig 模型配置
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

	// 加载提示词模板（从目录或配置文件）
	configDir := filepath.Dir(viper.ConfigFileUsed())
	resolvedConfigDir = configDir
	promptTemplates, err := loadPromptTemplates(configDir)
	if err != nil {
		fmt.Printf("Warning: failed to load prompt templates from directory: %v\n", err)
		// 如果目录加载失败，使用配置文件中的模板（如果有）
	} else if promptTemplates != nil {
		cfg.PromptTemplates = promptTemplates
	}

	// Resolve article processing prompts from configured template IDs
	// (so config.yaml can omit large prompt blocks and rely on template files)
	if cfg.PromptTemplates != nil && cfg.ArticleProcessing != nil {
		backfillArticleProcessingDefaults(&cfg)
	}

	// Validate configuration values
	applyKnowledgeBaseEnvOverrides(&cfg)

	if err := ValidateConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ValidateConfig performs basic validation of the loaded configuration.
// It checks for obviously invalid or missing values that would cause runtime failures.
func ValidateConfig(cfg *Config) error {
	var errs []string

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

// into actual prompt text content. Only xxx_id fields are used;
// no fallback to default templates.
func backfillArticleProcessingDefaults(cfg *Config) {
	if cfg.ArticleProcessing == nil {
		cfg.ArticleProcessing = &ArticleProcessingConfig{}
	}
	processing := cfg.ArticleProcessing
	for id, destination := range map[string]*string{
		processing.GenerateSummaryPromptID:   &processing.GenerateSummaryPrompt,
		processing.GenerateQuestionsPromptID: &processing.GenerateQuestionsPrompt,
	} {
		if id == "" {
			continue
		}
		if template := FindTemplateByID(cfg.PromptTemplates, id); template != nil {
			*destination = template.Content
		} else {
			fmt.Printf("Warning: article processing prompt %q not found\n", id)
		}
	}
}

// FindTemplateByID searches across all template lists for a template with the given ID.
// It returns the template if found, or nil otherwise.
func FindTemplateByID(pt *PromptTemplatesConfig, id string) *PromptTemplate {
	if pt == nil || id == "" {
		return nil
	}
	// Search all template collections
	for _, list := range [][]PromptTemplate{
		pt.GenerateSummary,
		pt.GenerateQuestions,
	} {
		for i := range list {
			if list[i].ID == id {
				return &list[i]
			}
		}
	}
	return nil
}

// promptTemplateFile 用于解析模板文件
type promptTemplateFile struct {
	Templates []PromptTemplate `yaml:"templates"`
}

// loadPromptTemplates 从目录加载提示词模板
func loadPromptTemplates(configDir string) (*PromptTemplatesConfig, error) {
	templatesDir := filepath.Join(configDir, "prompt_templates")

	// 检查目录是否存在
	if _, err := os.Stat(templatesDir); os.IsNotExist(err) {
		return nil, nil // 目录不存在，返回nil让调用者使用配置文件中的模板
	}

	config := &PromptTemplatesConfig{}

	// 定义模板文件映射
	templateFiles := map[string]*[]PromptTemplate{
		"generate_summary.yaml":   &config.GenerateSummary,
		"generate_questions.yaml": &config.GenerateQuestions,
	}

	// 加载每个模板文件
	for filename, target := range templateFiles {
		filePath := filepath.Join(templatesDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			continue // 文件不存在，跳过
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", filename, err)
		}

		var file promptTemplateFile
		if err := yaml.Unmarshal(data, &file); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filename, err)
		}

		*target = file.Templates
	}

	return config, nil
}
