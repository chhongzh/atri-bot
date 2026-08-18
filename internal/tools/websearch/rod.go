// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package websearch

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/go-rod/rod"
)

// SearchWithBrowser 通过 Chrome 调试端口以无痕模式渲染 Bing 搜索页面，
// 等待结果与生成式回答渲染稳定后解析。只关闭本标签页（defer page.Close），
// 不会关闭外部浏览器。
func SearchWithBrowser(ctx context.Context, browser *rod.Browser, query string) (*SearchResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	page, closePage, err := utils.NewIncognitoPage(ctx, browser)
	if err != nil {
		return nil, fmt.Errorf("open incognito page: %w", err)
	}
	defer func() { _ = closePage() }()

	if _, err := utils.InjectStealth(page); err != nil {
		return nil, fmt.Errorf("inject stealth script: %w", err)
	}

	if err := page.Navigate(buildSearchURL(query)); err != nil {
		return nil, fmt.Errorf("navigate to bing: %w", err)
	}
	// 等待结果出现；DOM 稳定后再等待生成式回答渲染
	if err := page.Context(ctx).WaitElementsMoreThan("li.b_algo", 0); err != nil {
		return nil, fmt.Errorf("wait for search results: %w", err)
	}
	_ = page.WaitDOMStable(time.Second, 0.9)
	waitAnswer(ctx, page)
	_ = page.WaitDOMStable(time.Second, 0.9)

	return Parse(page)
}

// waitAnswer 最多等 8 秒让生成式回答卡片渲染完成，超时不报错（回答可有可无）。
func waitAnswer(ctx context.Context, page *rod.Page) {
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	for {
		for _, selector := range []string{"#gs_main", ".gs_qna_fh", ".gs_qna_fhl", ".gs_h", ".gs_caphead"} {
			has, _, err := page.Has(selector)
			if err == nil && has {
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// buildSearchURL 构造与浏览器行为一致的搜索 URL。
func buildSearchURL(query string) string {
	u := &url.URL{Scheme: "https", Host: "www.bing.com", Path: "/search"}
	q := u.Query()
	q.Set("q", query)
	q.Set("go", "搜索")
	q.Set("qs", "ds")
	q.Set("form", "QBRE")
	q.Set("mkt", "zh-CN")
	u.RawQuery = q.Encode()
	return u.String()
}
