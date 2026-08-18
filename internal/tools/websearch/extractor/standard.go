// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package extractor

import "github.com/go-rod/rod"

// 各 extractor 的基础匹配度：变体越具体得分越高。
const (
	scoreStandard  = 10
	scoreDeeplinks = 20
)

func init() {
	Register(extractStandard)
}

// extractStandard 处理普通结果：h2 a 标题 + 描述，匹配所有带标题的结果。
func extractStandard(element *rod.Element) (Result, int, bool) {
	title, href, ok := extractTitle(element)
	if !ok {
		return Result{}, 0, false
	}
	result := Result{
		Title:   title,
		Caption: extractCaption(element),
		URL:     resolveURL(href),
	}
	score := scoreStandard
	if result.Caption != "" {
		score++
	}
	return result, score, true
}
