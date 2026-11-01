package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"reliquary/internal/application/service"
	"reliquary/internal/graphreplica"
	"reliquary/internal/types"
)

func registerGraphSearch(server *mcp.Server, graph *service.GraphSearchService) {
	if graph == nil {
		return
	}
	entity := object(map[string]any{"id": text("实体 ID，与 name 二选一"), "name": text("规范名或别名，与 id 二选一；多个匹配返回候选供调用方选择")})
	// Graph response fields already use entry_id and require no legacy rewrite.
	mcp.AddTool(server, &mcp.Tool{Name: "graph_search", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)}, Description: "必须指定一个或多个库。按规范名、别名或实体 ID 检索邻居或两实体间路径，跳数最多 3，默认 1；只沿所选库的边扩展，不调用模型。返回来源资料、位置、发言人、截断标记和未同步记录数。" + externalContentNotice, InputSchema: object(map[string]any{
		"libraries": boundedArray("必填，库 ID 或目录", map[string]any{"type": "string"}, 1, 100),
		"entity":    entity, "target": entity, "mode": enum("neighbors 为邻居，path 为路径", "neighbors", "path"), "hops": integer("跳数，默认 1，最大 3", 1, 3), "relation_types": stringArray("可选关系类型"), "limit": integer("关系或路径及资料列表上限，默认 20", 1, 100),
	}, "libraries", "entity")}, func(ctx context.Context, _ *mcp.CallToolRequest, input types.GraphSearchRequest) (*mcp.CallToolResult, any, error) {
		out, err := graph.Search(types.WithSearchSource(ctx, "mcp"), input)
		if errors.Is(err, graphreplica.ErrUnavailable) {
			err = graphreplica.ErrUnavailable
		}
		return nil, out, err
	})
}
