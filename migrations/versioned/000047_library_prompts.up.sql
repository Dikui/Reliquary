BEGIN;
-- Stable IDs and original timestamps let replica verification reproduce every added row.
WITH templates(id, prefix, suffix) AS (
 VALUES
 ('article_processing', '', E'\n' || $prompt$输出所有字段，格式：{"title":"中文资料标题",
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
图片附件与输入 images 的顺序一一对应。仅依据已提供的可读图片理解文章，不猜测缺失或不可读图片的内容。$prompt$),
 ('summary_merge', '', E'\n' || $prompt$只输出 JSON {"title":"中文资料标题",
"summary":{"overview":"中文一句话概述"},"domain_ids":[],"entity_mentions":[]}。
领域与实体各选 0–3 个。领域只能从各段已选 ID 中选择。
entity_mentions 只能从各段提供的实体中选择，完整保留 name、type、entity_id、quote，不得改写名称或逐字引文。匹配不到已有实体的新名称也必须保留，供人工确认。
若 fact_allowed=true，从各段 fact 中选择整篇报道的本次发生，最多输出一个 fact。
fact 包含 title、subject、action、object、conditions、occurred_at、quote，
以及 subject_entity_id、object_entity_id、subject_type、object_type。
保留所选事实的端点 ID 与类型，ID 只能来自 entity_options，无 ID 时类型为 organization、person 或 other。
quote 必须完整照抄所选 fact 的引文。综合稿、评论、教程或没有合适事实时输出 fact:null。
若 fact_allowed=false，必须输出 fact:null。$prompt$),
 ('evaluation_questions', $prompt$Generate exactly the number of questions specified by count: $prompt$, $prompt$ Return only a JSON array of objects {"query":"question","answers":[{"entry_id":"supplied ID","grade":1}]}. Use only supplied document IDs. Every question requires at least one positive answer.$prompt$)
), expanded AS (
 SELECT 'prompt.library.' || l.id || '.' || t.id AS key,
        to_jsonb(t.prefix || (s.value #>> '{}') || t.suffix) AS value,
        s.description, s.last_modified_by, s.created_at, s.updated_at
 FROM libraries l CROSS JOIN templates t
 JOIN system_settings s ON s.key='prompt.' || t.id AND jsonb_typeof(s.value)='string'
 WHERE l.deleted_at IS NULL
 UNION ALL
 SELECT 'prompt.evaluation_questions_multi_library',
        to_jsonb($prompt$Generate exactly the number of questions specified by count: $prompt$ || (value #>> '{}') || $prompt$ Return only a JSON array of objects {"query":"question","answers":[{"entry_id":"supplied ID","grade":1}]}. Use only supplied document IDs. Every question requires at least one positive answer.$prompt$),
        description, last_modified_by, created_at, updated_at
 FROM system_settings WHERE key='prompt.evaluation_questions' AND jsonb_typeof(value)='string'
), missing AS (
 SELECT expanded.* FROM expanded
 WHERE NOT EXISTS (SELECT 1 FROM system_settings existing WHERE existing.key=expanded.key)
)
INSERT INTO system_settings (id,key,value,value_type,category,description,is_secret,requires_restart,last_modified_by,created_at,updated_at)
SELECT (SELECT COALESCE(MAX(id),0) FROM system_settings) + row_number() OVER (ORDER BY key COLLATE "C"),
       key,value,'string','prompts',description,false,false,last_modified_by,created_at,updated_at
FROM missing;
SELECT setval(pg_get_serial_sequence('system_settings','id'),
              GREATEST(1,(SELECT COALESCE(MAX(id),0) FROM system_settings)), true);
COMMIT;
