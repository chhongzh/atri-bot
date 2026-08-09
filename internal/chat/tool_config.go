package chat

import (
	"github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/compose"
)

func toolNodeConfig(manager *tools.Manager) compose.ToolsNodeConfig {
	return compose.ToolsNodeConfig{
		Tools:               manager.Tools(),
		ExecuteSequentially: true,
	}
}
