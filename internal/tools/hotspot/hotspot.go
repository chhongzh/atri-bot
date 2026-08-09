package hotspot

import (
	"context"

	"github.com/antchfx/htmlquery"
	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/utils"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"resty.dev/v3"
)

type config struct {
}

type input struct {
}

type result struct {
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
	for _, text := range texts {
		panic(123)
		_ = text.Data
	}

	return &result{}, nil
}

// BindedRegister 使用闭包，捕获一些变量
func BindedRegister(logger *zap.Logger, restyClient *resty.Client) func(manager *toolmanager.Manager) error {
	logger = logger.Named("hotspot tool")
	fn := func(manager *toolmanager.Manager) error {
		return toolmanager.Register(manager, "get_hotspot", "在网络上搜索最新热点.", config{},
			func(ctx context.Context, runningState *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
				return tool(ctx, runningState, cfg, input, logger, restyClient)
			})
	}
	return fn
}
