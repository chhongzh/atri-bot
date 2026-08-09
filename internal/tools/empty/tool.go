package hotspot

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

func mailTool(ctx context.Context, cfg *config, input *input) (*result, error) {
	return &result{}, nil
}

func Register(manager *toolmanager.Manager) error {
	return toolmanager.Register(manager, "send_mail", "发送邮件", config{},
		func(ctx context.Context, _ *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
			return mailTool(ctx, cfg, input)
		})
}
