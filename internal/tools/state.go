// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package tools

import (
	"context"

	"github.com/chhongzh/atri-bot/internal/errs"
	"gopkg.in/telebot.v4"
)

type RunningState struct {
	UserID         int64
	CharacterID    string
	TelebotContext telebot.Context
}

type runningStateKey struct{}

func WithRunningState(ctx context.Context, state *RunningState) context.Context {
	return context.WithValue(ctx, runningStateKey{}, state)
}

func RunningStateFromContext(ctx context.Context) (*RunningState, bool) {
	state, ok := ctx.Value(runningStateKey{}).(*RunningState)
	return state, ok
}

// RequireRunningState returns the running state from ctx,
// or errs.ErrRunningStateMissing when it is absent.
func RequireRunningState(ctx context.Context) (*RunningState, error) {
	state, ok := RunningStateFromContext(ctx)
	if !ok {
		return nil, errs.ErrRunningStateMissing
	}
	return state, nil
}
