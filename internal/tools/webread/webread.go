package webread

import (
	"context"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
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
	return toolmanager.Register(manager, "tool", "tool desc", config{},
		func(ctx context.Context, _ *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
			return tool(ctx, cfg, input)
		})
}
