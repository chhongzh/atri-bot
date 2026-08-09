package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrRunningStateMissing = errors.New("tool running state is missing")
	ErrToolNotFound        = errors.New("tool not found")
	ErrConfigPathNotFound  = errors.New("tool config path not found")
)

var configPathPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(?:\.(?:[A-Za-z_][A-Za-z0-9_-]*|[0-9]+))*$`)

type ConfiguredFunc[C, I, O any] func(
	ctx context.Context,
	state *RunningState,
	config *C,
	input *I,
) (*O, error)

type Registrar func(*Manager) error

type registeredTool struct {
	tool          tool.BaseTool
	configType    reflect.Type
	defaultConfig []byte
}

type Manager struct {
	db     *gorm.DB
	logger *zap.Logger

	mu           sync.RWMutex
	registered   map[string]*registeredTool
	order        []string
	builtins     map[string]tool.BaseTool
	builtinOrder []string
}

func New(db *gorm.DB, logger *zap.Logger) *Manager {
	return &Manager{
		db:         db,
		logger:     logger,
		registered: make(map[string]*registeredTool),
		builtins:   make(map[string]tool.BaseTool),
	}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&ConfigRecord{})
}

func (m *Manager) RegisterAll(registrars ...Registrar) error {
	for _, registrar := range registrars {
		if registrar == nil {
			continue
		}
		if err := registrar(m); err != nil {
			return err
		}
	}
	return nil
}

func Register[C, I, O any](
	manager *Manager,
	name, description string,
	defaultConfig C,
	function ConfiguredFunc[C, I, O],
) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("invalid tool name %q", name)
	}
	configType := reflect.TypeOf((*C)(nil)).Elem()
	if configType.Kind() != reflect.Struct {
		return fmt.Errorf("tool config %s must be a struct", configType)
	}
	defaultJSON, err := json.Marshal(defaultConfig)
	if err != nil {
		return fmt.Errorf("marshal default config for %s: %w", name, err)
	}

	inferred, err := toolutils.InferTool(name, description, func(ctx context.Context, input *I) (*O, error) {
		state, ok := RunningStateFromContext(ctx)
		if !ok {
			return nil, ErrRunningStateMissing
		}
		configValue, loadErr := manager.loadConfig(ctx, state.UserID, name, configType, defaultJSON)
		if loadErr != nil {
			return nil, loadErr
		}
		config, ok := configValue.Interface().(*C)
		if !ok {
			return nil, fmt.Errorf("unexpected config type for tool %s", name)
		}
		return function(ctx, state, config, input)
	})
	if err != nil {
		return err
	}

	manager.mu.Lock()
	defer manager.mu.Unlock()
	if _, exists := manager.registered[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	if _, exists := manager.builtins[name]; exists {
		return fmt.Errorf("tool %q already registered as builtin", name)
	}
	manager.registered[name] = &registeredTool{
		tool:          inferred,
		configType:    configType,
		defaultConfig: defaultJSON,
	}
	manager.order = append(manager.order, name)
	return nil
}

func (m *Manager) RegisterBuiltin(name string, builtin tool.BaseTool) error {
	name = strings.TrimSpace(name)
	if name == "" || builtin == nil {
		return fmt.Errorf("invalid builtin tool %q", name)
	}
	info, err := builtin.Info(context.Background())
	if err != nil {
		return fmt.Errorf("read builtin tool %q info: %w", name, err)
	}
	declaredName := ""
	if info != nil {
		declaredName = info.Name
	}
	if declaredName != name {
		return fmt.Errorf("builtin tool name mismatch: registered %q, declared %q", name, declaredName)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.registered[name]; exists {
		return fmt.Errorf("tool %q already registered as configurable", name)
	}
	if _, exists := m.builtins[name]; exists {
		return fmt.Errorf("builtin tool %q already registered", name)
	}
	m.builtins[name] = builtin
	m.builtinOrder = append(m.builtinOrder, name)
	return nil
}

func (m *Manager) Tools() []tool.BaseTool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]tool.BaseTool, 0, len(m.order)+len(m.builtinOrder))
	for _, name := range m.order {
		result = append(result, m.registered[name].tool)
	}
	for _, name := range m.builtinOrder {
		result = append(result, m.builtins[name])
	}
	return result
}

func (m *Manager) Names() []string {
	m.mu.RLock()
	names := append([]string(nil), m.order...)
	m.mu.RUnlock()
	return names
}

func (m *Manager) SetConfig(ctx context.Context, userID int64, name string, value []byte) error {
	m.mu.RLock()
	registered, ok := m.registered[name]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	normalized, err := validateAndNormalizeConfig(name, registered.configType, value)
	if err != nil {
		return err
	}
	record := ConfigRecord{UserID: userID, ToolName: name, Config: string(normalized)}
	if err = m.db.WithContext(ctx).Save(&record).Error; err != nil {
		return err
	}
	m.logger.Info("updated tool config", zap.Int64("user_id", userID), zap.String("tool", name))
	return nil
}

func (m *Manager) ConfigValue(ctx context.Context, userID int64, name, path string) (any, error) {
	path, err := validateConfigPath(path)
	if err != nil {
		return nil, err
	}
	data, err := m.ConfigJSON(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	value := gjson.GetBytes(data, path)
	if !value.Exists() {
		return nil, fmt.Errorf("%w: %s.%s", ErrConfigPathNotFound, name, path)
	}
	return value.Value(), nil
}

func (m *Manager) SetConfigValue(ctx context.Context, userID int64, name, path string, value any) (any, error) {
	path, err := validateConfigPath(path)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("tool config value is required")
	}
	data, err := m.ConfigJSON(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	if !gjson.GetBytes(data, path).Exists() {
		return nil, fmt.Errorf("%w: %s.%s", ErrConfigPathNotFound, name, path)
	}
	updated, err := sjson.SetBytes(data, path, value)
	if err != nil {
		return nil, fmt.Errorf("update config path %s.%s: %w", name, path, err)
	}
	if err = m.SetConfig(ctx, userID, name, updated); err != nil {
		return nil, err
	}
	return m.ConfigValue(ctx, userID, name, path)
}

func (m *Manager) ConfigJSON(ctx context.Context, userID int64, name string) ([]byte, error) {
	m.mu.RLock()
	registered, ok := m.registered[name]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	var record ConfigRecord
	err := m.db.WithContext(ctx).
		Where("user_id = ? AND tool_name = ?", userID, name).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		record = ConfigRecord{UserID: userID, ToolName: name, Config: string(registered.defaultConfig)}
		if createErr := m.db.WithContext(ctx).Save(&record).Error; createErr != nil {
			return nil, createErr
		}
		return append([]byte(nil), registered.defaultConfig...), nil
	}
	if err != nil {
		return nil, err
	}
	return []byte(record.Config), nil
}

func (m *Manager) loadConfig(
	ctx context.Context,
	userID int64,
	name string,
	configType reflect.Type,
	defaultConfig []byte,
) (reflect.Value, error) {
	data, err := m.ConfigJSON(ctx, userID, name)
	if errors.Is(err, ErrToolNotFound) {
		data = defaultConfig
		err = nil
	}
	if err != nil {
		return reflect.Value{}, err
	}
	value := reflect.New(configType)
	if _, err = validateAndNormalizeConfig(name, configType, data); err != nil {
		return reflect.Value{}, err
	}
	if err = json.Unmarshal(data, value.Interface()); err != nil {
		return reflect.Value{}, fmt.Errorf("decode config for %s: %w", name, err)
	}
	return value, nil
}

func validateConfigPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("tool config path is required")
	}
	if !configPathPattern.MatchString(path) {
		return "", fmt.Errorf("invalid tool config path %q", path)
	}
	return path, nil
}

func validateAndNormalizeConfig(name string, configType reflect.Type, data []byte) ([]byte, error) {
	if !json.Valid(data) {
		return nil, fmt.Errorf("invalid config for %s: malformed JSON", name)
	}
	value := reflect.New(configType)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value.Interface()); err != nil {
		return nil, fmt.Errorf("invalid config for %s: %w", name, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid config for %s: trailing JSON data", name)
	}
	normalized, err := json.Marshal(value.Interface())
	if err != nil {
		return nil, fmt.Errorf("normalize config for %s: %w", name, err)
	}
	return normalized, nil
}
