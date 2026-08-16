// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
)

var (
	ErrEmailContentEmpty          = errors.New("email content is required")
	ErrEmailBodyMissing           = errors.New("at least one of text or html body is required")
	ErrEmailNoRecipients          = errors.New("at least one recipient is required")
	ErrEmailSMTPHostEmpty         = errors.New("smtp host is required")
	ErrEmailCredentialsIncomplete = errors.New("username and password must be configured together")
)

// EmailInvalidSMTPPort reports an out-of-range SMTP port.
func EmailInvalidSMTPPort(port int) error {
	return fmt.Errorf("smtp port must be between 1 and 65535, got %d", port)
}
