// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package sendimage

import (
	"errors"
	"testing"

	"github.com/chhongzh/atri-bot/internal/errs"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name    string
		input   *input
		wantURL string
		wantErr error
	}{
		{name: "valid HTTPS URL", input: &input{URL: " https://example.com/image.png "}, wantURL: "https://example.com/image.png"},
		{name: "missing input", wantErr: errs.ErrImageURLRequired},
		{name: "missing URL", input: &input{}, wantErr: errs.ErrImageURLRequired},
		{name: "unsupported scheme", input: &input{URL: "file:///tmp/image.png"}, wantErr: errs.ErrImageURLInvalid},
		{name: "missing host", input: &input{URL: "https:/image.png"}, wantErr: errs.ErrImageURLInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, err := validateInput(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("validateInput() error = %v, want %v", err, tt.wantErr)
			}
			if gotURL != tt.wantURL {
				t.Fatalf("validateInput() URL = %q, want %q", gotURL, tt.wantURL)
			}
		})
	}
}
