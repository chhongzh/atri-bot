package character

import "context"

type Provider interface {
	ID() string
	Load(context.Context) ([]*Character, error)
}
