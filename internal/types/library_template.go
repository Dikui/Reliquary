package types

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	TemplateStandard = "standard"
	// Legacy values are retained for historical snapshot readers and internal constructors.
	TemplateExternalTimely  = "external_timely"
	TemplateExternalDurable = "external_durable"
	TemplateOwn             = "own"
	TemplateGenerated       = "generated"
)

func (l *Library) ExtractsEntities() bool {
	return l != nil
}

// TemplateRules are code-owned behavior, never per-library configuration.
type TemplateRules struct {
	Deduplication   string `json:"deduplication"`
	SourceEvidence  string `json:"source_evidence"`
	Review          bool   `json:"review"`
	Process         bool   `json:"process"`
	MergeReposts    bool   `json:"merge_reposts"`
	CollapseSimilar bool   `json:"collapse_similar"`
	FactSource      bool   `json:"fact_source"`
}

func RulesForTemplate(template string) (TemplateRules, bool) {
	switch template {
	case TemplateStandard, TemplateExternalTimely, TemplateExternalDurable, TemplateOwn, TemplateGenerated:
		return TemplateRules{Deduplication: "仅在本库按来源标识或规范化链接定位；保存内容相同返回原 ID，变化覆盖旧文；无来源标识时新建。", SourceEvidence: "来源可选，保留已有依据，不因缺少来源进入待审。", Process: true, CollapseSimilar: true, FactSource: true}, true
	default:
		return TemplateRules{}, false
	}
}

type LibraryContentType struct {
	LibraryID    string `json:"-" gorm:"primaryKey"`
	Name         string `json:"name" gorm:"primaryKey"`
	Position     int    `json:"position"`
	Enabled      bool   `json:"enabled"`
	ExtractGraph bool   `json:"extract_graph"`
}

type LibraryRelationType struct {
	Fields       []EntityFieldDefinition `json:"fields" gorm:"serializer:json;type:jsonb;not null;default:'[]'"`
	Hierarchical bool                    `json:"hierarchical" gorm:"not null;default:false"`
	SubjectKinds []string                `json:"subject_kinds" gorm:"serializer:json;type:jsonb;not null;default:'[]'"`
	ObjectKinds  []string                `json:"object_kinds" gorm:"serializer:json;type:jsonb;not null;default:'[]'"`
	LibraryID    string                  `json:"-" gorm:"primaryKey"`
	Name         string                  `json:"name" gorm:"primaryKey"`
	Position     int                     `json:"position"`
	Enabled      bool                    `json:"enabled"`
}

// LibraryPatch intentionally includes immutable fields so attempts to write
// them are rejected instead of silently ignored by JSON decoding.
type LibraryPatch struct {
	ProductLibraryIDs     *[]string              `json:"product_library_ids"`
	DocumentRelationTypes *[]string              `json:"document_relation_types"`
	EntityKindMapping     map[string]string      `json:"entity_kind_mapping"`
	EntityMode            *string                `json:"entity_mode"`
	EntitySchema          *EntitySchema          `json:"entity_schema"`
	PromptOverrides       *map[string]string     `json:"prompt_overrides"`
	Color                 *int                   `json:"color"`
	Name                  *string                `json:"name"`
	Description           *string                `json:"description"`
	ReviewCriteria        *string                `json:"review_criteria"`
	ModelTiers            *ModelTierConfig       `json:"model_tiers"`
	ChunkingConfig        *ChunkingConfig        `json:"chunking_config"`
	SourceEvidenceRule    *string                `json:"source_evidence_rule"`
	FeedEnabled           *bool                  `json:"feed_enabled"`
	ContentTypes          *[]LibraryContentType  `json:"content_types"`
	RelationTypes         *[]LibraryRelationType `json:"relation_types"`
	Template              *string                `json:"template"`
	Directory             *string                `json:"directory"`
	SystemRole            *string                `json:"system_role"`
}

const MaxLibraryPromptChars = 16000

var LibraryPromptIDs = [...]string{"article_processing", "library_summary", "article_entities", "article_relations", "summary_merge", "topic_backfill"}

func ValidateLibraryPromptOverrides(prompts map[string]string) error {
	for id, content := range prompts {
		known := false
		for _, allowed := range LibraryPromptIDs {
			known = known || id == allowed
		}
		if !known {
			return fmt.Errorf("unknown library prompt: %s", id)
		}
		if strings.TrimSpace(content) == "" || len([]rune(content)) > MaxLibraryPromptChars {
			return fmt.Errorf("library prompt %s must contain 1 to %d characters of nonblank content", id, MaxLibraryPromptChars)
		}
	}
	return nil
}

// DefaultReviewCriteria describes the shared technical admission policy.
func (l *Library) DefaultReviewCriteria() string {
	return "默认接收可读的 Markdown 正文；拒绝空内容、纯链接、仅图片和技术异常。不逐条调用模型评价价值，不因来源、篇幅或完整度不足进入待审。"
}

