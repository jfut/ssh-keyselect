// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jfut/ssh-keyselect/internal/identity"
)

// FormatIdentityTable renders public identity metadata as a Markdown table.
func FormatIdentityTable(identities []identity.Identity) string {
	options := makeIdentityOptions(identities)
	for index := range options {
		// Escape Markdown's cell separator so comments cannot break the table.
		options[index].comment = strings.ReplaceAll(options[index].comment, "|", `\|`)
	}

	noWidth, commentWidth, typeWidth, sizeWidth, fingerprintWidth := identityColumnWidths(options, 0)
	for _, option := range options {
		commentWidth = max(commentWidth, utf8.RuneCountInString(option.comment))
	}

	var table strings.Builder
	writeRow := func(number, comment, keyType, size, fingerprint string) {
		writeCell := func(value string, width int, rightAlign bool) {
			table.WriteByte(' ')
			cell := fitIdentityCell(value, width)
			if rightAlign {
				table.WriteString(strings.Repeat(" ", max(0, width-utf8.RuneCountInString(cell))))
			}
			table.WriteString(cell)
			if !rightAlign {
				table.WriteString(strings.Repeat(" ", max(0, width-utf8.RuneCountInString(cell))))
			}
			table.WriteString(" |")
		}
		table.WriteByte('|')
		writeCell(number, noWidth, false)
		writeCell(comment, commentWidth, false)
		writeCell(keyType, typeWidth, false)
		writeCell(size, sizeWidth, true)
		writeCell(fingerprint, fingerprintWidth, false)
		table.WriteByte('\n')
	}

	writeRow("No", "Comment", "Type", "Size", "Fingerprint")
	fmt.Fprintf(&table, "|%s|%s|%s|%s|%s|\n",
		strings.Repeat("-", noWidth+2), strings.Repeat("-", commentWidth+2),
		strings.Repeat("-", typeWidth+2), strings.Repeat("-", sizeWidth+2),
		strings.Repeat("-", fingerprintWidth+2))
	for index, option := range options {
		writeRow(fmt.Sprintf("%d", index+1), option.comment, option.algorithm, option.size, option.fingerprint)
	}
	return table.String()
}
