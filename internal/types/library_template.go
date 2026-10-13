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
	case TemplateExternalTimely:
		return TemplateRules{"按链接与正文识别同一份资料；无链接时按正文及机构、标题、发布日期识别。", "链接必填，或按库配置接受机构名称加发布日期，后者标记来源未核实。", true, true, true, true, true}, true
	case TemplateExternalDurable:
		return TemplateRules{"按链接、标识与正文识别同一份资料。", "链接或标识至少提供一项。", true, true, false, false, true}, true
	case TemplateOwn:
		return TemplateRules{"按可选来源标识与正文识别；同一来源正文变化时新增记录，旧记录保留。", "不需要来源证据。", true, true, false, false, true}, true
	case TemplateGenerated:
		return TemplateRules{"不去重；同一写报告请求的重试按内容指纹幂等。", "不需要来源证据。", false, false, false, false, false}, true
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
		return "mine"
	case "report":
		return "ai-reports"
	default:
		return "institutional"
	}
}

func SystemLibraryDefaults() []*Library {
	institutional := []string{"投资", "收购", "合作", "竞争", "任职", "发布", "供应", "客户", "监管", "隶属"}
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
		makeLibrary("institutional", "机构发布", TemplateExternalTimely, "官方、公司、媒体、研究机构发布的新闻、公告、财报、研报、政策。有时效的机构研报收在这里。", "发布者是机构（官方、公司、媒体、研究机构、行业协会）；有实质信息，不是广告、招聘、纯转发；正文完整。", "link_or_publisher_date", true, []string{"新闻", "公告", "财报", "研究报告", "政策法规", "其他"}, 5, institutional),
		makeLibrary("opinions", "人物观点", TemplateExternalTimely, "外部个人的观点：专家、分析师、KOL 的评论与文章。", "作者是外部个人，必须能确定作者；有明确观点或判断；不是纯转发、无内容的短评。", "link_required", true, []string{"评论", "分析", "预测", "访谈", "其他"}, 4, append([]string{"看好", "看空", "评价", "预测", "批评", "推荐"}, institutional...)),
		makeLibrary("references", "参考文献", TemplateExternalDurable, "长期引用的论文、书籍、标准、官方文档、教程。作为定义和方法长期引用的白皮书收在这里。", "长期有参考价值；正文完整，不是摘要或目录。", "", false, []string{"论文", "书籍", "标准", "官方文档", "教程", "其他"}, 0, []string{"提出", "引用", "属于", "依赖", "对比"}),
		makeLibrary("mine", "我的资料", TemplateOwn, "本人的工作资料、经验、SOP、笔记。", "正文完整、可读；不是结构化记录（任务清单、工单、明细表）。", "", false, []string{"方案", "纪要", "报告", "SOP", "经验", "笔记", "其他"}, 3, []string{"负责", "参与", "合作", "客户", "供应", "依赖", "属于", "决定"}),
		makeLibrary("ai-reports", "AI 报告", TemplateGenerated, "外部 AI 按要求写回的报告；不能当作事实。", "不审核、不处理，只建索引。标题、Markdown 正文必填；关联资料、已存在的实体与主题可选。", "", false, nil, 0, nil),
	}
}

func (l *Library) DisplayName(language string) string {
	en := map[string]string{"institutional": "Institutional", "opinions": "Opinions", "references": "References", "mine": "My documents", "ai-reports": "AI reports"}
	for _, defaults := range SystemLibraryDefaults() {
		if defaults.SystemRole == l.SystemRole && defaults.Name == l.Name && !strings.HasPrefix(strings.ToLower(language), "zh") {
			return en[l.SystemRole]
		}
	}
	return l.Name
}
