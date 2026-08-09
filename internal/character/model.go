package character

import (
	"time"
)

type ProviderKind string

const (
	ProviderLocal  ProviderKind = "local"
	ProviderRemote ProviderKind = "remote"
)

type ProviderRecord struct {
	ID        string       `gorm:"primaryKey;size:128"`
	Kind      ProviderKind `gorm:"type:varchar(16);not null"`
	URL       string
	Branch    string
	Path      string
	BuiltIn   bool `gorm:"not null;default:false"`
	Enabled   bool `gorm:"not null;default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Character struct {
	ID         string
	ProviderID string
	Definition map[string]any
	Source     string
}

func (c *Character) Name() string {
	for _, key := range []string{"name", "display_name", "displayName"} {
		if value, ok := c.Definition[key].(string); ok && value != "" {
			return value
		}
	}
	return c.ID
}
