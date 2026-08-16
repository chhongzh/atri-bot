// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	configmanager "github.com/chhongzh/atri-bot/internal/config"
	"github.com/chhongzh/atri-bot/internal/errs"
	"github.com/chhongzh/atri-bot/internal/model"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/mark3labs/mcp-go/mcp"
	pkgErrors "github.com/pkg/errors"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var headerNamePattern = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

const (
	maxProviderNameBytes = 255
	maxProviderURLBytes  = 2048
	maxProviderJSONBytes = 64 << 10
)

func (m *Manager) List(ctx context.Context, userID int64) ([]model.MCPProvider, error) {
	var providers []model.MCPProvider
	err := m.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("id ASC").
		Find(&providers).Error
	return providers, err
}

func (m *Manager) Get(ctx context.Context, userID int64, name string) (*model.MCPProvider, error) {
	name = strings.TrimSpace(name)
	var provider model.MCPProvider
	err := m.db.WithContext(ctx).
		Where("user_id = ? AND name = ?", userID, name).
		First(&provider).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.ErrProviderNotFound
	}
	return &provider, err
}

func (m *Manager) Add(ctx context.Context, userID int64, name, rawURL, meta, header string) (*model.MCPProvider, error) {
	var err error
	name = strings.TrimSpace(name)
	if len(name) > maxProviderNameBytes {
		return nil, errs.MCPProviderNameTooLong(maxProviderNameBytes)
	}
	rawURL = strings.TrimSpace(rawURL)
	if len(rawURL) > maxProviderURLBytes {
		return nil, errs.MCPURLTooLong(maxProviderURLBytes)
	}
	if err := validateProviderURL(rawURL, m.allowPrivateIP); err != nil {
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
		return nil, errs.MCPProviderLimit(maxTools)
	}

	provider := &model.MCPProvider{UserID: userID, Name: name, URL: rawURL, Meta: meta, Header: header}
	if err = m.db.WithContext(ctx).Create(provider).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, errs.MCPProviderExists(name)
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
	m.providersMu.Lock()
	result := m.db.WithContext(ctx).
		Where("user_id = ? AND name = ?", userID, name).
		Delete(&model.MCPProvider{})
	m.providersMu.Unlock()
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errs.ErrProviderNotFound
	}
	m.logger.Info("removed mcp provider", zap.Int64("user_id", userID), zap.String("provider", name))
	m.notifyChange(userID)
	return nil
}

func (m *Manager) Value(ctx context.Context, userID int64, name, path string) (any, error) {
	path, err := utils.ValidateJSONPath(path, "mcp provider path", 512)
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
		return nil, errs.MCPPathNotFound(name, path)
	}
	return value.Value(), nil
}

func (m *Manager) SetValue(ctx context.Context, userID int64, name, path string, value any) (any, error) {
	path, err := utils.ValidateJSONPath(path, "mcp provider path", 512)
	if err != nil {
		return nil, err
	}
	if top := providerTopLevel(path); top != "url" && top != "meta" && top != "header" {
		return nil, errs.MCPPathForbidden(top)
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
		return nil, errs.MCPPathNotFound(name, path)
	}
	updated, err := sjson.SetBytes(data, path, value)
	if err != nil {
		return nil, pkgErrors.Wrapf(err, "update mcp provider %s.%s", name, path)
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
	if err = validateProviderURL(decoded.URL, m.allowPrivateIP); err != nil {
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
	err := m.db.WithContext(ctx).Model(&model.MCPProvider{}).Where("user_id = ?", userID).Count(&count).Error
	return int(count), err
}

func (m *Manager) maxToolsFor(ctx context.Context, userID int64) (int, error) {
	settings, err := m.accounts.Settings(ctx, userID)
	if err != nil {
		return 0, err
	}
	if settings.MCPMaxTools > 0 {
		return settings.MCPMaxTools, nil
	}
	runtime, err := m.configs.Query[configmanager.RuntimeSettings](ctx, configmanager.RuntimeSettingsKey)
	if err != nil {
		return 0, err
	}
	return runtime.MCPDefaultMaxTools, nil
}

func providerJSON(provider *model.MCPProvider) ([]byte, error) {
	return json.Marshal(providerDocument{
		Name:   provider.Name,
		URL:    provider.URL,
		Meta:   json.RawMessage(provider.Meta),
		Header: json.RawMessage(provider.Header),
	})
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
		return nil, pkgErrors.Wrap(err, "decode mcp provider")
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
		return "", errs.MCPInvalidJSONTooLarge(field, maxProviderJSONBytes)
	}
	if !json.Valid([]byte(raw)) || !gjson.Parse(raw).IsObject() {
		return "", errs.MCPInvalidJSONNotObject(field)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err != nil {
		return "", errs.MCPInvalidJSONNotValid(field)
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
		return nil, errs.MCPInvalidJSONNotStringMap(field)
	}
	for key, value := range result {
		if !headerNamePattern.MatchString(key) || strings.ContainsAny(value, "\r\n") {
			return nil, errs.MCPInvalidJSONInvalidHeader(field)
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
		return nil, errors.Join(errs.ErrInvalidJSON, err)
	}
	return &meta, nil
}

func providerTopLevel(path string) string {
	if index := strings.IndexByte(path, '.'); index >= 0 {
		return path[:index]
	}
	return path
}
