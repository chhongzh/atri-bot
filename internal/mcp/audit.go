package mcp

import (
	"context"
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// auditTool wraps a remote MCP tool and logs every invocation so that remote
// requests can be audited. Only the tool name, provider and host are logged;
// arguments and results are never written to the log.
type auditTool struct {
	info         *schema.ToolInfo
	inner        tool.InvokableTool
	logger       *zap.Logger
	userID       int64
	providerName string
	urlHost      string
	remoteName   string
}

var invalidToolName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func wrapAuditTool(
	logger *zap.Logger,
	userID int64,
	provider *MCPProvider,
	base tool.BaseTool,
) tool.BaseTool {
	invokable, ok := base.(tool.InvokableTool)
	if !ok {
		return base
	}
	info, err := base.Info(context.Background())
	if err != nil || info == nil {
		return base
	}
	clonedInfo := *info
	clonedInfo.Name = exposedToolName(provider, info.Name)
	return &auditTool{
		info:         &clonedInfo,
		inner:        invokable,
		logger:       logger,
		userID:       userID,
		providerName: provider.Name,
		urlHost:      urlHost(provider.URL),
		remoteName:   info.Name,
	}
}

func (t *auditTool) Info(_ context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *auditTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	toolName := t.info.Name
	t.logger.Info("mcp tool call started",
		zap.Int64("user_id", t.userID),
		zap.String("provider", t.providerName),
		zap.String("url_host", t.urlHost),
		zap.String("tool", toolName),
		zap.String("remote_tool", t.remoteName),
	)
	startedAt := time.Now()
	result, err := t.inner.InvokableRun(ctx, argumentsInJSON, opts...)
	if err != nil {
		t.logger.Warn("mcp tool call failed",
			zap.Int64("user_id", t.userID),
			zap.String("provider", t.providerName),
			zap.String("url_host", t.urlHost),
			zap.String("tool", toolName),
			zap.String("remote_tool", t.remoteName),
			zap.Duration("duration", time.Since(startedAt)),
			zap.Error(err),
		)
		return result, err
	}
	t.logger.Info("mcp tool call completed",
		zap.Int64("user_id", t.userID),
		zap.String("provider", t.providerName),
		zap.String("url_host", t.urlHost),
		zap.String("tool", toolName),
		zap.String("remote_tool", t.remoteName),
		zap.Duration("duration", time.Since(startedAt)),
		zap.Int("result_bytes", len(result)),
	)
	return result, nil
}

func exposedToolName(provider *MCPProvider, remoteName string) string {
	providerName := strings.Trim(invalidToolName.ReplaceAllString(provider.Name, "_"), "_")
	remoteName = strings.Trim(invalidToolName.ReplaceAllString(remoteName, "_"), "_")
	if providerName == "" {
		providerName = "provider"
	}
	if remoteName == "" {
		remoteName = "tool"
	}
	name := fmt.Sprintf("mcp_%d_%s_%s", provider.ID, providerName, remoteName)
	if len(name) <= 64 {
		return name
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	suffix := fmt.Sprintf("_%08x", hash.Sum32())
	return name[:64-len(suffix)] + suffix
}

var _ tool.BaseTool = (*auditTool)(nil)
var _ tool.InvokableTool = (*auditTool)(nil)
