package character

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/cloudwego/eino/components/prompt"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

const localProviderID = "local"

var providerIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

//go:embed system.j2
var systemTemplate string

//go:embed user.j2
var userTemplate string

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
	userTemplate   *prompt.DefaultChatTemplate
}

func New(db *gorm.DB, logger *zap.Logger, cfg Config) *Manager {
	return &Manager{
		db:             db,
		logger:         logger,
		cfg:            cfg,
		characters:     make(map[string]*model.Character),
		systemTemplate: prompt.FromMessages(schema.Jinja2, schema.SystemMessage(systemTemplate)),
		userTemplate:   prompt.FromMessages(schema.Jinja2, schema.UserMessage(userTemplate)),
	}
}

func (m *Manager) Init(ctx context.Context) error {
	if err := m.db.AutoMigrate(&model.ProviderRecord{}); err != nil {
		return err
	}
	localRoot := filepath.Join(m.cfg.CWD, "chardefs")
	local := model.ProviderRecord{
		ID:      localProviderID,
		Kind:    model.ProviderLocal,
		Path:    localRoot,
		BuiltIn: true,
		Enabled: true,
	}
	if err := m.db.WithContext(ctx).Where("id = ?", local.ID).Assign(local).FirstOrCreate(&local).Error; err != nil {
		return err
	}
	if strings.TrimSpace(m.cfg.RemoteURL) != "" {
		remote := model.ProviderRecord{
			ID:      "remote-default",
			Kind:    model.ProviderRemote,
			URL:     utils.GitNormalizeRepoURL(m.cfg.RemoteURL),
			Branch:  m.cfg.RemoteBranch,
			Path:    utils.ProviderGetPath(m.cfg.CWD, "remote-default", m.cfg.RemoteURL, m.cfg.RemoteBranch),
			BuiltIn: true,
			Enabled: true,
		}
		if err := m.db.WithContext(ctx).Where("id = ?", remote.ID).FirstOrCreate(&remote).Error; err != nil {
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
		return "", fmt.Errorf("character %q not found", id)
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

	messages, err := m.systemTemplate.Format(ctx, values)
	if err != nil {
		return "", err
	}
	if len(messages) != 1 {
		return "", errors.New("system prompt template returned no message")
	}
	return messages[0].Content, nil
}

func (m *Manager) RenderUserMessage(ctx context.Context, text string, now time.Time) (*schema.Message, error) {
	messages, err := m.userTemplate.Format(ctx, map[string]any{
		"Time":        now.Format(time.RFC3339),
		"UserMessage": text,
	})
	if err != nil {
		return nil, err
	}
	if len(messages) != 1 {
		return nil, errors.New("user prompt template returned no message")
	}
	return messages[0], nil
}

func (m *Manager) Providers(ctx context.Context) ([]model.ProviderRecord, error) {
	var records []model.ProviderRecord
	err := m.db.WithContext(ctx).
		Order("built_in DESC").
		Order("created_at ASC").
		Find(&records).Error
	return records, err
}

func (m *Manager) AddRemote(ctx context.Context, id, url, branch string) error {
	id = strings.TrimSpace(id)
	if id == "" || id == localProviderID || !providerIDPattern.MatchString(id) || strings.Contains(id, "..") {
		return errors.New("invalid provider id")
	}
	url = utils.GitNormalizeRepoURL(url)
	if url == "" {
		return errors.New("remote url cannot be empty")
	}
	record := model.ProviderRecord{
		ID:      id,
		Kind:    model.ProviderRemote,
		URL:     url,
		Branch:  branch,
		Path:    utils.ProviderGetPath(m.cfg.CWD, id, url, branch),
		Enabled: true,
	}
	if err := m.db.WithContext(ctx).Create(&record).Error; err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) UpdateRemote(ctx context.Context, id, url, branch string) error {
	var record model.ProviderRecord
	if err := m.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		return err
	}
	if record.Kind != model.ProviderRemote {
		return errors.New("only remote providers can be updated")
	}
	url = utils.GitNormalizeRepoURL(url)
	if url == "" {
		return errors.New("remote url cannot be empty")
	}
	updates := map[string]any{
		"url":    url,
		"branch": strings.TrimSpace(branch),
		"path":   utils.ProviderGetPath(m.cfg.CWD, id, url, branch),
	}
	if err := m.db.WithContext(ctx).Model(&record).Updates(updates).Error; err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) Remove(ctx context.Context, id string) error {
	var record model.ProviderRecord
	if err := m.db.WithContext(ctx).First(&record, "id = ?", id).Error; err != nil {
		return err
	}
	if record.BuiltIn {
		return errors.New("built-in provider cannot be removed")
	}
	if err := m.db.WithContext(ctx).Delete(&record).Error; err != nil {
		return err
	}
	return m.Reload(ctx)
}

func providerFromRecord(record model.ProviderRecord) (Provider, error) {
	switch record.Kind {
	case model.ProviderLocal:
		return NewLocalProvider(record.ID, record.Path), nil
	case model.ProviderRemote:
		if record.URL == "" {
			return nil, errors.New("remote url is empty")
		}
		return NewRemoteProvider(record.ID, record.URL, record.Branch, record.Path), nil
	default:
		return nil, fmt.Errorf("unsupported provider kind %q", record.Kind)
	}
}
