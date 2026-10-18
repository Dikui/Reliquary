package mcpserver

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reliquary/internal/diagnostics"
	"reliquary/internal/types"
)

func registerIngestDiagnostics(server *mcp.Server, s *diagnostics.Service) {
	if s == nil {
		return
	}
	addTool(server, "list_ingest_failures", "辅助诊断：分页查询接收技术故障；不包括明确拒绝的内容。"+externalContentNotice, object(map[string]any{
		"page": integer("页码，默认1", 1, 0), "page_size": integer("每页数量，最大100", 1, 100), "from": text("开始时间 RFC3339"), "to": text("结束时间 RFC3339"), "api_key_id": text("提交工具标识"), "library_id": text("库ID"), "error_code": text("故障类别"), "request_id": text("请求编号"), "id": text("故障编号"),
	}), func(ctx context.Context, q types.FailureQuery) (any, error) { return s.List(ctx, q) })
	addTool(server, "get_ingest_failure", "辅助诊断：读取接收故障、执行条件、材料清单、完整性与过期状态；读取不执行重放。"+externalContentNotice, object(map[string]any{"id": text("故障编号")}, "id"), func(ctx context.Context, in idInput) (any, error) { return s.Get(ctx, in.ID) })
	type materialInput struct {
		ID         string `json:"id"`
		MaterialID string `json:"material_id"`
		Offset     int    `json:"offset,omitempty"`
		Length     int    `json:"length,omitempty"`
	}
	addTool(server, "read_ingest_failure_material", "辅助诊断：读取故障包中的原始输入或调用记录；文本按Unicode分段，图片返回需Bearer认证的HTTP读取地址。材料过期或缺失明确失败。"+externalContentNotice, object(map[string]any{"id": text("故障编号"), "material_id": text("清单中的材料标识"), "offset": integer("Unicode偏移，默认0", 0, 0), "length": integer("默认20000，最大100000", 0, 100000)}, "id", "material_id"), func(ctx context.Context, in materialInput) (any, error) {
		return s.ReadText(ctx, in.ID, in.MaterialID, in.Offset, in.Length)
	})
}
