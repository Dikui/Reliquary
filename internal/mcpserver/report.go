package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reliquary/internal/application/service"
)

// The report tool shares aggregate validation with HTTP instead of stopping at
// the first SDK schema error. The published schema remains the format contract.
func addReportTool(server *mcp.Server, reports *service.VaultDocumentService) {
	server.AddTool(&mcp.Tool{Name: "write_report", Description: "将外部 Agent 的 AI 报告写入 ai-reports/。标题和 Markdown 正文必填，关联资料、已有实体和主题可选。一次返回全部字段错误，实体名称有多个候选时返回候选 ID。不审核、不调用生成模型，只建立 BM25 和向量索引。报告不能当作事实，不进入实体档案或图谱。request_id 优先幂等，否则按规范化内容指纹幂等，重试返回 existing=true。索引失败保留报告并排队重试。", InputSchema: object(map[string]any{
		"library":             text("可省略；仅接受 ai-reports 或该系统库 ID"),
		"title":               text("标题必填，最多 255 个字符"),
		"markdown":            text("Markdown 正文必填，最多 5000000 个字符"),
		"related_article_ids": stringArray("已存在且未删除的关联资料 ID"),
		"related_entities":    stringArray("已有实体 ID、规范名或别名；多候选拒绝"),
		"topics":              stringArray("已有主题 ID 或名称"),
		"request_id":          text("可选调用方请求 ID，最多 255 个字符；优先作为幂等键"),
	}, "title", "markdown")}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		input, err := service.DecodeReportInput(bytes.NewReader(request.Params.Arguments))
		var result *service.ReportWriteResult
		if err == nil {
			result, err = reports.CreateReportRequest(ctx, input)
		}
		if err != nil {
			output := &mcp.CallToolResult{}
			output.SetError(err)
			return output, nil
		}
		contract, err := contractOutput(result)
		if err != nil {
			return nil, err
		}
		data, err := json.Marshal(contract)
		if err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{StructuredContent: contract, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	})
}
