// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package extractor

import "github.com/go-rod/rod"

func init() {
	Register(extractDeeplinks)
}

// extractDeeplinks 处理带站点深链的变体（li.b_algo.b_vtl_deeplinks），
// 除了标题、描述和 URL，还会提取 .b_vlist2col.b_deep 下的深链子项。
func extractDeeplinks(element *rod.Element) (Result, int, bool) {
	if _, err := element.Element(".b_vlist2col.b_deep"); err != nil {
		return Result{}, 0, false
	}
	title, href, ok := extractTitle(element)
	if !ok {
		return Result{}, 0, false
	}
	result := Result{
		Title:   title,
		Caption: extractCaption(element),
		URL:     resolveURL(href),
	}
	deeplinkItems, err := element.Elements(".b_vlist2col.b_deep ul li")
	if err != nil {
		return Result{}, 0, false
	}
	for _, item := range deeplinkItems {
		link := firstElement(item, "h3.deeplink_title a")
		href := firstAttribute(link, "href")
		if href == "" {
			continue
		}
		deeplink := Deeplink{
			Title:   elementText(link),
			Caption: firstText(item, "p"),
			URL:     resolveURL(href),
		}
		if deeplink.Title == "" {
			continue
		}
		result.Deeplinks = append(result.Deeplinks, deeplink)
	}
	score := scoreDeeplinks
	if result.Caption != "" {
		score++
	}
	if len(result.Deeplinks) > 0 {
		score++
	}
	return result, score, true
}
