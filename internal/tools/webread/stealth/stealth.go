package stealth

import (
	_ "embed"

	"github.com/go-rod/rod"
)

//go:generate go run ./generate

//go:embed stealth.min.js
var script string

func Inject(page *rod.Page) (func() error, error) {
	return page.EvalOnNewDocument(script)
}
