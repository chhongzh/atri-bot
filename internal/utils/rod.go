// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package utils

import (
	"context"
	"errors"
	"fmt"

	"github.com/chhongzh/atri-bot/internal/stealth"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

func NewIncognitoPage(ctx context.Context, browser *rod.Browser) (*rod.Page, func() error, error) {
	incognito, err := browser.Incognito()
	if err != nil {
		return nil, nil, fmt.Errorf("create incognito browser context: %w", err)
	}

	page, err := incognito.Page(proto.TargetCreateTarget{})
	if err != nil {
		_ = incognito.Close()
		return nil, nil, fmt.Errorf("create incognito page: %w", err)
	}
	page = page.Context(ctx)

	closePage := func() error {
		return errors.Join(page.Close(), incognito.Close())
	}
	return page, closePage, nil
}

func InjectStealth(page *rod.Page) (func() error, error) {
	return stealth.Inject(page)
}
