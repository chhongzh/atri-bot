// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

// Package exit provides a control tool for ending a chat turn silently.
package exit

import (
	"context"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"go.uber.org/zap"
)

type input struct{}

// Register adds the silent turn-exit tool.
func Register(manager *toolmanager.Manager, logger *zap.Logger) error {
	exitTool, err := toolutils.InferTool(
		"exit",
		"立即结束当前聊天轮次且不向用户发送任何文字。仅在明确应当保持沉默时调用，例如无法理解、疑似错字、无理取闹或用户明确不需要回复；调用后不得再输出文字。",
		func(ctx context.Context, _ *input) (*struct{}, error) {
			state, ok := toolmanager.RunningStateFromContext(ctx)
			if ok {
				logger.Info("model exited chat turn without a reply",
					zap.Int64("user_id", state.UserID),
					zap.String("character_id", state.CharacterID),
				)
			} else {
				logger.Info("model exited chat turn without a reply")
			}
			return nil, tool.Interrupt(ctx, "silent chat turn exit")
		},
	)
	if err != nil {
		return err
	}
	return manager.RegisterBuiltin("exit", exitTool)
}
