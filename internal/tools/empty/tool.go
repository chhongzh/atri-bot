// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

// Package empty 提供了一个空的tool的代码模板
package empty

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
	return manager.Register("tool", "示例工具的占位描述", config{},
		func(ctx context.Context, _ *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
			return tool(ctx, cfg, input)
		})
}
