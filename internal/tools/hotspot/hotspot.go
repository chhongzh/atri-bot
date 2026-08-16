// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package hotspot

import (
	"context"
	"strings"

	"github.com/antchfx/htmlquery"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"resty.dev/v3"
)

const (
	toolName        = "get_hotspot"
	toolDescription = `获取百度热搜榜当前的热门话题。返回每个话题的标题与来源 URL；只有用户明确要求时才展示 URL。`
)

type config struct {
}

type input struct {
}

type hotspotInfo struct {
	Title string `json:"title"`
	Link  string `json:"link"`
}
type result struct {
	Baidu []*hotspotInfo `json:"baidu"`
}

func tool(ctx context.Context, runningState *toolmanager.RunningState, cfg *config, input *input, logger *zap.Logger, restyClient *resty.Client) (*result, error) {
	c := runningState.TelebotContext

	logger.Info("发起热点查询",
		utils.ExpandUserFields(c)...,
	)

	resp, err := restyClient.R().
		SetContext(ctx).
		SetHeader("Referer", "https://www.baidu.com/").
		Get("https://top.baidu.com/board?platform=pc&sa=pcindex_entry")
	if err != nil {
		return nil, errors.Wrap(err, "failed to create network request")
	}

	documents, err := htmlquery.Parse(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse html")
	}
	node, err := htmlquery.Query(documents, "//*[@id=\"sanRoot\"]/main/div[1]/div[1]/div[2]")
	if err != nil {
		return nil, errors.Wrap(err, "failed to find SanRoot")
	}
	texts, err := htmlquery.QueryAll(node, "//div[@class='c-single-text-ellipsis']")
	if err != nil {
		return nil, errors.Wrap(err, "failed to find text nodes")
	}
	var baiduHotspots []*hotspotInfo
	for _, text := range texts {
		title := strings.TrimSpace(text.FirstChild.Data)
		link := "no data"
		if text.Parent != nil && text.Parent.Parent != nil && text.Parent.Parent.Parent != nil && text.Parent.Parent.Parent.Parent != nil && len(text.Parent.Parent.Parent.Parent.Attr) > 0 {
			for _, attr := range text.Parent.Parent.Parent.Parent.Attr {
				if attr.Key == "href" {
					link = attr.Val
					break
				}
			}
		}

		baiduHotspots = append(baiduHotspots, &hotspotInfo{
			title, link,
		})
	}

	return &result{
		baiduHotspots,
	}, nil
}

// BindedRegister 使用闭包，捕获一些变量
func BindedRegister(logger *zap.Logger, restyClient *resty.Client) func(manager *toolmanager.Manager) error {
	logger = logger.Named("hotspot tool")
	fn := func(manager *toolmanager.Manager) error {
		return manager.Register(toolName, toolDescription, config{},
			func(ctx context.Context, runningState *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
				return tool(ctx, runningState, cfg, input, logger, restyClient)
			})
	}
	return fn
}
