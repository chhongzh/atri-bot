// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

// Package sendimage provides a tool for sending an image to the active Telegram chat.
package sendimage

import (
	"context"
	"net/url"
	"strings"

	"github.com/chhongzh/atri-bot/internal/errs"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	pkgErrors "github.com/pkg/errors"
	"gopkg.in/telebot.v4"
)

const (
	toolName        = "send_image"
	toolDescription = `向当前用户发送一张图片。需要提供可由 Telegram 访问的 HTTP 或 HTTPS 图片 URL，可选附带图片说明。`
)

type config struct{}

type input struct {
	URL     string `json:"url" jsonschema:"required" jsonschema_description:"图片的 HTTP 或 HTTPS URL"`
	Caption string `json:"caption,omitempty" jsonschema_description:"图片的可选说明"`
}

type result struct {
	Delivered bool `json:"delivered"`
}

func tool(_ context.Context, state *toolmanager.RunningState, _ *config, input *input) (*result, error) {
	imageURL, err := validateInput(input)
	if err != nil {
		return nil, err
	}
	if state == nil || state.TelebotContext == nil {
		return nil, errs.ErrTelebotContextMissing
	}

	photo := &telebot.Photo{
		File:    telebot.FromURL(imageURL),
		Caption: strings.TrimSpace(input.Caption),
	}
	if err := state.TelebotContext.Send(photo); err != nil {
		return nil, pkgErrors.Wrap(err, "send image")
	}
	return &result{Delivered: true}, nil
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

// Register adds the send_image tool.
func Register(manager *toolmanager.Manager) error {
	return manager.Register(toolName, toolDescription, config{}, tool)
}
