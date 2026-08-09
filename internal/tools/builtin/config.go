package builtin

import (
	"context"
	"errors"
	"sort"
	"strings"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
)

const (
	ListConfigurableToolsName = "list_configurable_tools"
	GetToolConfigName         = "get_tool_config"
	ConfigureToolName         = "configure_tool"
)

type listConfigurableToolsInput struct{}

type listConfigurableToolsResult struct {
	ToolNames []string `json:"tool_names"`
}

type getToolConfigInput struct {
	ToolName string `json:"tool_name" jsonschema:"required" jsonschema_description:"要查询配置的工具名"`
	Path     string `json:"path" jsonschema:"required" jsonschema_description:"要查询的配置项 JSON path，例如 smtpHost"`
}

type toolConfigValueResult struct {
	ToolName string `json:"tool_name"`
	Path     string `json:"path"`
	Value    any    `json:"value"`
}

type configureToolInput struct {
	ToolName string `json:"tool_name" jsonschema:"required" jsonschema_description:"要修改配置的工具名"`
	Path     string `json:"path" jsonschema:"required" jsonschema_description:"要修改的配置项 JSON path，例如 smtpHost"`
	Value    any    `json:"value" jsonschema:"required" jsonschema_description:"配置项的新值，类型必须与工具配置定义一致"`
}

func Register(manager *toolmanager.Manager) error {
	if manager == nil {
		return errors.New("tool manager is nil")
	}

	listTool, err := toolutils.InferTool(
		ListConfigurableToolsName,
		"列举所有可以由当前用户单独配置的工具。",
		func(context.Context, *listConfigurableToolsInput) (*listConfigurableToolsResult, error) {
			names := manager.Names()
			sort.Strings(names)
			return &listConfigurableToolsResult{ToolNames: names}, nil
		},
	)
	if err != nil {
		return err
	}

	getTool, err := toolutils.InferTool(
		GetToolConfigName,
		"查询当前用户某个工具的单个配置项。先用 list_configurable_tools 获取可配置工具名。",
		func(ctx context.Context, input *getToolConfigInput) (*toolConfigValueResult, error) {
			state, ok := toolmanager.RunningStateFromContext(ctx)
			if !ok {
				return nil, toolmanager.ErrRunningStateMissing
			}
			toolName, err := requiredToolName(input.ToolName)
			if err != nil {
				return nil, err
			}
			value, err := manager.ConfigValue(ctx, state.UserID, toolName, input.Path)
			if err != nil {
				return nil, err
			}
			return &toolConfigValueResult{ToolName: toolName, Path: strings.TrimSpace(input.Path), Value: value}, nil
		},
	)
	if err != nil {
		return err
	}

	configureTool, err := toolutils.InferTool(
		ConfigureToolName,
		"修改当前用户某个工具的单个配置项。path 必须是已有配置项，value 类型必须与配置定义一致。",
		func(ctx context.Context, input *configureToolInput) (*toolConfigValueResult, error) {
			state, ok := toolmanager.RunningStateFromContext(ctx)
			if !ok {
				return nil, toolmanager.ErrRunningStateMissing
			}
			toolName, err := requiredToolName(input.ToolName)
			if err != nil {
				return nil, err
			}
			value, err := manager.SetConfigValue(ctx, state.UserID, toolName, input.Path, input.Value)
			if err != nil {
				return nil, err
			}
			return &toolConfigValueResult{ToolName: toolName, Path: strings.TrimSpace(input.Path), Value: value}, nil
		},
	)
	if err != nil {
		return err
	}

	if err = manager.RegisterBuiltin(ListConfigurableToolsName, listTool); err != nil {
		return err
	}
	if err = manager.RegisterBuiltin(GetToolConfigName, getTool); err != nil {
		return err
	}
	return manager.RegisterBuiltin(ConfigureToolName, configureTool)
}

func requiredToolName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("tool_name is required")
	}
	return name, nil
}
