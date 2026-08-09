package session

import "gorm.io/gorm"

type Record struct {
	gorm.Model
	UserID      int64  `gorm:"not null;index:idx_records_session_order,priority:1;index:idx_records_session_role_order,priority:1"`
	CharacterID string `gorm:"size:255;not null;index:idx_records_session_order,priority:2;index:idx_records_session_role_order,priority:2"`
	Role        string `gorm:"size:32;not null;index:idx_records_session_role_order,priority:3"`
	Message     string `gorm:"type:json;not null"`
}
