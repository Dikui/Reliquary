package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

const shelfExpertNotice = "货架专家须注明来源与数据时点，说明适用条件、来自部件和来自上层；GA 不代表可销售或认证齐全；报告总数并说明截断；同名且影响答案时先澄清；无记录说“货架中没有记录”。先读 reliquary_guide 的 shelf-expert 流程。"

type shelfFindInput struct {
	LibraryID string `json:"library_id"`
	types.ShelfFindRequest
}

type shelfGetInput struct {
	LibraryID   string            `json:"library_id"`
	NodeID      string            `json:"node_id"`
	Combination map[string]string `json:"combination,omitempty"`
	AsOf        *time.Time        `json:"as_of,omitempty"`
}

type shelfQueryInput struct {
	LibraryID string `json:"library_id"`
	types.ShelfQueryRequest
}

type shelfSubmitInput struct {
	LibraryID   string                 `json:"library_id"`
	ChangesetID string                 `json:"changeset_id,omitempty"`
	Revision    int64                  `json:"revision,omitempty"`
	Source      *types.OperationSource `json:"source,omitempty"`
	Operations  []types.ShelfOperation `json:"operations,omitempty"`
	Confirmed   bool                   `json:"confirmed,omitempty"`
	Decisions   []types.ShelfDecision  `json:"decisions,omitempty"`
}

func registerShelfTools(server *mcp.Server, entities *service.EntityService) {
	if entities == nil {
		return
	}
	addTool(server, "shelf_find", mcp.ToolAnnotations{ReadOnlyHint: true}, "按名称、别名或代码找货架节点，返回 items（含 id、kind、kind_label）、分页前 total 和 truncated，不自动选择同名候选。"+shelfExpertNotice+externalContentNotice,
		object(map[string]any{
			"library_id": text("已开启货架的库 ID，必填"), "q": text("名称、别名或代码中的字面文本，必填"),
			"limit": integer("默认 50，最大 100", 1, 100), "offset": integer("跳过条数，默认 0", 0, 0),
		}, "library_id", "q"), func(ctx context.Context, in shelfFindInput) (any, error) {
			return entities.ShelfFind(ctx, in.LibraryID, in.ShelfFindRequest)
		})
	addTool(server, "shelf_get", mcp.ToolAnnotations{ReadOnlyHint: true}, "按已澄清的节点 ID 读取详情，支持部件组合与业务时间。返回 entity、shelf、facts、as_of、recorded，事实保留条件、来源日志、快照及生效和记录时间；recorded=false 表示该时点无记录。"+shelfExpertNotice+externalContentNotice,
		object(map[string]any{
			"library_id": text("已开启货架的库 ID，必填"), "node_id": text("候选中明确选定的节点 ID，必填"),
			"combination": map[string]any{"type": "object", "additionalProperties": text("本模块选择的部件 ID"), "description": "模块键到部件 ID 的映射，指定时须选择每个选配模块；省略返回所有适用条件"},
			"as_of":       text("业务时间，RFC3339，省略为当前时间"),
		}, "library_id", "node_id"), func(ctx context.Context, in shelfGetInput) (any, error) {
			return entities.ShelfGet(ctx, in.LibraryID, in.NodeID, types.ShelfReadOptions{AsOf: in.AsOf, Combination: in.Combination})
		})
	addTool(server, "shelf_query", mcp.ToolAnnotations{ReadOnlyHint: true}, "结构化筛选或按部件、认证标准反查型号，所有筛选须由同一可行组合满足。返回 items（完整详情、matching_conditions、partial_options）、分页前 total、truncated、as_of；matching_conditions 外层为或，内层为且。"+shelfExpertNotice+externalContentNotice,
		object(map[string]any{
			"library_id": text("已开启货架的库 ID，必填"), "ancestor_id": text("产品线、平台或产品名称 ID，限定其下属型号"),
			"sale_statuses": map[string]any{"type": "array", "description": "区域状态筛选，各项取交集", "items": object(map[string]any{"region": text("配置区域键"), "status": enum("状态", "GA", "DEV", "Planning", "EOM", "待定")}, "region", "status")},
			"parameters":    map[string]any{"type": "array", "description": "数值参数闭区间筛选，各项取交集", "items": object(map[string]any{"field": text("本库参数字段键"), "min": map[string]any{"type": "number"}, "max": map[string]any{"type": "number"}}, "field")},
			"composition":   shelfConditionsSchema(), "part_id": text("反查使用此部件的型号"),
			"certification": object(map[string]any{"standard": text("标准配置键、规范名或明确别名"), "progress": text("进度原文"), "region": text("地区原文")}),
			"document_gaps": boolean("只查有到期资料缺口的型号，默认 false"), "as_of": text("业务时间，RFC3339，省略为当前时间"),
			"limit": integer("默认 50，最大 100", 1, 100), "offset": integer("跳过条数，默认 0", 0, 0),
		}, "library_id"), func(ctx context.Context, in shelfQueryInput) (any, error) {
			return entities.ShelfQuery(ctx, in.LibraryID, in.ShelfQueryRequest)
		})
	mcp.AddTool(server, &mcp.Tool{Name: "shelf_submit", Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(false)}, Description: "提交有类型的货架操作列表，返回持久化变更集及逐项 before、after、conflicts。未带 confirmed 或为 false 只保存待确认提议，不改货架。预览后用 changeset_id、revision 和逐项 decisions 继续保存或确认同一变更集，不重传 source、operations。必须向用户展示差异与冲突并取得明确同意后才传 confirmed=true，不得自行补确认。含义不确定项必须先修正、放弃或暂缓，普通 apply 不能跳过。" + shelfExpertNotice + externalContentNotice, InputSchema: object(map[string]any{
		"library_id":   text("已开启货架的库 ID，必填"),
		"changeset_id": text("继续处理先前返回的待确认变更集 ID；此时必传 revision，不传 source、operations"), "revision": integer("读取到的变更集修订号", 1, 0),
		"source":     object(map[string]any{"type": enum("来源类型", "agent"), "agent_name": text("提交 agent 名称，必填")}, "type", "agent_name"),
		"operations": boundedArray("有类型操作列表，按顺序执行；用 $操作id 引用本批前项，名称原样保存", shelfOperationSchema(), 1, 500),
		"confirmed":  boolean("仅在用户明确同意所展示的差异后传 true；省略或 false 只保存提议"),
		"decisions":  map[string]any{"type": "array", "items": shelfDecisionSchema()},
	}, "library_id")}, func(ctx context.Context, request *mcp.CallToolRequest, in shelfSubmitInput) (*mcp.CallToolResult, any, error) {
		// Configurable payloads keep open schemas, but their typed envelopes must
		// reject unknown fields just like the HTTP changeset endpoint.
		decoder := json.NewDecoder(bytes.NewReader(request.Params.Arguments))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&in); err != nil {
			return nil, nil, apperrors.NewBadRequestError(err.Error())
		}
		output, err := submitShelf(ctx, entities, in)
		if err != nil {
			var app *apperrors.AppError
			if !errors.As(err, &app) {
				return nil, nil, err
			}
			failure, encodeErr := contractOutput(map[string]any{"error": app})
			if encodeErr != nil {
				return nil, nil, encodeErr
			}
			data, encodeErr := json.Marshal(failure)
			if encodeErr != nil {
				return nil, nil, encodeErr
			}
			return &mcp.CallToolResult{IsError: true, StructuredContent: failure, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil, nil
		}
		value, err := contractOutput(output)
		return nil, value, err
	})
}

