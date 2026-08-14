package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"

	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/chhongzh/atri-bot/internal/utils"
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

	mu                 sync.RWMutex
	registered         map[string]*registeredTool
	order              []string
	builtins           map[string]tool.BaseTool
	builtinOrder       []string
	permissionByTool   map[string]string
	virtualPermissions map[string]struct{}
	permissionOrder    []string
}

func New(db *gorm.DB, logger *zap.Logger) *Manager {
	return &Manager{
		db:                 db,
		logger:             logger,
		registered:         make(map[string]*registeredTool),
		builtins:           make(map[string]tool.BaseTool),
		permissionByTool:   make(map[string]string),
		virtualPermissions: make(map[string]struct{}),
	}
}

func (m *Manager) Init() error {
	return m.db.AutoMigrate(&model.ToolConfig{})
}

func (m *Manager) RegisterAll(registrars ...Registrar) error {
	for _, registrar := range registrars {
		if err := registrar(m); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Register[C, I, O any](
	name, description string,
	defaultConfig C,
	function ConfiguredFunc[C, I, O],
) error {
	name = strings.TrimSpace(name)
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
		configValue, loadErr := m.loadConfig(ctx, state.UserID, name, configType, defaultJSON)
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

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.registered[name]; exists {
		return fmt.Errorf("tool %q already registered", name)
	}
	if _, exists := m.builtins[name]; exists {
		return fmt.Errorf("tool %q already registered as builtin", name)
	}
	if _, exists := m.virtualPermissions[name]; exists {
		return fmt.Errorf("tool %q conflicts with a permission-only capability", name)
	}
	m.registered[name] = &registeredTool{
		tool:          inferred,
		configType:    configType,
		defaultConfig: defaultJSON,
	}
	m.order = append(m.order, name)
	m.permissionByTool[name] = name
	m.permissionOrder = append(m.permissionOrder, name)
	return nil
}

func (m *Manager) RegisterBuiltin(name string, builtin tool.BaseTool) error {
	return m.registerBuiltin(name, name, builtin)
}

// RegisterBuiltinWithPermission registers a callable builtin under a shared
// permission. This is used for capabilities such as MCP where one permission
// must hide every related management tool as a group.
func (m *Manager) RegisterBuiltinWithPermission(name, permission string, builtin tool.BaseTool) error {
	permission = strings.TrimSpace(permission)
	return m.registerBuiltin(name, permission, builtin)
}

func (m *Manager) registerBuiltin(name, permission string, builtin tool.BaseTool) error {
	name = strings.TrimSpace(name)
	info, err := builtin.Info(context.Background())
	if err != nil {
		return fmt.Errorf("read builtin tool %q info: %w", name, err)
	}
	declaredName := info.Name
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
	if permission == name {
		if _, exists := m.virtualPermissions[name]; exists {
			return fmt.Errorf("builtin tool %q conflicts with a permission-only capability", name)
		}
	}
	if permission != name {
		if _, exists := m.virtualPermissions[permission]; !exists {
			return fmt.Errorf("tool permission %q is not registered", permission)
		}
	}
	m.builtins[name] = builtin
	m.builtinOrder = append(m.builtinOrder, name)
	m.permissionByTool[name] = permission
	if permission == name {
		m.permissionOrder = append(m.permissionOrder, name)
	}
	return nil
}

// RegisterPermission registers a permission-only capability. It appears in
// administrative permission controls but is never exposed to the model as a
// callable tool.
func (m *Manager) RegisterPermission(name string) error {
	name = strings.TrimSpace(name)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.virtualPermissions[name]; exists {
		return fmt.Errorf("tool permission %q already registered", name)
	}
	if _, exists := m.permissionByTool[name]; exists {
		return fmt.Errorf("tool permission %q conflicts with a tool", name)
	}
	m.virtualPermissions[name] = struct{}{}
	m.permissionOrder = append(m.permissionOrder, name)
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

// PermissionNames returns the independently controllable permission names.
// Tools sharing a permission are intentionally omitted as separate controls.
func (m *Manager) PermissionNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]string(nil), m.permissionOrder...)
}

// PermissionName returns the permission that controls a callable tool.
func (m *Manager) PermissionName(toolName string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	permission, ok := m.permissionByTool[strings.TrimSpace(toolName)]
	return permission, ok
}

// HasPermission reports whether name is an independently controllable tool
// permission, including permission-only capabilities.
func (m *Manager) HasPermission(name string) bool {
	name = strings.TrimSpace(name)
	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.virtualPermissions[name]; ok {
		return true
	}
	permission, ok := m.permissionByTool[name]
	return ok && permission == name
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
	record := model.ToolConfig{UserID: userID, ToolName: name, Config: string(normalized)}
	if err = m.db.WithContext(ctx).Save(&record).Error; err != nil {
		return err
	}
	m.logger.Info("updated tool config", zap.Int64("user_id", userID), zap.String("tool", name))
	return nil
}

func (m *Manager) ConfigValue(ctx context.Context, userID int64, name, path string) (any, error) {
	path, err := utils.ValidateJSONPath(path, "tool config path", 0)
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
	path, err := utils.ValidateJSONPath(path, "tool config path", 0)
	if err != nil {
		return nil, err
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
	var record model.ToolConfig
	err := m.db.WithContext(ctx).
		Where("user_id = ? AND tool_name = ?", userID, name).
		First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		record = model.ToolConfig{UserID: userID, ToolName: name, Config: string(registered.defaultConfig)}
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
