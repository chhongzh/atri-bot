// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package websearch

import (
	"context"
	"strings"
	"time"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/tools/websearch/extractor"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/go-rod/rod"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	toolName        = "web_search"
	toolDescription = `使用 Bing 搜索网页，返回生成式回答与结果列表。

支持 Bing 高级搜索语法：

| 语法 | 作用 | 示例 |
| --- | --- | --- |
| contains: | 搜索包含指向指定文件类型链接的网站 | music contains:wma |
| ext: | 仅返回指定文件扩展名的网页 | report ext:docx |
| filetype: | 仅返回指定文件类型的网页 | report filetype:pdf |
| inanchor:、inbody:、intitle: | 分别限定定位点、正文或标题中的术语，可组合使用 | inanchor:msn inbody:spaces inbody:magog |
| ip: | 搜索由指定 IPv4 地址托管的站点 | ip:192.0.2.1 |
| language: | 限定网页语言代码 | antiques language:en |
| loc:、location: | 限定国家或地区代码，可用 OR 组合 | sculpture (loc:US OR loc:GB) |
| prefer: | 为搜索词或其他运算符添加偏好 | football prefer:organization |
| site: | 限定网站、域名或目录，可用 OR 组合 | heart disease (site:bbc.co.uk OR site:cnn.com) |
| feed: | 搜索包含指定词的 RSS 或 Atom 源 | feed:football |
| hasfeed: | 搜索包含指定词且提供 RSS 或 Atom 源的网页 | site:nytimes.com hasfeed:football |
| url: | 检查域名或网址是否已编入 Bing 索引 | url:microsoft.com |
`
)

type config struct {
}

type input struct {
	Query string `json:"query" jsonschema:"required" jsonschema_description:"搜索关键词"`
}

type result struct {
	Answer  *Answer            `json:"answer,omitempty"`
	Results []extractor.Result `json:"results"`
}

func tool(ctx context.Context, runningState *toolmanager.RunningState, cfg *config, in *input, logger *zap.Logger, browser *rod.Browser) (out *result, err error) {
	startedAt := time.Now()
	logger = logger.With(utils.ExpandTelebotContext(runningState.TelebotContext)...)

	defer func() {
		fields := []zap.Field{
			zap.String("query", in.Query),
			zap.Duration("duration", time.Since(startedAt)),
		}
		if err != nil {
			logger.Error("web search failed", append(fields, zap.Error(err))...)
			return
		}
		logger.Info("web search completed", append(fields, zap.Int("results", len(out.Results)))...)
	}()

	ctx, cancel := context.WithTimeout(ctx, time.Second*30)
	defer cancel()

	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, errors.New("search query is empty")
	}

	resp, err := SearchWithBrowser(ctx, browser, query)
	if err != nil {
		return nil, errors.Wrap(err, "bing web search failed")
	}
	return &result{
		Answer:  resp.Answer,
		Results: resp.Results,
	}, nil
}

// BindedRegister 使用闭包捕获 logger 与浏览器连接。
func BindedRegister(logger *zap.Logger, browser *rod.Browser) func(manager *toolmanager.Manager) error {
	logger = logger.Named("websearch tool")
	fn := func(manager *toolmanager.Manager) error {
		return manager.Register(toolName, toolDescription, config{},
			func(ctx context.Context, runningState *toolmanager.RunningState, cfg *config, in *input) (*result, error) {
				return tool(ctx, runningState, cfg, in, logger, browser)
			})
	}
	return fn
}
