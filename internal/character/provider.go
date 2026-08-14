// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package character

import (
	"context"

	"github.com/chhongzh/atri-bot/internal/model"
)

type Provider interface {
	ID() string
	Load(context.Context) ([]*model.Character, error)
}
