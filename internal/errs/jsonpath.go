// SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
// SPDX-License-Identifier: MIT

package errs

import "fmt"

// JSONPathRequired reports a missing JSON path subject.
func JSONPathRequired(subject string) error {
	return fmt.Errorf("%s is required", subject)
}

// JSONPathInvalid reports a malformed JSON path.
func JSONPathInvalid(subject, path string) error {
	return fmt.Errorf("invalid %s %q", subject, path)
}

// JSONPathTooLong reports a JSON path exceeding the byte limit.
func JSONPathTooLong(subject string, maxBytes int) error {
	return fmt.Errorf("%s exceeds %d bytes", subject, maxBytes)
}
