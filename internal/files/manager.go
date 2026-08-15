package files

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/chhongzh/atri-bot/internal/config"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const MaxBytes int64 = 20 << 20 // Telegram's hosted Bot API download limit.

type Ref struct{ ID string }

type Manager struct {
	db     *gorm.DB
	logger *zap.Logger
	client *http.Client
}

func New(db *gorm.DB, logger *zap.Logger, client *http.Client) *Manager {
	return &Manager{db: db, logger: logger, client: client}
}

func (m *Manager) Init() error { return m.db.AutoMigrate(&model.ProviderFile{}) }

func (m *Manager) Upload(ctx context.Context, settings config.UserSettings, userID int64, characterID, kind, name string, body io.ReadCloser, size int64) (Ref, error) {
	defer body.Close()
	if size > MaxBytes {
		return Ref{}, fmt.Errorf("媒体文件不能超过 %d MB", MaxBytes>>20)
	}
	endpoint := strings.TrimRight(strings.TrimSpace(settings.AIBaseURL), "/") + "/files"
	pipeReader, pipeWriter := io.Pipe()
	writer := multipart.NewWriter(pipeWriter)
	go func() {
		defer pipeWriter.Close()
		defer writer.Close()
		if err := writer.WriteField("purpose", "user_data"); err != nil {
			_ = pipeWriter.CloseWithError(err)
			return
		}
		part, err := writer.CreateFormFile("file", cleanName(name))
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
			return
		}
		var copied int64
		copied, err = io.Copy(part, io.LimitReader(body, MaxBytes+1))
		if copied > MaxBytes && err == nil {
			err = fmt.Errorf("media exceeds %d MB", MaxBytes>>20)
		}
		if err != nil {
			_ = pipeWriter.CloseWithError(err)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pipeReader)
	if err != nil {
		return Ref{}, err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(settings.AIAPIKey))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := m.client.Do(req)
	if err != nil {
		return Ref{}, fmt.Errorf("upload file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Ref{}, fmt.Errorf("upload file: provider returned %s", response.Status)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return Ref{}, fmt.Errorf("decode file response: %w", err)
	}
	if result.ID == "" {
		return Ref{}, fmt.Errorf("upload file: provider response has no id")
	}
	record := model.ProviderFile{ID: uuid.NewString(), UserID: userID, CharacterID: characterID, AIConfigRevision: settings.AIConfigRevision, ProviderFileID: result.ID, Kind: kind}
	if err = m.db.WithContext(ctx).Create(&record).Error; err != nil {
		m.delete(ctx, settings, result.ID)
		return Ref{}, err
	}
	m.logger.Info("uploaded provider file", zap.String("file_ref", record.ID), zap.Int64("user_id", userID), zap.String("kind", kind))
	return Ref{ID: record.ID}, nil
}

func (m *Manager) delete(ctx context.Context, settings config.UserSettings, id string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(strings.TrimSpace(settings.AIBaseURL), "/")+"/files/"+url.PathEscape(id), nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(settings.AIAPIKey))
	response, err := m.client.Do(req)
	if err == nil {
		response.Body.Close()
	}
}

func (m *Manager) IDs(ctx context.Context, userID int64, characterID string, revision uint64, refs []string) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	var records []model.ProviderFile
	if err := m.db.WithContext(ctx).Where("id IN ? AND user_id = ? AND character_id = ? AND ai_config_revision = ?", refs, userID, characterID, revision).Find(&records).Error; err != nil {
		return nil, err
	}
	byID := make(map[string]string, len(records))
	for _, record := range records {
		byID[record.ID] = record.ProviderFileID
	}
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		if id := byID[ref]; id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func cleanName(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		return "upload.bin"
	}
	return strings.Map(func(r rune) rune {
		if r < 32 {
			return -1
		}
		return r
	}, name)
}
