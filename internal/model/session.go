// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package model

import "time"

type SessionRound struct {
	ID          uint      `gorm:"primaryKey"`
	CreatedAt   time.Time `gorm:"not null"`
	UserID      int64     `gorm:"not null;index:idx_session_rounds_session_order,priority:1"`
	CharacterID string    `gorm:"size:255;not null;index:idx_session_rounds_session_order,priority:2"`
	Interrupted bool      `gorm:"not null"`
	Completed   bool      `gorm:"not null"`
}

func (SessionRound) TableName() string {
	return "session_rounds"
}

type SessionMessage struct {
	ID              uint      `gorm:"primaryKey"`
	CreatedAt       time.Time `gorm:"not null"`
	UserID          int64     `gorm:"not null;index:idx_session_messages_session_order,priority:1"`
	CharacterID     string    `gorm:"size:255;not null;index:idx_session_messages_session_order,priority:2"`
	RoundID         uint      `gorm:"not null;index:idx_session_messages_session_order,priority:3;index"`
	Interrupted     bool      `gorm:"not null"`
	MessageMetadata bool      `gorm:"not null"`
	Message         string    `gorm:"type:json;not null"`
}

func (SessionMessage) TableName() string {
	return "session_messages"
}

type SessionSummary struct {
	ID            uint      `gorm:"primaryKey"`
	CreatedAt     time.Time `gorm:"not null"`
	UserID        int64     `gorm:"not null;index:idx_session_summaries_latest,priority:1"`
	CharacterID   string    `gorm:"size:255;not null;index:idx_session_summaries_latest,priority:2"`
	CutoffRoundID uint      `gorm:"not null;index:idx_session_summaries_latest,priority:3,sort:desc"`
	Message       string    `gorm:"type:json;not null"`
}

func (SessionSummary) TableName() string {
	return "session_summaries"
}
