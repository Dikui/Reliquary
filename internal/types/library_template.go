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

// IdentityRules define the independent matching keys available to a template.
type IdentityRules struct {
	URL, Body, NearDuplicate, PublisherDate, Identifier, SourceRef bool
}

// TemplateRules are code-owned behavior, never per-library configuration.
type TemplateRules struct {
	Identity       IdentityRules `json:"-"`
	EvidenceMode   string        `json:"-"`
	Deduplication  string        `json:"deduplication"`
	SourceEvidence string        `json:"source_evidence"`
	Review         bool          `json:"review"`
	Process        bool          `json:"process"`
	MergeReposts   bool          `json:"merge_reposts"`
	FactSource     bool          `json:"fact_source"`
}

func RulesForTemplate(template string) (TemplateRules, bool) {
	switch template {
	case TemplateExternalTimely, TemplateExternalDurable, TemplateOwn, TemplateGenerated:
		return TemplateRules{
				Deduplication:  "仅在本库按来源标识或规范化链接定位；保存内容相同返回原 ID，变化覆盖旧文；无来源标识时新建。",
				SourceEvidence: "来源可选，保留已有依据，不因缺少来源进入待审。",
				Process:        true,
				FactSource:     template != TemplateGenerated,
				Identity: IdentityRules{
					URL:        true,
					Identifier: true,
					SourceRef:  true,
				},
			},
			true
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

// LibraryPatch intentionally includes immutable fields so attempts to write
// them are rejected instead of silently ignored by JSON decoding.
type LibraryPatch struct {
	Name               *string          `json:"name"`
	Description        *string          `json:"description"`
	ReviewCriteria     *string          `json:"review_criteria"`
	ModelTiers         *ModelTierConfig `json:"model_tiers"`
	ChunkingConfig     *ChunkingConfig  `json:"chunking_config"`
	SourceEvidenceRule *string          `json:"source_evidence_rule"`
	FeedEnabled        *bool            `json:"feed_enabled"`
	Template           *string          `json:"template"`
	Directory          *string          `json:"directory"`
	SystemRole         *string          `json:"system_role"`
}

const (
	OfficialReviewCriteria = `本库保存机构发布的事实、变化、安排、研究成果和实践案例。
发布者可识别为公司、研究机构、媒体、行业组织或其他机构；至少包含一项能够明确表述的事实、变化、安排、结果或机构主张。
允许产品与功能发布、价格变化、活动安排、政策调整、人员变动、研究摘要、成果发布页、实践案例。不要求关联论文全文、研究深度、技术细节或完整论证。商业目的、购买链接、报名入口及宣传措辞本身不构成拒收理由。机构主张保留归属，不因来自机构就视为已经核实。
不收录全文只有口号、空泛赞美或导流而无具体信息、以招聘为主要目的、无新增信息的纯转发。
完整性不作为准入条件。正文、图片、视频或关联材料缺失、截断不构成拒收理由；依据已提供的具体信息收录，在 warning 中说明已知缺失与使用限制。未提供的内容不补写，使用时可另行检索。`
	MediaReviewCriteria = `本库保存外部媒体、机构账号或个人的报道、观点、建议、判断、观察和实践经验。
至少包含一项具体事实、观点、建议、判断、观察或实践经验。个人署名不是收录前提；作者为空、未知，或仅有机构、媒体、账号署名，不构成拒收理由。
按原文保留已知个人作者；没有个人署名时保留已知发布机构或账号，不编造个人作者，不把采集工具当作原作者。
允许短帖、单句技术选择、简短实践经验、评论、访谈以及必要的回复和引用上下文。不要求最低篇幅、深入分析、完整论证或观点新颖。
引用和上下文随主文章保存，不要求逐条识别引用者或核查引用归属。主文章须有可辨认的实质内容；材料主要只有引用、转发或回复上下文，主文章没有实质内容时不收录。不设引用比例或最低篇幅。
不收录纯寒暄、表情、无具体内容的互动、无法辨认任何具体信息的碎片、没有新增内容的纯社交转发。
完整性不作为准入条件。依据已有文字或可读图片中的具体信息收录，不因正文、视频、图片或对话上下文缺失而拒绝；在 warning 中说明限制，不猜测缺失信息。`
	ReferenceReviewCriteria = `本库保存以后可以查阅、引用或使用的知识材料。
材料含可辨认的参考信息；不要求本次提交的内容能够独立使用、复现或运行。
允许完整文档、独立章节、论文摘要、文献摘录、操作步骤、代码示例、速查表，以及用途明确、经过组织或带有实质说明的资源索引。不要求每份材料都是完整论文、完整书籍或完整教程。
完整性不作为准入条件。允许正文截断、缺少章节、步骤、参数或代码，以及未提供的外链材料；在 warning 中说明已知缺失、不可直接运行或使用等限制，后续使用时可另行检索。
不收录空白、占位内容、无实质信息的网站导航，以及无法辨认任何参考信息的乱码或碎片。不得补写未提供的步骤、参数、代码或研究结论。`
	PersonalReviewCriteria = `本库保存本人的工作资料、思考、经验和记录。
内容可读，含可辨认的实际信息或记录事项；完整性不作为准入条件。
允许笔记、草稿、方案、经验、SOP、会议记录、检查清单及包含实际信息的任务或工作记录。不要求公开发表质量、外部来源、权威性、最低篇幅或深入分析。未完成草稿可以收录，不因尚无最终结论拒绝。
不收录空白、纯占位、无法辨认任何实际信息的乱码或碎片。缺少正文、步骤、结论或上下文只在 warning 中说明，不据此拒绝；不得仅因采用列表、表格或结构化形式而拒绝。`
	GeneratedReviewCriteria = `不进行收录价值审核，不调用模型处理，只建索引。
标题和 Markdown 正文满足接口要求，保留 AI 产出标识。生成内容中的陈述、推断和引用不因成功保存而视为已经核实。`
)

// DefaultReviewCriteria describes the shared technical admission policy.
func (l *Library) DefaultReviewCriteria() string {
	return "默认接收可读的 Markdown 正文；拒绝空内容、纯链接、仅图片和技术异常。不逐条调用模型评价价值，不因来源、篇幅或完整度不足进入待审。"
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
	return nil
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
	makeLibrary := func(role, name, template, description, evidence string, feed bool) *Library {
		l := &Library{
			SystemRole:         role,
			Directory:          role,
			Name:               name,
			Template:           template,
			Description:        description,
			SourceEvidenceRule: evidence,
			FeedEnabled:        feed,
		}
		l.ReviewCriteria = l.DefaultReviewCriteria()
		l.EnsureDefaults()
		return l
	}
	return []*Library{
		makeLibrary(
			"official",
			"官方信息",
			TemplateExternalTimely,
			"官方、公司、媒体、研究机构发布的新闻、公告、财报、研报、政策。有时效的机构研报收在这里。",
			"link_or_publisher_date",
			true,
		),
		makeLibrary(
			"media",
			"媒体信息",
			TemplateExternalTimely,
			"外部媒体、机构账号或个人的报道、观点、分析与实践经验；不要求个人署名。",
			"link_required",
			true,
		),
		makeLibrary(
			"references",
			"参考文档",
			TemplateExternalDurable,
			"长期引用的论文、书籍、标准、官方文档、教程。作为定义和方法长期引用的白皮书收在这里。",
			"",
			false,
		),
		makeLibrary(
			"personal",
			"个人产出",
			TemplateOwn,
			"本人的工作资料、经验、SOP、笔记。",
			"",
			false,
		),
		makeLibrary(
			"ai-output",
			"AI产出",
			TemplateGenerated,
			"外部 AI 按要求写回的报告；不能当作事实。",
			"",
			false,
		),
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
