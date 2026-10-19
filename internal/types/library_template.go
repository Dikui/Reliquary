package types

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	TemplateExternalTimely  = "external_timely"
	TemplateExternalDurable = "external_durable"
	TemplateOwn             = "own"
	TemplateGenerated       = "generated"
)

func (l *Library) ExtractsEntities() bool {
	return l.Template != TemplateOwn && l.Template != TemplateExternalDurable && l.Template != TemplateGenerated
}

// IdentityRules define the independent matching keys available to a template.
type IdentityRules struct {
	URL, Body, NearDuplicate, PublisherDate, Identifier, SourceRef bool
}

// TemplateRules are code-owned behavior, never per-library configuration.
type TemplateRules struct {
	Identity        IdentityRules `json:"-"`
	EvidenceMode    string        `json:"-"`
	Deduplication   string        `json:"deduplication"`
	SourceEvidence  string        `json:"source_evidence"`
	Review          bool          `json:"review"`
	Process         bool          `json:"process"`
	MergeReposts    bool          `json:"merge_reposts"`
	CollapseSimilar bool          `json:"collapse_similar"`
	FactSource      bool          `json:"fact_source"`
}

func RulesForTemplate(template string) (TemplateRules, bool) {
	switch template {
	case TemplateExternalTimely:
		return TemplateRules{Deduplication: "仅在本库按规范化链接、正文哈希或同标题近似正文识别；无链接时还按机构、标题、发布日期识别。", SourceEvidence: "链接必填，或按库配置接受机构名称加发布日期，后者标记来源未核实。", Review: true, Process: true, MergeReposts: true, CollapseSimilar: true, FactSource: true, Identity: IdentityRules{URL: true, Body: true, NearDuplicate: true, PublisherDate: true}, EvidenceMode: "configured"}, true
	case TemplateExternalDurable:
		return TemplateRules{Deduplication: "仅在本库按规范化链接、identifier 或正文哈希识别；不做近似去重、转载归并或相似折叠。", SourceEvidence: "链接或 identifier 至少提供一项。", Review: true, Process: true, FactSource: true, Identity: IdentityRules{URL: true, Body: true, Identifier: true}, EvidenceMode: "link_or_identifier"}, true
	case TemplateOwn:
		return TemplateRules{Deduplication: "仅在本库去重；有 source_ref 时只与同来源最新记录比较正文，相同返回最新记录，变化新增并保留旧记录；无 source_ref 时按正文哈希识别。", SourceEvidence: "不需要来源证据。", Review: true, Process: true, FactSource: true, Identity: IdentityRules{Body: true, SourceRef: true}}, true
	case TemplateGenerated:
		return TemplateRules{Deduplication: "不做内容去重；生成内容按规范化内容指纹保证收录幂等。", SourceEvidence: "不需要来源证据。"}, true
	default:
		return TemplateRules{}, false
	}
}

// SourceEvidenceDecision runs after the caller normalizes the publication date.
// Its first result is a pending-review reason; the second marks unverified provenance.
func (r TemplateRules) SourceEvidenceDecision(configured string, source SourceInfo, identifier string) (string, bool) {
	if r.EvidenceMode == "" {
		return "", false
	}
	if strings.TrimSpace(source.OriginalURL) != "" {
		return "", false
	}
	if r.EvidenceMode == "link_or_identifier" {
		if strings.TrimSpace(identifier) != "" {
			return "", false
		}
		return "缺少原文链接或 identifier", false
	}
	if configured == "link_or_publisher_date" && evidencePublisherPresent(source.Publisher) && strings.TrimSpace(source.PublishedAt) != "" {
		return "", true
	}
	return "缺少原文链接", false
}

func evidencePublisherPresent(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "unknown", "n/a", "null", "none", "未知", "不详", "未提供":
		return false
	}
	return true
}

type LibraryContentType struct {
	LibraryID    string `json:"-" gorm:"primaryKey"`
	Name         string `json:"name" gorm:"primaryKey"`
	Position     int    `json:"position"`
	Enabled      bool   `json:"enabled"`
	ExtractGraph bool   `json:"extract_graph"`
}

type LibraryRelationType struct {
	LibraryID string `json:"-" gorm:"primaryKey"`
	Name      string `json:"name" gorm:"primaryKey"`
	Position  int    `json:"position"`
	Enabled   bool   `json:"enabled"`
}

// LibraryPatch intentionally includes immutable fields so attempts to write
// them are rejected instead of silently ignored by JSON decoding.
type LibraryPatch struct {
	Name               *string                `json:"name"`
	Description        *string                `json:"description"`
	ReviewCriteria     *string                `json:"review_criteria"`
	ModelTiers         *ModelTierConfig       `json:"model_tiers"`
	ChunkingConfig     *ChunkingConfig        `json:"chunking_config"`
	SourceEvidenceRule *string                `json:"source_evidence_rule"`
	FeedEnabled        *bool                  `json:"feed_enabled"`
	ContentTypes       *[]LibraryContentType  `json:"content_types"`
	RelationTypes      *[]LibraryRelationType `json:"relation_types"`
	Template           *string                `json:"template"`
	Directory          *string                `json:"directory"`
	SystemRole         *string                `json:"system_role"`
}

