package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
)

func maintenanceWriteGate(check func(context.Context) error) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
				switch call.Params.Name {
				case "shelf_submit", "ingest", "update_entry_content", "delete_document",
					"approve_pending", "discard_pending", "set_star", "create_topic", "update_topic", "follow", "unfollow":
					if err := check(ctx); err != nil {
						result := &mcp.CallToolResult{}
						result.SetError(err)
						formatMaintenanceError(result)
						return result, nil
					}
					result, err := next(ctx, method, req)
					if toolResult, ok := result.(*mcp.CallToolResult); ok && toolResult != nil {
						formatMaintenanceError(toolResult)
					}
					return result, err
				}
			}
			return next(ctx, method, req)
		}
	}
}

func formatMaintenanceError(result *mcp.CallToolResult) {
	err := result.GetError()
	if !maintenanceops.IsPausedError(err) {
		return
	}
	var app *apperrors.AppError
	if errors.As(err, &app) {
		result.Content = []mcp.Content{&mcp.TextContent{Text: app.Message}}
	}
}
