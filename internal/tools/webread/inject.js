/*
 * SPDX-FileCopyrightText: 2026 chhongzh <szchzcn@gmail.com>
 * SPDX-License-Identifier: MIT
 */

(async () => {
    const documentCloned = document.cloneNode(true)
    const reader = new Readability(documentCloned)

    return reader.parse()
})
