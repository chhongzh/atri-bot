package webread

import (
	"context"
	_ "embed"
	"time"

	toolmanager "github.com/chhongzh/atri-bot/internal/tools"
	"github.com/chhongzh/atri-bot/internal/tools/webread/stealth"
	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	toolName        = "web_read"
	toolDescription = `Read and extract the main textual content from a web page so it can be summarized or analyzed.`
)

//go:embed inject.js
var injectScript string

//go:embed Readability.js
var readabilityScript string

type config struct {
}

type input struct {
	URL string `json:"url" jsonschema:"required" jsonschema_description:"web网站的URL"`
}

type result struct {
	Title         string `json:"title"`
	TextContent   string `json:"textContent"`
	Excerpt       string `json:"excerpt"`
	Byline        string `json:"byline"`
	SiteName      string `json:"siteName"`
	PublishedTime string `json:"publishedTime"`
}

func tool(ctx context.Context, runningState *toolmanager.RunningState, cfg *config, input *input, logger *zap.Logger, browser *rod.Browser) (*result, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*300)
	defer cancel()

	page, err := browser.Context(ctx).Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, errors.Wrap(err, "failed to open blank page")
	}
	defer func(page *rod.Page) {
		err := page.Close()
		if err != nil {
			logger.Warn("failed to close page, this may cause leakage!", zap.Error(err))
		}
	}(page)

	remove, err := stealth.Inject(page)
	if err != nil {
		return nil, errors.Wrap(err, "failed to install stealth script")
	}
	defer func(remove func() error) {
		err := remove()
		if err != nil {
			logger.Warn("failed to remove eval", zap.Error(err))
		}
	}(remove)

	err = page.Navigate(input.URL)
	if err != nil {
		return nil, errors.Wrap(err, "failed to navigate to URL")
	}

	err = page.WaitDOMStable(time.Millisecond*500, 0.75)
	if err != nil {
		return nil, errors.Wrap(err, "failed to wait for dom stable")
	}

	// inject readability
	evalResult, err := page.Evaluate(rod.Eval(injectScript).ByPromise())
	if err != nil {
		return nil, errors.Wrap(err, "failed to evaluate result")
	}
	articleObj := evalResult.Value

	title := articleObj.Get("title").String()
	textContent := articleObj.Get("textContent").String()
	excerpt := articleObj.Get("excerpt").String()
	byline := articleObj.Get("byline").String()
	siteName := articleObj.Get("siteName").String()
	publishedTime := articleObj.Get("publishedTime").String()

	return &result{
		title, textContent, excerpt, byline, siteName, publishedTime,
	}, nil
}

func BindedRegister(logger *zap.Logger, browser *rod.Browser) func(manager *toolmanager.Manager) error {
	logger = logger.Named("webread tool")
	fn := func(manager *toolmanager.Manager) error {
		return toolmanager.Register(manager, toolName, toolDescription, config{},
			func(ctx context.Context, runningState *toolmanager.RunningState, cfg *config, input *input) (*result, error) {
				return tool(ctx, runningState, cfg, input, logger, browser)
			})
	}
	return fn
}
