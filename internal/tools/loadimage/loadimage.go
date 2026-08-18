// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

// Package loadimage provides an enhanced tool that exposes a remote image to the model.
package loadimage

import (
	"context"
	"net/url"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	toolName        = "load_image"
	toolDescription = `读取并理解一张远程图片。图片会以多模态内容提供给模型，不会由机器人服务器下载；URL 必须是 Telegram 或模型服务可访问的 HTTP 或 HTTPS 地址。`
)

type input struct {
	URL string `json:"url" jsonschema:"required" jsonschema_description:"图片的 HTTP 或 HTTPS URL"`
}

// Register adds the enhanced load_image tool.
func Register(manager *toolmanager.Manager) error {
	loadImageTool, err := toolutils.InferEnhancedTool(toolName, toolDescription,
		func(_ context.Context, input *input) (*schema.ToolResult, error) {
			imageURL, err := validateInput(input)
			if err != nil {
				return nil, err
			}
			return &schema.ToolResult{
				Parts: []schema.ToolOutputPart{{
					Type:  schema.ToolPartTypeImage,
					Image: &schema.ToolOutputImage{MessagePartCommon: schema.MessagePartCommon{URL: &imageURL}},
				}},
			}, nil
		})
	if err != nil {
		return err
	}
	return manager.RegisterBuiltin(toolName, loadImageTool)
}

func validateInput(input *input) (string, error) {
	if input == nil {
		return "", errs.ErrImageURLRequired
	}
	imageURL := strings.TrimSpace(input.URL)
	if imageURL == "" {
		return "", errs.ErrImageURLRequired
	}
	parsed, err := url.ParseRequestURI(imageURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errs.ErrImageURLInvalid
	}
	return imageURL, nil
}