func (l *Library) ProcessingModelTiers() []ModelTier {
	rules, _ := RulesForTemplate(l.Template)
	tiers := []ModelTier{}
	if rules.Process {
		tiers = append(tiers, ModelTierGeneration)
	}
	if l.ExtractsEntities() {
		tiers = append(tiers, ModelTierEntityResolution)
	}
	return tiers
}

type CreateLibraryRequest struct {
	ProductLibraryIDs     []string     `json:"product_library_ids"`
	DocumentRelationTypes []string     `json:"document_relation_types"`
	EntityMode            string       `json:"entity_mode"`
	EntitySchema          EntitySchema `json:"entity_schema"`
	Color                 *int         `json:"color"`
	Template              string       `json:"template"`
	Directory             string       `json:"directory"`
	Name                  string       `json:"name"`
	Description           string       `json:"description"`
}

var libraryDirectoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidateLibraryColor checks that a color belongs to the library palette.
func ValidateLibraryColor(color int) error {
	if color < 1 || color > 8 {
		return fmt.Errorf("library color must be an integer between 1 and 8")
	}
	return nil
}

func ValidateLibrary(l *Library) error {
	if err := ValidateEntitySchema(l); err != nil {
		return err
	}
	if l.Template != TemplateStandard {
		return fmt.Errorf("invalid library template")
	}
	if len(l.Directory) > 100 || !libraryDirectoryPattern.MatchString(l.Directory) {
		return fmt.Errorf("directory must contain lowercase letters, digits and single hyphens")
	}
	if strings.TrimSpace(l.Name) == "" || len([]rune(l.Name)) > 255 {
		return fmt.Errorf("library name is required and must fit 255 characters")
	}

	for _, names := range [][]string{contentTypeNames(l.ContentTypes), relationTypeNames(l.RelationTypes)} {
		seen := map[string]bool{}
		for _, name := range names {
			if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 || seen[name] {
				return fmt.Errorf("type names must be nonempty, unique and fit 100 characters")
			}
			seen[name] = true
		}
	}
	return nil
}
func contentTypeNames(rows []LibraryContentType) []string {
	names := []string{}
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}
func relationTypeNames(rows []LibraryRelationType) []string {
	names := []string{}
	for _, row := range rows {
		names = append(names, row.Name)
	}
	return names
}

func SystemRoleForSource(kind string) string {
	switch kind {
	case "mine":
		return "personal"
	case "report":
		return "ai-output"
	default:
		return "official"
	}
}

func SystemLibraryDefaults() []*Library {
	official := []string{"投资", "收购", "合作", "竞争", "任职", "发布", "供应", "客户", "监管", "隶属"}
	makeLibrary := func(role, name, description string, feed bool, names []string, graphCount int, relations []string) *Library {
		l := &Library{SystemRole: role, Directory: role, Name: name, Template: TemplateStandard, Description: description, FeedEnabled: feed, ContentTypes: []LibraryContentType{}, RelationTypes: []LibraryRelationType{}}
		l.ReviewCriteria = l.DefaultReviewCriteria()
		for i, n := range names {
			l.ContentTypes = append(l.ContentTypes, LibraryContentType{Name: n, Position: i, Enabled: true, ExtractGraph: i < graphCount})
		}
		for i, n := range relations {
			l.RelationTypes = append(l.RelationTypes, LibraryRelationType{Name: n, Position: i, Enabled: true})
		}
		l.EnsureDefaults()
		return l
	}
	return []*Library{
		makeLibrary("official", "官方信息", "官方、公司、媒体、研究机构发布的新闻、公告、财报、研报、政策。有时效的机构研报收在这里。", true, []string{"新闻", "公告", "财报", "研究报告", "政策法规", "其他"}, 5, official),
		makeLibrary("media", "媒体信息", "外部媒体、机构账号或个人的报道、观点、分析与实践经验；不要求个人署名。", true, []string{"评论", "分析", "预测", "访谈", "其他"}, 4, append([]string{"看好", "看空", "评价", "预测", "批评", "推荐"}, official...)),
		makeLibrary("references", "参考文档", "长期引用的论文、书籍、标准、官方文档、教程。作为定义和方法长期引用的白皮书收在这里。", false, []string{"论文", "书籍", "标准", "官方文档", "教程", "其他"}, 0, []string{"提出", "引用", "属于", "依赖", "对比"}),
		makeLibrary("personal", "个人产出", "本人的工作资料、经验、SOP、笔记。", false, []string{"方案", "纪要", "报告", "SOP", "经验", "笔记", "其他"}, 0, []string{"负责", "参与", "合作", "客户", "供应", "依赖", "属于", "决定"}),
		makeLibrary("ai-output", "AI产出", "外部 AI 按要求写回的报告；不能当作事实。", false, nil, 0, nil),
	}
}

func (l *Library) DisplayName(language string) string {
	en := map[string]string{"official": "Official information", "media": "Media information", "references": "Reference documents", "personal": "Personal output", "ai-output": "AI output"}
	for _, defaults := range SystemLibraryDefaults() {
		if defaults.SystemRole == l.SystemRole && defaults.Name == l.Name && !strings.HasPrefix(strings.ToLower(language), "zh") {
			return en[l.SystemRole]
		}
	}
	return l.Name
}