func submitShelf(ctx context.Context, entities *service.EntityService, in shelfSubmitInput) (*types.ShelfChangeset, error) {
	if in.ChangesetID != "" {
		if in.Revision < 1 || in.Operations != nil || in.Source != nil {
			return nil, apperrors.NewBadRequestError("review requires revision and must not replace source or operations; use decisions instead")
		}
		return entities.ReviewShelfChangeset(ctx, in.LibraryID, in.ChangesetID, types.ShelfChangesetReview{Revision: in.Revision, Decisions: in.Decisions}, in.Confirmed)
	}
	if in.Revision != 0 {
		return nil, apperrors.NewBadRequestError("revision requires changeset_id")
	}
	if in.Source == nil || in.Source.Type != "agent" {
		return nil, apperrors.NewBadRequestError("shelf_submit requires a named agent source")
	}
	return entities.SubmitShelfChangeset(ctx, in.LibraryID, types.ShelfChangesetInput{Source: *in.Source, Operations: in.Operations, Confirmed: in.Confirmed, Decisions: in.Decisions})
}

func shelfOperationSchema() map[string]any {
	// Payload fields follow the shared changeset DTO. Business validation and
	// uncertain meanings remain owned by the changeset service.
	return object(map[string]any{
		"id":        text("本批唯一操作 ID，省略由服务端分配"),
		"type":      enum("操作类型，提供对应数据字段", "create_entity", "update_entity", "add_alias", "archive_entity", "missing_entity", "save_relation", "end_relation", "set_entity_link", "configure_shelf", "set_sale_statuses", "set_parameter_values", "create_certification", "update_certification", "add_document_version", "reparse_document", "save_import_plan", "import_notice"),
		"entity_id": text("目标实体 ID，可用 $操作id 引用前项"), "candidate_entity_id": text("疑似同一的实体 ID，须由提交方决定"),
		"entity": object(map[string]any{
			"name": text("原始名称"), "kind": text("本库类别键"), "code": text("代码"), "description": text("说明"), "aliases": stringArray("明确别名"),
			"attributes": shelfPayloadSchema("完整属性快照"), "auto_match": map[string]any{"type": "object", "additionalProperties": boolean("是否匹配此名称")},
			"effective_at": text("RFC3339 生效时间"), "name_confirmations": shelfNameConfirmationsSchema(),
		}, "name", "kind"),
		"patch": object(map[string]any{
			"revision": integer("读取到的实体修订号，必填", 1, 0), "name": text("原始名称"), "kind": text("类别键"), "code": text("代码"), "description": text("说明"),
			"aliases": stringArray("完整别名列表"), "attributes": shelfPayloadSchema("完整属性快照"),
			"auto_match": map[string]any{"type": "object", "additionalProperties": boolean("是否匹配此名称")}, "archived": boolean("归档状态"),
			"effective_at": text("RFC3339 生效时间"), "name_confirmations": shelfNameConfirmationsSchema(),
		}, "revision"),
		"revision": integer("读取到的实体修订号或关系版本", 1, 0), "alias": text("明确新增的别名"), "effective_at": text("状态、参数或认证的 RFC3339 生效时间"),
		"relation_id": text("修改或结束的关系 ID"), "valid_to": text("结束关系的 RFC3339 时间"),
		"relation": object(map[string]any{
			"subject_id": text("主体 ID"), "object_id": text("客体 ID"), "relation_type": text("本库关系类型键"), "attributes": shelfPayloadSchema("完整关系属性"),
			"version": integer("修改时的关系版本", 1, 0), "valid_from": text("RFC3339 生效时间"),
		}, "subject_id", "object_id", "relation_type"),
		"entry_id": text("明确关联的资料 ID"), "excluded": boolean("是否排除此关联"), "relation_type": text("资料关联类型"),
		"library": shelfPayloadSchema("configure_shelf 的库设置 patch，与 HTTP 库设置相同；客户模块、区域、阶段等使用本库配置"), "library_updated_at": text("读取到的库更新时间，RFC3339"),
		"sale_statuses": map[string]any{"type": "array", "items": object(map[string]any{
			"region": text("配置区域键"), "status": enum("状态，空白须提交方决定", "GA", "DEV", "Planning", "EOM", "待定", ""),
			"planned_eom": boolean("计划退市"), "notes": text("备注"), "raw": text("原始写法"), "conditions": shelfConditionsSchema(),
		}, "region", "status")},
		"parameter_values": map[string]any{"type": "object", "description": "字段键到参数值数组，空数组移除该参数", "additionalProperties": map[string]any{"type": "array", "items": object(map[string]any{
			"value": map[string]any{"description": "按配置字段类型提供值，数值也可为闭区间 {min,max}"}, "conditions": shelfConditionsSchema(),
		}, "value")}},
		"certification":   shelfPayloadSchema("认证完整字段：name、owner_id、standard、certification_object、cell_part_id、region、year、agency、progress、start_date、obtained_date、issued_date、validity、number、cloud_url、conditions；新增须给 name，日期为 YYYY-MM-DD；不猜所属节点"),
		"document":        shelfPayloadSchema("资料版本：name、owner_id、requirement_id、certificate_number、attachment_id 或 cloud_url、entry_id、version_label、stage(draft/final)、origin、occurred_at、conditions；归属须明确指定"),
		"reparse":         object(map[string]any{"version_id": text("原资料版本 ID"), "parser_key": text("配置解析器键")}, "version_id", "parser_key"),
		"import_plan":     shelfPayloadSchema("保存导入方案，字段与 HTTP 变更集 save_import_plan 相同，含 key、revision、file_kind、name、sheets 等映射"),
		"import_location": shelfPayloadSchema("导入来源位置，与 HTTP 变更集相同"),
		"issues": map[string]any{"type": "array", "items": object(map[string]any{
			"type": enum("提交方标出的阻塞问题", "unparseable", "uncertain", "required_missing", "cross_table_contradiction"), "message": text("原始问题说明"), "details": map[string]any{},
		}, "type")},
	}, "type")
}

