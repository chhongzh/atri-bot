package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var providerPathPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(?:\.(?:[A-Za-z_][A-Za-z0-9_-]*|[0-9]+))*$`)
var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

const (
	maxProviderNameBytes = 255
	maxProviderURLBytes  = 2048
	maxProviderJSONBytes = 64 << 10
)

func (m *Manager) List(ctx context.Context, userID int64) ([]MCPProvider, error) {
	var providers []MCPProvider
	err := m.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id ASC").
		Find(&providers).Error
	return providers, err
}

func (m *Manager) Get(ctx context.Context, userID int64, name string) (*MCPProvider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("mcp provider name is required")
	}
	var provider MCPProvider
	err := m.db.WithContext(ctx).
		Where("user_id = ? AND name = ?", userID, name).
		First(&provider).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrProviderNotFound
	}
	return &provider, err
}

func (m *Manager) Add(ctx context.Context, userID int64, name, rawURL, meta, header string) (*MCPProvider, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("mcp provider name is required")
	}
	if len(name) > maxProviderNameBytes {
		return nil, fmt.Errorf("mcp provider name exceeds %d bytes", maxProviderNameBytes)
	}
	rawURL = strings.TrimSpace(rawURL)
	if len(rawURL) > maxProviderURLBytes {
		return nil, fmt.Errorf("mcp url exceeds %d bytes", maxProviderURLBytes)
	}
	blockInternal, err := m.blockInternalFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err = validateProviderURL(rawURL, blockInternal); err != nil {
		return nil, err
	}
	if meta, err = normalizeJSONObject(meta, "meta"); err != nil {
		return nil, err
	}
	if header, err = normalizeJSONObject(header, "header"); err != nil {
		return nil, err
	}
	if _, err = parseStringMap(header, "header"); err != nil {
		return nil, err
	}
	if _, err = parseMeta(meta); err != nil {
		return nil, err
	}
	m.providersMu.Lock()
	defer m.providersMu.Unlock()
	maxTools, err := m.maxToolsFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	count, err := m.count(ctx, userID)
	if err != nil {
		return nil, err
	}
	if count >= maxTools {
		return nil, fmt.Errorf("%w: %d", ErrProviderLimit, maxTools)
	}

	provider := &MCPProvider{UserID: userID, Name: name, URL: rawURL, Meta: meta, Header: header}
	if err = m.db.WithContext(ctx).Create(provider).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, fmt.Errorf("%w: %s", ErrProviderExists, name)
		}
		return nil, err
	}
	m.logger.Info("added mcp provider",
		zap.Int64("user_id", userID),
		zap.String("provider", name),
		zap.String("url_host", urlHost(rawURL)),
	)
	m.notifyChange(userID)
	return provider, nil
}

func (m *Manager) Remove(ctx context.Context, userID int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("mcp provider name is required")
	}
	m.providersMu.Lock()
	result := m.db.WithContext(ctx).
		Where("user_id = ? AND name = ?", userID, name).
		Delete(&MCPProvider{})
	m.providersMu.Unlock()
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrProviderNotFound
	}
	m.logger.Info("removed mcp provider", zap.Int64("user_id", userID), zap.String("provider", name))
	m.notifyChange(userID)
	return nil
}

func (m *Manager) Value(ctx context.Context, userID int64, name, path string) (any, error) {
	path, err := validateProviderPath(path)
	if err != nil {
		return nil, err
	}
	provider, err := m.Get(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	data, err := providerJSON(provider)
	if err != nil {
		return nil, err
	}
	value := gjson.GetBytes(data, path)
	if !value.Exists() {
		return nil, fmt.Errorf("%w: %s.%s", ErrPathNotFound, name, path)
	}
	return value.Value(), nil
}

func (m *Manager) SetValue(ctx context.Context, userID int64, name, path string, value any) (any, error) {
	path, err := validateProviderPath(path)
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("mcp provider value is required")
	}
	if top := providerTopLevel(path); top != "url" && top != "meta" && top != "header" {
		return nil, fmt.Errorf("%w: %s", ErrPathForbidden, top)
	}
	provider, err := m.Get(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	data, err := providerJSON(provider)
	if err != nil {
		return nil, err
	}
	if top := providerTopLevel(path); top == "url" && !gjson.GetBytes(data, path).Exists() {
		return nil, fmt.Errorf("%w: %s.%s", ErrPathNotFound, name, path)
	}
	updated, err := sjson.SetBytes(data, path, value)
	if err != nil {
		return nil, fmt.Errorf("update mcp provider %s.%s: %w", name, path, err)
	}
	decoded, err := decodeProviderJSON(updated)
	if err != nil {
		return nil, err
	}
	if _, err = parseStringMap(string(decoded.Header), "header"); err != nil {
		return nil, err
	}
	if _, err = parseMeta(string(decoded.Meta)); err != nil {
		return nil, err
	}
	blockInternal, err := m.blockInternalFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err = validateProviderURL(decoded.URL, blockInternal); err != nil {
		return nil, err
	}
	provider.URL = decoded.URL
	provider.Meta = string(decoded.Meta)
	provider.Header = string(decoded.Header)
	if err = m.db.WithContext(ctx).Save(provider).Error; err != nil {
		return nil, err
	}
	m.logger.Info("updated mcp provider",
		zap.Int64("user_id", userID),
		zap.String("provider", name),
		zap.String("path", path),
	)
	value = gjson.GetBytes(updated, path).Value()
	m.notifyChange(userID)
	return value, nil
}

func (m *Manager) count(ctx context.Context, userID int64) (int, error) {
	var count int64
	err := m.db.WithContext(ctx).Model(&MCPProvider{}).Where("user_id = ?", userID).Count(&count).Error
	return int(count), err
}

func (m *Manager) maxToolsFor(ctx context.Context, userID int64) (int, error) {
	user, err := m.accounts.Get(ctx, userID)
	if err != nil {
		return 0, err
	}
	if user.MCPMaxTools > 0 {
		return user.MCPMaxTools, nil
	}
	return m.cfg.DefaultMaxTools, nil
}

func (m *Manager) blockInternalFor(ctx context.Context, userID int64) (bool, error) {
	user, err := m.accounts.Get(ctx, userID)
	if err != nil {
		return false, err
	}
	if user.MCPBlockInternal != nil {
		return *user.MCPBlockInternal, nil
	}
	return m.cfg.BlockInternal, nil
}

func providerJSON(provider *MCPProvider) ([]byte, error) {
	return json.Marshal(struct {
		Name   string          `json:"name"`
		URL    string          `json:"url"`
		Meta   json.RawMessage `json:"meta"`
		Header json.RawMessage `json:"header"`
	}{provider.Name, provider.URL, json.RawMessage(provider.Meta), json.RawMessage(provider.Header)})
}

type providerDocument struct {
	Name   string          `json:"name"`
	URL    string          `json:"url"`
	Meta   json.RawMessage `json:"meta"`
	Header json.RawMessage `json:"header"`
}

func decodeProviderJSON(data []byte) (*providerDocument, error) {
	var document providerDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode mcp provider: %w", err)
	}
	if _, err := normalizeJSONObject(string(document.Meta), "meta"); err != nil {
		return nil, err
	}
	if _, err := normalizeJSONObject(string(document.Header), "header"); err != nil {
		return nil, err
	}
	return &document, nil
}

func normalizeJSONObject(raw, field string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}", nil
	}
	if len(raw) > maxProviderJSONBytes {
		return "", fmt.Errorf("%w: %s exceeds %d bytes", ErrInvalidJSON, field, maxProviderJSONBytes)
	}
	if !json.Valid([]byte(raw)) || !gjson.Parse(raw).IsObject() {
		return "", fmt.Errorf("%w: %s must be a JSON object", ErrInvalidJSON, field)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err != nil {
		return "", fmt.Errorf("%w: %s must be valid JSON", ErrInvalidJSON, field)
	}
	return compact.String(), nil
}

func parseStringMap(raw, field string) (map[string]string, error) {
	normalized, err := normalizeJSONObject(raw, field)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	if err = json.Unmarshal([]byte(normalized), &result); err != nil {
		return nil, fmt.Errorf("%w: %s values must be strings", ErrInvalidJSON, field)
	}
	for key, value := range result {
		if !headerNamePattern.MatchString(key) || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%w: %s contains an invalid HTTP header", ErrInvalidJSON, field)
		}
	}
	return result, nil
}

func parseMeta(raw string) (*mcp.Meta, error) {
	normalized, err := normalizeJSONObject(raw, "meta")
	if err != nil {
		return nil, err
	}
	if normalized == "{}" {
		return nil, nil
	}
	var meta mcp.Meta
	if err = json.Unmarshal([]byte(normalized), &meta); err != nil {
		return nil, fmt.Errorf("%w: meta: %v", ErrInvalidJSON, err)
	}
	return &meta, nil
}

func validateProviderPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("mcp provider path is required")
	}
	if !providerPathPattern.MatchString(path) {
		return "", fmt.Errorf("invalid mcp provider path %q", path)
	}
	if len(path) > 512 {
		return "", errors.New("mcp provider path exceeds 512 bytes")
	}
	return path, nil
}

func providerTopLevel(path string) string {
	if index := strings.IndexByte(path, '.'); index >= 0 {
		return path[:index]
	}
	return path
}
