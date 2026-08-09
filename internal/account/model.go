package account

import "time"

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"
)

type User struct {
	TelegramID  int64 `gorm:"primaryKey;autoIncrement:false"`
	Username    string
	DisplayName string
	Role        Role `gorm:"type:varchar(16);not null;default:user;index"`
	Banned      bool `gorm:"not null;default:false;index"`

	CharacterID string
	AIBaseURL   string
	AIAPIKey    string
	AIModel     string
	AIMaxRounds int `gorm:"not null;default:36"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Stats struct {
	Users  int64
	Admins int64
	Banned int64
}
