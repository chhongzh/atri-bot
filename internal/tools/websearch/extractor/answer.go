// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package extractor

import (
	"net/url"
	"strings"

	"github.com/go-rod/rod"
)

func init() {
	RegisterAnswer(extractGeneratedAnswer)
}

func extractGeneratedAnswer(element *rod.Element) (Answer, int, bool) {
	sum := collectAnswerText(element)
	sources := collectSources(element)
	if sum == "" && len(sources) == 0 {
		return Answer{}, 0, false
	}
	score := 1
	if sum != "" {
		score += 10
	}
	if len(sources) > 0 {
		score += 5
	}
	return Answer{Sum: sum, Sources: sources}, score, true
}

func collectAnswerText(element *rod.Element) string {
	selectors := []string{
		".gs_h",
		".gs_caphead",
		".gs_text",
		".gs_heroTextHeader",
		".rwrl_sec",
		".rwrl",
	}
	var texts []string
	seen := make(map[string]struct{})
	for _, selector := range selectors {
		elements, err := element.Elements(selector)
		if err != nil {
			continue
		}
		for _, item := range elements {
			text := elementText(item)
			if text == "" {
				continue
			}
			if _, ok := seen[text]; ok {
				continue
			}
			seen[text] = struct{}{}
			texts = append(texts, text)
		}
	}
	if len(texts) == 0 {
		for _, selector := range []string{".gs_h", ".gs_caphead"} {
			if elementTextValue := firstText(element, selector); elementTextValue != "" {
				texts = append(texts, elementTextValue)
			}
		}
	}
	if len(texts) == 0 {
		className := firstAttribute(element, "class")
		if strings.Contains(className, "gs_h") || strings.Contains(className, "gs_caphead") {
			if text := elementText(element); text != "" {
				texts = append(texts, text)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func collectSources(element *rod.Element) []Source {
	var sources []Source
	seen := make(map[string]struct{})
	appendSource := func(source Source) {
		if source.Title == "" && source.URL == "" {
			return
		}
		key := source.URL
		if key == "" {
			key = source.Title
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		sources = append(sources, source)
	}

	citations, err := element.Elements(".gs_cit")
	if err == nil {
		for _, citation := range citations {
			title := firstAttribute(citation, "data-title")
			if title == "" {
				title = firstText(citation, ".title", "h3", "a")
			}
			site := firstAttribute(citation, "data-displayname")
			if site == "" {
				site = firstText(citation, ".site", "cite")
			}
			citationURL := firstAttribute(citation, "data-url")
			if citationURL == "" {
				citationURL = firstAttribute(firstElement(citation, "a"), "href")
			}
			appendSource(Source{
				Title: title,
				Site:  site,
				URL:   resolveURL(citationURL),
			})
		}
	}

	answerResults, err := element.Elements(".rwrl_cred .b_algo")
	if err == nil {
		for _, answerResult := range answerResults {
			result, ok := Extract(answerResult)
			if !ok {
				continue
			}
			site := firstText(answerResult, ".b_attribution cite")
			if site == "" {
				site = siteFromURL(result.URL)
			}
			appendSource(Source{Title: result.Title, Site: site, URL: result.URL})
		}
	}

	cards, err := element.Elements(".racard")
	if err == nil {
		for _, card := range cards {
			link := firstElement(card, "a")
			appendSource(Source{
				Title: firstText(card, ".rainfo .title", ".title"),
				Site:  firstText(card, ".rainfo .site", ".site"),
				URL:   resolveURL(firstAttribute(link, "href")),
			})
		}
	}
	return sources
}

func siteFromURL(rawURL string) string {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(parsedURL.Hostname(), "www.")
}