func shelfDecisionSchema() map[string]any {
	return object(map[string]any{
		"item_id": text("变更集项 ID"), "action": enum("逐项决定", "apply", "discard", "defer", "replace", "same_entity", "keep", "retire"),
		"target_entity_id": text("明确选择的目标实体 ID"), "target_revision": integer("目标修订号", 1, 0),
		"name_confirmations": shelfNameConfirmationsSchema(), "replacement": shelfOperationSchema(),
	}, "item_id", "action")
}

func shelfNameConfirmationsSchema() map[string]any {
	return map[string]any{"type": "array", "items": object(map[string]any{
		"other_entity_id": text("确认同名的另一实体 ID"), "other_kind": text("另一实体类别键"), "name": text("确认的原始同名写法"),
	}, "other_entity_id", "other_kind", "name")}
}

func shelfPayloadSchema(description string) map[string]any {
	return map[string]any{"type": "object", "description": description, "additionalProperties": true}
}

func shelfConditionsSchema() map[string]any {
	return map[string]any{"type": "array", "description": "各模块条件取交集，同模块部件集合内任选其一，空数组为全部适用", "items": object(map[string]any{
		"module_type": text("本库模块类型键"), "part_ids": map[string]any{"type": "array", "minItems": 1, "items": text("允许的本库部件 ID")},
	}, "module_type", "part_ids")}
}
