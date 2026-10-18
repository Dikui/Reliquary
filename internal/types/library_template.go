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
	SourceEvidenceRule *string                `json:"source_evidence_rule"`
	FeedEnabled        *bool                  `json:"feed_enabled"`
	ContentTypes       *[]LibraryContentType  `json:"content_types"`
	RelationTypes      *[]LibraryRelationType `json:"relation_types"`
	Template           *string                `json:"template"`
	Directory          *string                `json:"directory"`
	SystemRole         *string                `json:"system_role"`
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
		makeLibrary("official", "官方信息", TemplateExternalTimely, "官方、公司、媒体、研究机构发布的新闻、公告、财报、研报、政策。有时效的机构研报收在这里。", "发布者是机构（官方、公司、媒体、研究机构、行业协会）；有实质信息，不是广告、招聘、纯转发；正文完整。", "link_or_publisher_date", true, []string{"新闻", "公告", "财报", "研究报告", "政策法规", "其他"}, 5, official),
		makeLibrary("media", "媒体信息", TemplateExternalTimely, "外部个人的观点：专家、分析师、KOL 的评论与观点资料。", "作者是外部个人，必须能确定作者；有明确观点或判断；不是纯转发、无内容的短评。", "link_required", true, []string{"评论", "分析", "预测", "访谈", "其他"}, 4, append([]string{"看好", "看空", "评价", "预测", "批评", "推荐"}, official...)),
		makeLibrary("references", "参考文档", TemplateExternalDurable, "长期引用的论文、书籍、标准、官方文档、教程。作为定义和方法长期引用的白皮书收在这里。", "长期有参考价值；正文完整，不是摘要或目录。", "", false, []string{"论文", "书籍", "标准", "官方文档", "教程", "其他"}, 0, []string{"提出", "引用", "属于", "依赖", "对比"}),
		makeLibrary("personal", "个人产出", TemplateOwn, "本人的工作资料、经验、SOP、笔记。", "正文完整、可读；不是结构化记录（任务清单、工单、明细表）。", "", false, []string{"方案", "纪要", "报告", "SOP", "经验", "笔记", "其他"}, 3, []string{"负责", "参与", "合作", "客户", "供应", "依赖", "属于", "决定"}),
		makeLibrary("ai-output", "AI产出", TemplateGenerated, "外部 AI 按要求写回的报告；不能当作事实。", "不审核、不处理，只建索引。标题、Markdown 正文必填；来源引用写在正文中。", "", false, nil, 0, nil),
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
