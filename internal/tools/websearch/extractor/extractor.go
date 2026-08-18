// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package extractor

import (
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/go-rod/rod"
)

type Result struct {
	Title     string     `json:"title"`
	Caption   string     `json:"caption"`
	URL       string     `json:"url"`
	Deeplinks []Deeplink `json:"deeplinks,omitempty"`
}

type Deeplink struct {
	Title   string `json:"title"`
	Caption string `json:"caption"`
	URL     string `json:"url"`
}

type Answer struct {
	Sum     string   `json:"sum"`
	Sources []Source `json:"sources"`
}

type Source struct {
	Title string `json:"title"`
	Site  string `json:"site"`
	URL   string `json:"url"`
}

type Extractor func(element *rod.Element) (Result, int, bool)

var extractors []Extractor

func Register(extractor Extractor) {
	extractors = append(extractors, extractor)
}

func Extract(element *rod.Element) (Result, bool) {
	var (
		bestResult Result
		bestScore  int
		found      bool
	)
	for _, extractor := range extractors {
		result, score, ok := extractor(element)
		if !ok || score <= bestScore {
			continue
		}
		bestResult, bestScore, found = result, score, true
	}
	return bestResult, found
}

type AnswerExtractor func(element *rod.Element) (Answer, int, bool)

var answerExtractors []AnswerExtractor

func RegisterAnswer(extractor AnswerExtractor) {
	answerExtractors = append(answerExtractors, extractor)
}

func ExtractAnswer(element *rod.Element) (Answer, bool) {
	answer, _, ok := extractAnswer(element)
	return answer, ok
}

func extractAnswer(element *rod.Element) (Answer, int, bool) {
	var (
		bestAnswer Answer
		bestScore  int
		found      bool
	)
	for _, extractor := range answerExtractors {
		answer, score, ok := extractor(element)
		if !ok || score <= bestScore {
			continue
		}
		bestAnswer, bestScore, found = answer, score, true
	}
	return bestAnswer, bestScore, found
}

func ExtractAnswerFromPage(page *rod.Page) (Answer, bool) {
	selectors := []string{
		"#gs_main",
		".gs_qna_fh",
		".gs_qna_fhl",
		".gs_h",
		".gs_caphead",
	}
	var (
		bestAnswer Answer
		bestScore  int
		found      bool
		seen       = make(map[string]struct{})
	)
	for _, selector := range selectors {
		elements, err := page.Elements(selector)
		if err != nil {
			continue
		}
		for _, element := range elements {
			key := ""
			if element.Object != nil {
				key = string(element.Object.ObjectID)
			}
			if key != "" {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
			}
			answer, score, ok := extractBestAnswer(element)
			if !ok || score <= bestScore {
				continue
			}
			bestAnswer, bestScore, found = answer, score, true
		}
	}
	return bestAnswer, found
}

func extractBestAnswer(element *rod.Element) (Answer, int, bool) {
	answer, score, ok := extractAnswer(element)
	if ok {
		return answer, score, true
	}

	for _, selector := range []string{"#gs_main", ".gs_qna_fh", ".gs_qna_fhl"} {
		ancestors, err := element.Parents(selector)
		if err != nil || len(ancestors) == 0 {
			continue
		}
		answer, score, ok = extractAnswer(ancestors[0])
		if ok {
			return answer, score, true
		}
	}
	return Answer{}, 0, false
}

func resolveURL(href string) string {
	if href == "" {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "www.bing.com") {
		return href
	}
	if !strings.HasPrefix(u.Path, "/ck/a") {
		return href
	}

	encoded := u.Query().Get("u")
	if encoded == "" {
		return href
	}
	encoded = strings.TrimPrefix(encoded, "a1")
	raw, decodeErr := base64.RawStdEncoding.DecodeString(strings.ReplaceAll(encoded, " ", "+"))
	if decodeErr != nil {
		raw, decodeErr = base64.StdEncoding.DecodeString(encoded)
	}
	if decodeErr != nil {
		raw, decodeErr = base64.RawURLEncoding.DecodeString(encoded)
	}
	if decodeErr != nil {
		return href
	}
	decoded := string(raw)
	if target, parseErr := url.Parse(decoded); parseErr != nil || target.Scheme == "" {
		return href
	}
	return decoded
}

func clean(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func elementText(element *rod.Element) string {
	if element == nil {
		return ""
	}
	text, err := element.Text()
	if err != nil {
		return ""
	}
	return clean(text)
}

func firstElement(element *rod.Element, selector string) *rod.Element {
	if element == nil {
		return nil
	}
	child, err := element.Element(selector)
	if err != nil {
		return nil
	}
	return child
}

func firstAttribute(element *rod.Element, name string) string {
	if element == nil {
		return ""
	}
	value, err := element.Attribute(name)
	if err != nil || value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func firstText(element *rod.Element, selectors ...string) string {
	for _, selector := range selectors {
		if text := elementText(firstElement(element, selector)); text != "" {
			return text
		}
	}
	return ""
}

func extractTitle(element *rod.Element) (string, string, bool) {
	titleElement := firstElement(element, "h2 a")
	title := elementText(titleElement)
	href := firstAttribute(titleElement, "href")
	if title == "" || href == "" {
		return "", "", false
	}
	return title, href, true
}

func extractCaption(element *rod.Element) string {
	return firstText(element, ".b_caption p", "p.b_lineclamp2", "p.b_lineclamp3", "p.b_lineclamp4")
}
