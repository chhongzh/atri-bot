// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/chhongzh/atri-bot/internal/model"
	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpprotocol "github.com/mark3labs/mcp-go/mcp"
	pkgErrors "github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	clientName     = "atri-bot"
	clientVersion  = "1.0.0"
	connectTimeout = 30 * time.Second
)

type providerLoadResult struct {
	tools []tool.BaseTool
	close func()
	err   error
}

func (m *Manager) loadUserTools(
	ctx context.Context,
	userID int64,
	gate func(context.Context) (bool, error),
) (*LoadResult, error) {
	allowed, err := gate(ctx)
	if err != nil {
		m.logger.Warn("mcp gate check failed", zap.Int64("user_id", userID), zap.Error(err))
		return nil, err
	}
	if !allowed {
		m.logger.Info("mcp loading skipped: permission denied", zap.Int64("user_id", userID))
		return &LoadResult{}, nil
	}

	providers, err := m.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	maxTools, err := m.maxToolsFor(ctx, userID)
	if err != nil {
		return nil, err
	}
	if len(providers) > maxTools {
		m.logger.Warn("mcp provider limit exceeded; extra providers skipped",
			zap.Int64("user_id", userID),
			zap.Int("providers", len(providers)),
			zap.Int("limit", maxTools),
		)
		providers = providers[:maxTools]
	}
	if len(providers) == 0 {
		return &LoadResult{}, nil
	}

	m.logger.Info("loading mcp providers",
		zap.Int64("user_id", userID),
		zap.Int("providers", len(providers)),
	)
	loadedProviders := make([]providerLoadResult, len(providers))
	var waitGroup sync.WaitGroup
	for i := range providers {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			provider := &providers[index]
			loaded, closeFn, loadErr := m.loadProvider(ctx, userID, provider)
			if loadErr == nil && ctx.Err() != nil {
				closeFn()
				loaded = nil
				loadErr = ctx.Err()
			}
			loadedProviders[index] = providerLoadResult{tools: loaded, close: closeFn, err: loadErr}
			if loadErr != nil && !errors.Is(loadErr, context.Canceled) {
				m.logger.Warn("failed to load mcp provider",
					zap.Int64("user_id", userID),
					zap.String("provider", provider.Name),
					zap.String("url_host", urlHost(provider.URL)),
					zap.Error(loadErr),
				)
			}
		}(i)
	}
	waitGroup.Wait()

	loadResult := &LoadResult{}
	failed := 0
	for i := range loadedProviders {
		if loadedProviders[i].err != nil {
			failed++
			continue
		}
		loadResult.Tools = append(loadResult.Tools, loadedProviders[i].tools...)
		loadResult.closers = append(loadResult.closers, loadedProviders[i].close)
	}
	if ctx.Err() != nil {
		loadResult.Close()
		return nil, ctx.Err()
	}
	m.logger.Info("finished loading mcp providers",
		zap.Int64("user_id", userID),
		zap.Int("providers", len(providers)),
		zap.Int("failed_providers", failed),
		zap.Int("tools", len(loadResult.Tools)),
	)
	return loadResult, nil
}

func (m *Manager) loadProvider(
	ctx context.Context,
	userID int64,
	provider *model.MCPProvider,
) ([]tool.BaseTool, func(), error) {
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	headers, err := parseStringMap(provider.Header, "header")
	if err != nil {
		return nil, nil, err
	}
	meta, err := parseMeta(provider.Meta)
	if err != nil {
		return nil, nil, err
	}

	loaded, closeFn, err := m.loadProviderWithTransport(
		ctx,
		connectCtx,
		userID,
		provider,
		headers,
		meta,
		"streamable_http",
		func(httpClient *http.Client) (*client.Client, error) {
			return client.NewStreamableHttpClient(
				provider.URL,
				transport.WithHTTPBasicClient(httpClient),
				transport.WithHTTPHeaders(headers),
			)
		},
	)
	if err == nil {
		return loaded, closeFn, nil
	}
	if !errors.Is(err, transport.ErrLegacySSEServer) {
		return nil, nil, err
	}
	streamableErr := err
	m.logger.Debug("mcp provider requires legacy sse transport",
		zap.Int64("user_id", userID),
		zap.String("provider", provider.Name),
		zap.String("url_host", urlHost(provider.URL)),
	)
	loaded, closeFn, err = m.loadProviderWithTransport(
		ctx,
		connectCtx,
		userID,
		provider,
		headers,
		meta,
		"sse",
		func(httpClient *http.Client) (*client.Client, error) {
			return client.NewSSEMCPClient(
				provider.URL,
				client.WithHeaders(headers),
				client.WithHTTPClient(httpClient),
				transport.WithEndpointTimeout(connectTimeout),
			)
		},
	)
	if err != nil {
		return nil, nil, errors.Join(
			pkgErrors.Wrap(streamableErr, "streamable http"),
			pkgErrors.Wrap(err, "legacy sse"),
		)
	}
	return loaded, closeFn, nil
}

func (m *Manager) loadProviderWithTransport(
	ctx context.Context,
	connectCtx context.Context,
	userID int64,
	provider *model.MCPProvider,
	headers map[string]string,
	meta *mcpprotocol.Meta,
	transportName string,
	newClient func(*http.Client) (*client.Client, error),
) ([]tool.BaseTool, func(), error) {
	httpClient, closeHTTPClient := newMCPHTTPClient(m.allowPrivateIP)
	mcpClient, err := newClient(httpClient)
	if err != nil {
		closeHTTPClient()
		return nil, nil, pkgErrors.Wrapf(err, "create %s mcp client", transportName)
	}
	if err = mcpClient.Start(ctx); err != nil {
		_ = mcpClient.Close()
		closeHTTPClient()
		return nil, nil, pkgErrors.Wrapf(err, "start %s mcp client", transportName)
	}
	initRequest := mcpprotocol.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcpprotocol.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcpprotocol.Implementation{Name: clientName, Version: clientVersion}
	if _, err = mcpClient.Initialize(connectCtx, initRequest); err != nil {
		_ = mcpClient.Close()
		closeHTTPClient()
		return nil, nil, pkgErrors.Wrapf(err, "initialize %s mcp client", transportName)
	}
	rawTools, err := einomcp.GetTools(connectCtx, &einomcp.Config{
		Cli:           mcpClient,
		CustomHeaders: headers,
		Meta:          meta,
	})
	if err != nil {
		_ = mcpClient.Close()
		closeHTTPClient()
		return nil, nil, pkgErrors.Wrapf(err, "list %s mcp tools", transportName)
	}

	audited := make([]tool.BaseTool, 0, len(rawTools))
	for _, remoteTool := range rawTools {
		audited = append(audited, wrapAuditTool(m.logger, userID, provider, remoteTool))
	}
	closeFn := func() {
		defer closeHTTPClient()
		if closeErr := mcpClient.Close(); closeErr != nil {
			m.logger.Warn("failed to close mcp client",
				zap.Int64("user_id", userID),
				zap.String("provider", provider.Name),
				zap.String("url_host", urlHost(provider.URL)),
				zap.String("transport", transportName),
				zap.Error(closeErr),
			)
		}
	}
	m.logger.Info("loaded mcp provider",
		zap.Int64("user_id", userID),
		zap.String("provider", provider.Name),
		zap.String("url_host", urlHost(provider.URL)),
		zap.String("transport", transportName),
		zap.Int("tools", len(audited)),
	)
	return audited, closeFn, nil
}

func urlHost(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return parsed.Host
}
