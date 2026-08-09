package chat

import (
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

func toolNodeConfig(tools []tool.BaseTool) compose.ToolsNodeConfig {
	return compose.ToolsNodeConfig{
		Tools:               tools,
		ExecuteSequentially: true,
	}
}
