// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package email

import (
	"context"
	"errors"
	"testing"

	"github.com/chhongzh/atri-bot/internal/security"
)

func TestMailToolBlocksPrivateSMTPHost(t *testing.T) {
	_, err := mailTool(context.Background(), &config{
		SmtpHost:    "127.0.0.1",
		SmtpPort:    25,
		FromAddress: "sender@example.com",
	}, &input{
		Subject: "subject",
		Html:    "body",
		To:      []string{"recipient@example.com"},
	}, false)
	if !errors.Is(err, security.ErrPrivateAddressBlocked) {
		t.Fatalf("mailTool() error = %v, want ErrPrivateAddressBlocked", err)
	}
}
