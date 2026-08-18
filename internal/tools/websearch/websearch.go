// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package websearch

import (
	"fmt"

	"github.com/chhongzh/atri-bot/internal/tools/websearch/extractor"
	"github.com/go-rod/rod"
)

type SearchResponse struct {
	Answer  *Answer            `json:"answer,omitempty"`
	Results []extractor.Result `json:"results"`
}

type Answer = extractor.Answer
type Source = extractor.Source

func Parse(page *rod.Page) (*SearchResponse, error) {
	results, err := parseResults(page)
	if err != nil {
		return nil, fmt.Errorf("parse bing search results: %w", err)
	}
	return &SearchResponse{
		Answer:  parseAnswer(page),
		Results: results,
	}, nil
}

func parseResults(page *rod.Page) ([]extractor.Result, error) {
	resultElements, err := page.Elements("#b_results > li.b_algo")
	if err != nil {
		return nil, err
	}
	if len(resultElements) == 0 {
		resultElements, err = page.Elements("li.b_algo")
		if err != nil {
			return nil, err
		}
	}
	results := make([]extractor.Result, 0, len(resultElements))
	for _, element := range resultElements {
		result, ok := extractor.Extract(element)
		if ok {
			results = append(results, result)
		}
	}
	return results, nil
}

func parseAnswer(page *rod.Page) *Answer {
	answer, ok := extractor.ExtractAnswerFromPage(page)
	if !ok {
		return nil
	}
	return &answer
}
