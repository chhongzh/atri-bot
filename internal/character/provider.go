package character

import (
	"context"

	"github.com/chhongzh/atri-bot/internal/model"
)

type Provider interface {
	ID() string
	Load(context.Context) ([]*model.Character, error)
}
