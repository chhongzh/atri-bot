package webread

import (
	"context"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
)

const (
	toolName        = "web_read"
	toolDescription = `Read and extract the main textual content from a web page so it can be summarized or analyzed.`
)

type config struct {
}

type input struct {
}

type result struct {
}

func tool(ctx context.Context, cfg *config, input *input) (*result, error) {
	return &result{}, nil
}

func Register(manager *toolmanager.Manager) error {
	return toolmanager.Register(manager, toolName, toolDescription, config{},
		func(ctx context.Context, _ *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
			return tool(ctx, cfg, input)
		})
}
