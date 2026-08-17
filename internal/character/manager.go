// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package character

import (
	"context"
	_ "embed"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const localProviderID = "local"

var providerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

//go:embed system.j2
var systemTemplate string

type Config struct {
	CWD          string
	RemoteURL    string
	RemoteBranch string
}

type Manager struct {
	db     *gorm.DB
	logger *zap.Logger
	cfg    Config

	mu             sync.RWMutex
	characters     map[string]*model.Character
	systemTemplate *prompt.DefaultChatTemplate
}

func New(db *gorm.DB, logger *zap.Logger, cfg Config) *Manager {
	return &Manager{
		db:             db,
		logger:         logger,
		cfg:            cfg,
		characters:     make(map[string]*model.Character),
		systemTemplate: prompt.FromMessages(schema.Jinja2, schema.SystemMessage(systemTemplate)),
	}
}

func (m *Manager) Init(ctx context.Context) error {
	if err := m.db.WithContext(ctx).AutoMigrate(&model.Provider{}); err != nil {
		return err
	}
	localRoot := filepath.Join(m.cfg.CWD, "chardefs")
	local := model.Provider{
		ID:      localProviderID,
		Kind:    model.ProviderLocal,
		Path:    localRoot,
		BuiltIn: true,
		Enabled: true,
	}
	if err := m.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"kind", "url", "branch", "path", "built_in", "enabled", "updated_at"}),
	}).Create(&local).Error; err != nil {
		return err
	}
	if strings.TrimSpace(m.cfg.RemoteURL) != "" {
		remote := model.Provider{
			ID:      "remote-default",
			Kind:    model.ProviderRemote,
			URL:     utils.NormalizeGitRepoURL(m.cfg.RemoteURL),
			Branch:  m.cfg.RemoteBranch,
			Path:    utils.GetProviderPath(m.cfg.CWD, "remote-default", m.cfg.RemoteURL, m.cfg.RemoteBranch),
			BuiltIn: true,
			Enabled: true,
		}
		if err := m.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoNothing: true,
		}).Create(&remote).Error; err != nil {
			return err
		}
	}
	return m.Reload(ctx)
}

func (m *Manager) Reload(ctx context.Context) error {
	records, err := m.Providers(ctx)
	if err != nil {
		return err
	}
	loaded := make(map[string]*model.Character)
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		provider, buildErr := providerFromRecord(record)
		if buildErr != nil {
			m.logger.Warn("invalid character provider", zap.String("provider", record.ID), zap.Error(buildErr))
			continue
		}
		characters, loadErr := provider.Load(ctx)
		if loadErr != nil {
			m.logger.Warn("failed to load character provider", zap.String("provider", record.ID), zap.Error(loadErr))
			continue
		}
		for _, character := range characters {
			if _, exists := loaded[character.ID]; exists {
				m.logger.Warn("duplicate character id ignored",
					zap.String("character_id", character.ID),
					zap.String("provider", record.ID),
				)
				continue
			}
			loaded[character.ID] = character
		}
	}
	m.mu.Lock()
	m.characters = loaded
	m.mu.Unlock()
	m.logger.Info("reloaded characters", zap.Int("count", len(loaded)))
	return nil
}

func (m *Manager) List() []*model.Character {
	m.mu.RLock()
	characters := make([]*model.Character, 0, len(m.characters))
	for _, character := range m.characters {
		characters = append(characters, character)
	}
	m.mu.RUnlock()
	sort.Slice(characters, func(i, j int) bool { return characters[i].ID < characters[j].ID })
	return characters
}

func (m *Manager) Get(id string) (*model.Character, bool) {
	m.mu.RLock()
	character, ok := m.characters[id]
	m.mu.RUnlock()
	return character, ok
}

func (m *Manager) Default() (*model.Character, bool) {
	characters := m.List()
	if len(characters) == 0 {
		return nil, false
	}
	return characters[0], true
}

func (m *Manager) RenderSystemPrompt(ctx context.Context, id, username string) (string, error) {
	character, ok := m.Get(id)
	if !ok {
		return "", errs.CharacterNotFound(id)
	}
	definitionYAML, err := yaml.Marshal(character.Definition)
	if err != nil {
		return "", err
	}
	values := make(map[string]any, len(character.Definition)+4)
	for key, value := range character.Definition {
		values[key] = value
	}
	values["Username"] = username
	values["CharacterID"] = character.ID
	values["Character"] = character.Definition
	values["CharacterYAML"] = string(definitionYAML)

	message, err := renderTemplate(ctx, m.systemTemplate, "system", values)
	if err != nil {
		return "", err
	}
	return message.Content, nil
}

func renderTemplate(ctx context.Context, template *prompt.DefaultChatTemplate, name string, values map[string]any) (*schema.Message, error) {
	messages, err := template.Format(ctx, values)
	if err != nil {
		return nil, err
	}
	if len(messages) != 1 {
		return nil, errs.PromptTemplateNoMessage(name)
	}
	return messages[0], nil
}

func (m *Manager) Providers(ctx context.Context) ([]model.Provider, error) {
	var records []model.Provider
	err := m.db.WithContext(ctx).
		Order("built_in DESC").
		Order("created_at ASC").
		Find(&records).Error
	return records, err
}

func (m *Manager) AddRemote(ctx context.Context, id, url, branch string) error {
	id = strings.TrimSpace(id)
	if id == "" || id == localProviderID || !providerIDPattern.MatchString(id) || strings.Contains(id, "..") {
		return errs.ErrInvalidProviderID
	}
	url, err := normalizeRemoteURL(url)
	if err != nil {
		return err
	}
	record := model.Provider{
		ID:      id,
		Kind:    model.ProviderRemote,
		URL:     url,
		Branch:  branch,
		Path:    utils.GetProviderPath(m.cfg.CWD, id, url, branch),
		Enabled: true,
	}
	if err := m.db.WithContext(ctx).Create(&record).Error; err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) UpdateRemote(ctx context.Context, id, url, branch string) error {
	var record model.Provider
	if err := m.db.WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		return err
	}
	if record.Kind != model.ProviderRemote {
		return errs.ErrRemoteOnlyUpdate
	}
	url, err := normalizeRemoteURL(url)
	if err != nil {
		return err
	}
	updates := map[string]any{
		"url":    url,
		"branch": strings.TrimSpace(branch),
		"path":   utils.GetProviderPath(m.cfg.CWD, id, url, branch),
	}
	if err := m.db.WithContext(ctx).Model(&record).Updates(updates).Error; err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) Remove(ctx context.Context, id string) error {
	var record model.Provider
	if err := m.db.WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		return err
	}
	if record.BuiltIn {
		return errs.ErrBuiltInProviderProtected
	}
	if err := m.db.WithContext(ctx).Delete(&record).Error; err != nil {
		return err
	}
	return m.Reload(ctx)
}

func normalizeRemoteURL(url string) (string, error) {
	url = utils.NormalizeGitRepoURL(url)
	if url == "" {
		return "", errs.ErrRemoteURLEmpty
	}
	return url, nil
}

func providerFromRecord(record model.Provider) (Provider, error) {
	switch record.Kind {
	case model.ProviderLocal:
		return NewLocalProvider(record.ID, record.Path), nil
	case model.ProviderRemote:
		if record.URL == "" {
			return nil, errs.ErrRemoteURLEmpty
		}
		return NewRemoteProvider(record.ID, record.URL, record.Branch, record.Path), nil
	default:
		return nil, errs.UnsupportedProviderKind(string(record.Kind))
	}
}
