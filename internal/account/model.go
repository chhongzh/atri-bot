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

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Stats struct {
	Users  int64
	Admins int64
	Banned int64
}

// UserListFilter limits account listings to a single account category.
// A nil Role and Banned includes every account.
type UserListFilter struct {
	Role   *Role
	Banned *bool
}

type UserPage struct {
	Users []User
	Total int64
	Page  int
	Pages int
}
