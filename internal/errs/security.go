// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import (
	"errors"
	"fmt"
)

var ErrPrivateAddressBlocked = errors.New("network address points to an internal or private network")

// NoAddressesResolved reports a host that resolved to no addresses.
func NoAddressesResolved(subject string) error {
	return fmt.Errorf("%s resolved to no addresses", subject)
}
