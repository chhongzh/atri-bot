package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	mcpmanager "github.com/chhongzh/atri-bot/internal/mcp"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/tidwall/gjson"
)

const (
	// GateToolName is the special tool that controls all MCP ability of a user.
	// Admins toggle it through the tool permission system; it is a blanket
	// switch and exposes neither the user's MCP configuration nor per-tool
	// controls.
	GateToolName            = "mcp"
	ListMCPProvidersName    = "list_mcp_providers"
	GetMCPProviderValueName = "get_mcp_provider_value"
	AddMCPProviderName      = "add_mcp_provider"
	UpdateMCPProviderName   = "update_mcp_provider_value"
	RemoveMCPProviderName   = "remove_mcp_provider"
)

type listMCPProvidersInput struct{}

type mcpProviderSummary struct {
	Name       string         `json:"name"`
	URL        string         `json:"url"`
	Meta       map[string]any `json:"meta"`
	HeaderKeys []string       `json:"header_keys"`
}

type listMCPProvidersResult struct {
	Providers []mcpProviderSummary `json:"providers"`
}

type getMCPProviderValueInput struct {
	Name string `json:"name" jsonschema:"required" jsonschema_description:"MCP 工具名"`
	Path string `json:"path" jsonschema:"required" jsonschema_description:"要查询的字段 JSON path，例如 url、meta.timeout、header.Authorization"`
}

type mcpProviderValueResult struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

type addMCPProviderInput struct {
	Name   string            `json:"name" jsonschema:"required" jsonschema_description:"MCP provider 名，同一用户下必须唯一"`
	URL    string            `json:"url" jsonschema:"required" jsonschema_description:"MCP SSE 服务地址，例如 https://example.com/mcp/sse"`
	Meta   map[string]any    `json:"meta,omitempty" jsonschema_description:"可选，发送给 MCP 的附加元数据对象"`
	Header map[string]string `json:"header,omitempty" jsonschema_description:"可选，HTTP 请求头对象，如 Authorization"`
}

type updateMCPProviderValueInput struct {
	Name  string `json:"name" jsonschema:"required" jsonschema_description:"MCP 工具名"`
	Path  string `json:"path" jsonschema:"required" jsonschema_description:"要修改的字段 JSON path，例如 url、meta.timeout、header.Authorization"`
	Value any    `json:"value" jsonschema:"required" jsonschema_description:"字段的新值，类型必须与原有字段一致"`
}

type removeMCPProviderInput struct {
	Name string `json:"name" jsonschema:"required" jsonschema_description:"MCP 工具名"`
}

type removeMCPProviderResult struct {
	Removed string `json:"removed"`
}

