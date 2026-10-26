package maintenance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// These immutable suffixes reproduce migration 47, not the editable runtime templates.
const libraryPromptArticleSuffix = `
输出所有字段，格式：{"title":"中文资料标题",
"summary":{"overview":"中文一句话概述"},"domain_ids":[],
"entity_mentions":[{"name":"原文中的机构或人物名称","type":"organization 或 person","entity_id":"匹配的已有实体 ID，未匹配留空","quote":"包含该名称的一句逐字原文"}]}。
domain_ids 只能选 domain_options 中的 ID。领域和实体各选 0–3 个，没有依据时返回空数组。
实体先按名称和别名匹配 entity_options，匹配不到仍输出原文名称，供人工确认新实体，不得硬选已有实体。
quote 必须逐字来自正文并包含 name，不带段落编号。产品和模型本身不作为实体，不根据模型品牌推测机构名称。
segment 是本段在整篇资料中的位置。只分析本段，context_before 仅作衔接上下文。
仅当 fact_allowed=true 时允许输出事实。否则 fact 必须为 null。
仅当本篇报道一次具体发生时，额外输出一个 fact 对象：{"title":"事实标题","subject":"主体","action":"本次动作","object":"对象","conditions":"条件",
"occurred_at":"RFC3339 或 YYYY-MM-DD，未知用空字符串","quote":"一句逐字原文",
"subject_entity_id":"已有实体 ID 或空字符串","object_entity_id":"已有实体 ID 或空字符串",
"subject_type":"organization / person / other","object_type":"organization / person / other"}。
端点 ID 只能来自 entity_options；没有 ID 时标明类型，产品和模型用 other。署名作者不作为事实端点提取。
综合稿、评论、教程不输出事实，fact 为 null。提取本次动作而非背景动作，一篇资料最多一个事实。
quote 必须逐字出自正文，不含段落编号，不翻译、不改写、不拼接。
图片附件与输入 images 的顺序一一对应。仅依据已提供的可读图片理解文章，不猜测缺失或不可读图片的内容。`
const libraryPromptSummarySuffix = `
只输出 JSON {"title":"中文资料标题",
"summary":{"overview":"中文一句话概述"},"domain_ids":[],"entity_mentions":[]}。
领域与实体各选 0–3 个。领域只能从各段已选 ID 中选择。
entity_mentions 只能从各段提供的实体中选择，完整保留 name、type、entity_id、quote，不得改写名称或逐字引文。匹配不到已有实体的新名称也必须保留，供人工确认。
若 fact_allowed=true，从各段 fact 中选择整篇报道的本次发生，最多输出一个 fact。
fact 包含 title、subject、action、object、conditions、occurred_at、quote，
以及 subject_entity_id、object_entity_id、subject_type、object_type。
保留所选事实的端点 ID 与类型，ID 只能来自 entity_options，无 ID 时类型为 organization、person 或 other。
quote 必须完整照抄所选 fact 的引文。综合稿、评论、教程或没有合适事实时输出 fact:null。
若 fact_allowed=false，必须输出 fact:null。`
const libraryPromptEvaluationPrefix = `Generate exactly the number of questions specified by count: `
const libraryPromptEvaluationSuffix = ` Return only a JSON array of objects {"query":"question","answers":[{"entry_id":"supplied ID","grade":1}]}. Use only supplied document IDs. Every question requires at least one positive answer.`

func projectLibraryPromptsSnapshot(snapshot *Snapshot, targetVersion int64) {
	if snapshot.schemaVersion >= 47 || targetVersion < 47 {
		return
	}
	rows := snapshot.Tables["system_settings"]
	existing := make(map[string]bool, len(rows))
	var maximum int64
	for _, row := range rows {
		existing[stringValue(row["key"])] = true
		id, _ := strconv.ParseInt(fmt.Sprint(row["id"]), 10, 64)
		maximum = max(maximum, id)
	}
	additions := []map[string]any{}
	add := func(source map[string]any, key, value string) {
		if existing[key] {
			return
		}
		existing[key] = true
		additions = append(additions, map[string]any{
			"key": key, "value": value, "value_type": "string", "category": "prompts",
			"description": source["description"], "is_secret": false, "requires_restart": false,
			"last_modified_by": source["last_modified_by"],
			"created_at":       source["created_at"], "updated_at": source["updated_at"],
		})
	}
	for _, source := range rows {
		content, valid := source["value"].(string)
		if !valid {
			continue
		}
		id, prefix, suffix := "", "", ""
		switch source["key"] {
		case "prompt.article_processing":
			id, suffix = "article_processing", libraryPromptArticleSuffix
		case "prompt.summary_merge":
			id, suffix = "summary_merge", libraryPromptSummarySuffix
		case "prompt.evaluation_questions":
			id, prefix, suffix = "evaluation_questions", libraryPromptEvaluationPrefix, libraryPromptEvaluationSuffix
			add(source, "prompt.evaluation_questions_multi_library", prefix+content+suffix)
		default:
			continue
		}
		for _, library := range snapshot.Tables["libraries"] {
			if library["deleted_at"] == nil {
				add(source, "prompt.library."+stringValue(library["id"])+"."+id, prefix+content+suffix)
			}
		}
	}
	sort.Slice(additions, func(i, j int) bool { return additions[i]["key"].(string) < additions[j]["key"].(string) })
	for _, row := range additions {
		maximum++
		row["id"] = json.Number(strconv.FormatInt(maximum, 10))
	}
	snapshot.Tables["system_settings"] = append(append([]map[string]any{}, rows...), additions...)
	sortRows(snapshot.Tables["system_settings"])
}

func projectLibraryPromptsManifest(expected, actual *Manifest) (*Manifest, *Manifest) {
	if expected.SchemaVersion >= 47 || actual.SchemaVersion < 47 {
		return expected, actual
	}
	item, exists := expected.Tables["system_settings"]
	if !exists || item.Data == nil || item.Rows != int64(len(item.Data)) {
		return expected, actual
	}
	before, _ := json.Marshal(canonicalRows("system_settings", item.Data))
	if item.SHA256 != sum(before) {
		return expected, actual
	}
	snapshot := &Snapshot{schemaVersion: expected.SchemaVersion, Tables: map[string][]map[string]any{
		"system_settings": item.Data, "libraries": expected.Tables["libraries"].Data,
	}}
	projectLibraryPromptsSnapshot(snapshot, actual.SchemaVersion)
	item.Data = snapshot.Tables["system_settings"]
	item.Rows = int64(len(item.Data))
	data, _ := json.Marshal(canonicalRows("system_settings", item.Data))
	item.SHA256 = sum(data)
	left := *expected
	left.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, entry := range expected.Tables {
		left.Tables[table] = entry
	}
	left.Tables["system_settings"] = item
	return &left, actual
}
