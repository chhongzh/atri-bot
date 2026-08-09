package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	einomcp "github.com/cloudwego/eino-ext/components/tool/mcp"
	"github.com/cloudwego/eino/components/tool"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	mcpprotocol "github.com/mark3labs/mcp-go/mcp"
	"go.uber.org/zap"
)

const (
	clientName     = "atri-bot"
	clientVersion  = "1.0.0"
	connectTimeout = 30 * time.Second
)

type providerLoadJob struct {
	ctx           context.Context
	index         int
	userID        int64
	provider      MCPProvider
	blockInternal bool
	result        chan<- providerLoadResult
}

type providerLoadResult struct {
	index int
	tools []tool.BaseTool
	close func()
	err   error
}

func (m *Manager) worker(workerID int) {
	defer m.workersWG.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case job := <-m.jobs:
			m.runProviderJob(workerID, job)
		}
	}
}

func (m *Manager) runProviderJob(workerID int, job providerLoadJob) {
	loaded, closeFn, err := m.loadProvider(job.ctx, job.userID, &job.provider, job.blockInternal)
	if err == nil && job.ctx.Err() != nil {
		if closeFn != nil {
			closeFn()
		}
		loaded = nil
		closeFn = nil
		err = job.ctx.Err()
	}
	job.result <- providerLoadResult{
		index: job.index,
		tools: loaded,
		close: closeFn,
		err:   err,
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Warn("failed to load mcp provider",
			zap.Int("worker", workerID),
			zap.Int64("user_id", job.userID),
			zap.String("provider", job.provider.Name),
			zap.String("url_host", urlHost(job.provider.URL)),
			zap.Error(err),
		)
	}
}

func (m *Manager) loadUserTools(
	ctx context.Context,
	userID int64,
	gate func(context.Context) (bool, error),
	callback func(*LoadResult, error),
) {
	if gate != nil {
		allowed, err := gate(ctx)
		if err != nil {
			m.logger.Warn("mcp gate check failed", zap.Int64("user_id", userID), zap.Error(err))
			callback(nil, err)
			return
		}
		if !allowed {
			m.logger.Info("mcp loading skipped: permission denied", zap.Int64("user_id", userID))
			callback(nil, nil)
			return
		}
	}

	providers, err := m.List(ctx, userID)
	if err != nil {
		callback(nil, err)
		return
	}
	maxTools, err := m.maxToolsFor(ctx, userID)
	if err != nil {
		callback(nil, err)
		return
	}
	blockInternal, err := m.blockInternalFor(ctx, userID)
	if err != nil {
		callback(nil, err)
		return
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
		callback(&LoadResult{}, nil)
		return
	}

	m.logger.Info("loading mcp providers",
		zap.Int64("user_id", userID),
		zap.Int("providers", len(providers)),
	)
	results := make(chan providerLoadResult, len(providers))
	queued := 0
	for i := range providers {
		job := providerLoadJob{
			ctx:           ctx,
			index:         i,
			userID:        userID,
			provider:      providers[i],
			blockInternal: blockInternal,
			result:        results,
		}
		select {
		case m.jobs <- job:
			queued++
		case <-ctx.Done():
			callback(nil, ctx.Err())
			return
		}
	}

	loadedProviders := make([]providerLoadResult, queued)
	for completed := 0; completed < queued; completed++ {
		select {
		case result := <-results:
			loadedProviders[result.index] = result
		case <-ctx.Done():
			for i := range loadedProviders {
				if loadedProviders[i].close != nil {
					loadedProviders[i].close()
				}
			}
			callback(nil, ctx.Err())
			return
		}
	}
	loadResult := &LoadResult{}
	failed := 0
	for i := range loadedProviders {
		if loadedProviders[i].err != nil {
			failed++
			continue
		}
		loadResult.Tools = append(loadResult.Tools, loadedProviders[i].tools...)
		if loadedProviders[i].close != nil {
			loadResult.closers = append(loadResult.closers, loadedProviders[i].close)
		}
	}
	if ctx.Err() != nil {
		loadResult.Close()
		callback(nil, ctx.Err())
		return
	}
	m.logger.Info("finished loading mcp providers",
		zap.Int64("user_id", userID),
		zap.Int("providers", queued),
		zap.Int("failed_providers", failed),
		zap.Int("tools", len(loadResult.Tools)),
	)
	callback(loadResult, nil)
}

func (m *Manager) loadProvider(
	ctx context.Context,
	userID int64,
	provider *MCPProvider,
	blockInternal bool,
) ([]tool.BaseTool, func(), error) {
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := validateProviderURLContext(connectCtx, provider.URL, blockInternal); err != nil {
		return nil, nil, err
	}
	headers, err := parseStringMap(provider.Header, "header")
	if err != nil {
		return nil, nil, err
	}
	meta, err := parseMeta(provider.Meta)
	if err != nil {
		return nil, nil, err
	}

	httpClient, closeHTTPClient := newMCPHTTPClient(blockInternal)
	mcpClient, err := client.NewSSEMCPClient(
		provider.URL,
		client.WithHeaders(headers),
		client.WithHTTPClient(httpClient),
		transport.WithEndpointTimeout(connectTimeout),
	)
	if err != nil {
		closeHTTPClient()
		return nil, nil, fmt.Errorf("create sse mcp client: %w", err)
	}
	if err = mcpClient.Start(ctx); err != nil {
		_ = mcpClient.Close()
		closeHTTPClient()
		return nil, nil, fmt.Errorf("start sse mcp client: %w", err)
	}
	initRequest := mcpprotocol.InitializeRequest{}
	initRequest.Params.ProtocolVersion = mcpprotocol.LATEST_PROTOCOL_VERSION
	initRequest.Params.ClientInfo = mcpprotocol.Implementation{Name: clientName, Version: clientVersion}
	if _, err = mcpClient.Initialize(connectCtx, initRequest); err != nil {
		_ = mcpClient.Close()
		closeHTTPClient()
		return nil, nil, fmt.Errorf("initialize mcp client: %w", err)
	}
	rawTools, err := einomcp.GetTools(connectCtx, &einomcp.Config{
		Cli:           mcpClient,
		CustomHeaders: headers,
		Meta:          meta,
	})
	if err != nil {
		_ = mcpClient.Close()
		closeHTTPClient()
		return nil, nil, fmt.Errorf("list mcp tools: %w", err)
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
				zap.Error(closeErr),
			)
		}
	}
	m.logger.Info("loaded mcp provider",
		zap.Int64("user_id", userID),
		zap.String("provider", provider.Name),
		zap.String("url_host", urlHost(provider.URL)),
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