// Register registers the MCP permission gate plus the natural language MCP
// provider management tools.
func Register(manager *toolmanager.Manager, mcpManager *mcpmanager.Manager) error {
	if err := manager.RegisterPermission(GateToolName); err != nil {
		return err
	}

	listTool, err := toolutils.InferTool(
		ListMCPProvidersName,
		"列出当前用户已配置的所有外部 MCP provider。请求头只返回键名，URL 查询参数会被掩码。",
		func(ctx context.Context, _ *listMCPProvidersInput) (*listMCPProvidersResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			providers, err := mcpManager.List(ctx, state.UserID)
			if err != nil {
				return nil, err
			}
			result := make([]mcpProviderSummary, 0, len(providers))
			for i := range providers {
				meta := make(map[string]any)
				if gjson.Valid(providers[i].Meta) {
					if parsed, ok := gjson.Parse(providers[i].Meta).Value().(map[string]any); ok {
						meta = parsed
					}
				}
				headerKeys := make([]string, 0)
				if gjson.Valid(providers[i].Header) {
					for key := range gjson.Parse(providers[i].Header).Map() {
						headerKeys = append(headerKeys, key)
					}
				}
				sort.Strings(headerKeys)
				result = append(result, mcpProviderSummary{
					Name:       providers[i].Name,
					URL:        redactProviderURL(providers[i].URL),
					Meta:       meta,
					HeaderKeys: headerKeys,
				})
			}
			return &listMCPProvidersResult{Providers: result}, nil
		},
	)
	if err != nil {
		return err
	}
	if err = manager.RegisterBuiltinWithPermission(ListMCPProvidersName, GateToolName, listTool); err != nil {
		return err
	}

	getTool, err := toolutils.InferTool(
		GetMCPProviderValueName,
		"查询当前用户某个 MCP 工具的单个字段。先用 list_mcp_providers 获取工具名。",
		func(ctx context.Context, input *getMCPProviderValueInput) (*mcpProviderValueResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			name, err := requiredProviderName(input.Name)
			if err != nil {
				return nil, err
			}
			value, err := mcpManager.Value(ctx, state.UserID, name, input.Path)
			if err != nil {
				return nil, err
			}
			path := strings.TrimSpace(input.Path)
			return &mcpProviderValueResult{Name: name, Path: path, Value: displayProviderValue(path, value)}, nil
		},
	)
	if err != nil {
		return err
	}
	if err = manager.RegisterBuiltinWithPermission(GetMCPProviderValueName, GateToolName, getTool); err != nil {
		return err
	}

	addTool, err := toolutils.InferTool(
		AddMCPProviderName,
		"为当前用户添加一个外部 MCP provider。meta 与 header 直接传 JSON 对象，不要转义成字符串。",
		func(ctx context.Context, input *addMCPProviderInput) (*mcpProviderValueResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			name, err := requiredProviderName(input.Name)
			if err != nil {
				return nil, err
			}
			meta, err := marshalJSONObject(input.Meta)
			if err != nil {
				return nil, err
			}
			header, err := marshalJSONObject(input.Header)
			if err != nil {
				return nil, err
			}
			provider, err := mcpManager.Add(ctx, state.UserID, name, input.URL, meta, header)
			if err != nil {
				return nil, err
			}
			return &mcpProviderValueResult{Name: provider.Name, Path: "url", Value: redactProviderURL(provider.URL)}, nil
		},
	)
	if err != nil {
		return err
	}
	if err = manager.RegisterBuiltinWithPermission(AddMCPProviderName, GateToolName, addTool); err != nil {
		return err
	}

	updateTool, err := toolutils.InferTool(
		UpdateMCPProviderName,
		"修改当前用户某个 MCP provider 的字段。可新增 meta 或 header 的子键；修改 url 会按全局网络策略重新校验。",
		func(ctx context.Context, input *updateMCPProviderValueInput) (*mcpProviderValueResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			if input.Value == nil {
				return nil, errors.New("mcp provider value is required")
			}
			name, err := requiredProviderName(input.Name)
			if err != nil {
				return nil, err
			}
			value, err := mcpManager.SetValue(ctx, state.UserID, name, input.Path, input.Value)
			if err != nil {
				return nil, err
			}
			path := strings.TrimSpace(input.Path)
			return &mcpProviderValueResult{Name: name, Path: path, Value: displayProviderValue(path, value)}, nil
		},
	)
	if err != nil {
		return err
	}
	if err = manager.RegisterBuiltinWithPermission(UpdateMCPProviderName, GateToolName, updateTool); err != nil {
		return err
	}

	removeTool, err := toolutils.InferTool(
		RemoveMCPProviderName,
		"删除当前用户的一个 MCP 工具。",
		func(ctx context.Context, input *removeMCPProviderInput) (*removeMCPProviderResult, error) {
			state, err := toolmanager.RequireRunningState(ctx)
			if err != nil {
				return nil, err
			}
			name, err := requiredProviderName(input.Name)
			if err != nil {
				return nil, err
			}
			if err = mcpManager.Remove(ctx, state.UserID, name); err != nil {
				return nil, err
			}
			return &removeMCPProviderResult{Removed: name}, nil
		},
	)
	if err != nil {
		return err
	}
	return manager.RegisterBuiltinWithPermission(RemoveMCPProviderName, GateToolName, removeTool)
}

func requiredProviderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("name is required")
	}
	return name, nil
}

func marshalJSONObject(value any) (string, error) {
	if value == nil {
		return "{}", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal mcp provider object: %w", err)
	}
	return string(data), nil
}

func displayProviderValue(path string, value any) any {
	topLevel := strings.SplitN(strings.TrimSpace(path), ".", 2)[0]
	if topLevel == "url" {
		if rawURL, ok := value.(string); ok {
			return redactProviderURL(rawURL)
		}
	}
	if topLevel != "header" {
		return value
	}
	if header, ok := value.(map[string]any); ok {
		masked := make(map[string]any, len(header))
		for key := range header {
			masked[key] = "********"
		}
		return masked
	}
	return "********"
}

func redactProviderURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.RawQuery == "" {
		return rawURL
	}
	parsed.RawQuery = "redacted"
	parsed.ForceQuery = false
	return parsed.String()
}
