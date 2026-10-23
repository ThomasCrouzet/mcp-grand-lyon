package adapter

import (
	"context"
	"encoding/json"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/service"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const stdioMaxLineLength = 256 * 1024

func New(app *service.App) (*mcp.Server, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: "grand-lyon-mcp", Version: version.Version}, &mcp.ServerOptions{Instructions: "Read-only mobility tools for the Lyon metropolitan area. Examine status, warnings, source dates, and realtime flags before using results."})
	tools, err := service.Tools()
	if err != nil {
		return nil, err
	}
	for _, tool := range tools {
		server.AddTool(&mcp.Tool{Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			result := app.Call(ctx, tool.Name, request.Params.Arguments)
			data, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			return &mcp.CallToolResult{StructuredContent: result, Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
		})
	}
	return server, nil
}

func Run(ctx context.Context, app *service.App) error {
	server, err := New(app)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{MaxLineLength: stdioMaxLineLength})
}