const (
	OfficialReviewCriteria  = "发布者是机构（官方、公司、媒体、研究机构、行业协会）；至少包含一个可明确识别的事实、变化或研究结论。产品发布、调价、活动、人员变动和有过程及结果的客户案例可收录；研究摘要可作为独立发布页收录，不要求包含链接论文全文。排除纯口号、空导流、招聘和无新增信息的转发。完整性以提交的独立发布内容为准：缺失内容影响主要事实理解时待审，否则保留并提示缺失。"
	MediaReviewCriteria     = "作者是外部个人，必须能确定作者；包含具体观点、建议、判断或第一手经验。允许短帖及理解它所需的回复上下文，不设最低篇幅或分析深度。排除纯转发、寒暄、表情和无法理解的碎片；他人的引文不能代替作者自己的贡献。图片或视频缺失时，已有文字能独立表达实质信息的可收录，并提示缺失。"
	ReferenceReviewCriteria = "有长期参考价值；提交的文档主体完整。论文应有主要章节，教程应有必要步骤与代码。排除只有摘要、目录、链接集合或缺少核心内容的文档。按声明的文档单元判断完整性，不要求另附链接资源的全文。"
	PersonalReviewCriteria  = "本人的工作资料、经验、SOP 或笔记；正文可读且能独立理解。不要求外部来源、权威性、最低篇幅或分析深度。排除空白、占位内容和结构化记录（任务清单、工单、明细表）。"
	GeneratedReviewCriteria = "不做收录价值审核，不调用模型处理，只建索引。标题、Markdown 正文必填；来源引用写在正文中，保留 AI 产出标识，不能当作事实。"
)

// DefaultReviewCriteria is a suggestion; saved, user-authored criteria remain authoritative.
func (l *Library) DefaultReviewCriteria() string {
	switch l.SystemRole {
	case "official":
		return OfficialReviewCriteria
	case "media":
		return MediaReviewCriteria
	}
	switch l.Template {
	case TemplateExternalTimely:
		return OfficialReviewCriteria
	case TemplateExternalDurable:
		return ReferenceReviewCriteria
	case TemplateOwn:
		return PersonalReviewCriteria
	case TemplateGenerated:
		return GeneratedReviewCriteria
	}
	return ""
}

func (l *Library) ProcessingModelTiers() []ModelTier {
	rules, _ := RulesForTemplate(l.Template)
	tiers := []ModelTier{}
	if rules.Review {
		tiers = append(tiers, ModelTierLight)
	}
	if rules.Process {
		tiers = append(tiers, ModelTierGeneration)
	}
	if l.ExtractsEntities() {
		tiers = append(tiers, ModelTierEntityResolution)
	}
	return tiers
}

type CreateLibraryRequest struct {
	Template    string `json:"template"`
	Directory   string `json:"directory"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var libraryDirectoryPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func ValidateLibrary(l *Library) error {
	if _, ok := RulesForTemplate(l.Template); !ok {
		return fmt.Errorf("invalid library template")
	}
	if len(l.Directory) > 100 || !libraryDirectoryPattern.MatchString(l.Directory) {
		return fmt.Errorf("directory must contain lowercase letters, digits and single hyphens")
	}
	if strings.TrimSpace(l.Name) == "" || len([]rune(l.Name)) > 255 {
		return fmt.Errorf("library name is required and must fit 255 characters")
	}
	if l.Template == TemplateExternalTimely {
		if l.SourceEvidenceRule != "link_required" && l.SourceEvidenceRule != "link_or_publisher_date" {
			return fmt.Errorf("invalid source evidence rule")
		}
	} else if l.SourceEvidenceRule != "" {
		return fmt.Errorf("source evidence rule is only configurable for external_timely")
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
	makeLibrary := func(role, name, template, description, criteria, evidence string, feed bool, names []string, graphCount int, relations []string) *Library {
		l := &Library{SystemRole: role, Directory: role, Name: name, Template: template, Description: description, ReviewCriteria: criteria, SourceEvidenceRule: evidence, FeedEnabled: feed, ContentTypes: []LibraryContentType{}, RelationTypes: []LibraryRelationType{}}
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
		makeLibrary("official", "官方信息", TemplateExternalTimely, "官方、公司、媒体、研究机构发布的新闻、公告、财报、研报、政策。有时效的机构研报收在这里。", OfficialReviewCriteria, "link_or_publisher_date", true, []string{"新闻", "公告", "财报", "研究报告", "政策法规", "其他"}, 5, official),
		makeLibrary("media", "媒体信息", TemplateExternalTimely, "外部个人的观点：专家、分析师、KOL 的评论与观点资料。", MediaReviewCriteria, "link_required", true, []string{"评论", "分析", "预测", "访谈", "其他"}, 4, append([]string{"看好", "看空", "评价", "预测", "批评", "推荐"}, official...)),
		makeLibrary("references", "参考文档", TemplateExternalDurable, "长期引用的论文、书籍、标准、官方文档、教程。作为定义和方法长期引用的白皮书收在这里。", ReferenceReviewCriteria, "", false, []string{"论文", "书籍", "标准", "官方文档", "教程", "其他"}, 0, []string{"提出", "引用", "属于", "依赖", "对比"}),
		makeLibrary("personal", "个人产出", TemplateOwn, "本人的工作资料、经验、SOP、笔记。", PersonalReviewCriteria, "", false, []string{"方案", "纪要", "报告", "SOP", "经验", "笔记", "其他"}, 0, []string{"负责", "参与", "合作", "客户", "供应", "依赖", "属于", "决定"}),
		makeLibrary("ai-output", "AI产出", TemplateGenerated, "外部 AI 按要求写回的报告；不能当作事实。", GeneratedReviewCriteria, "", false, nil, 0, nil),
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
