// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jfut/ssh-keyselect/internal/identity"
)

// identityOption is the shared row model used by each selector view and the CLI table.
type identityOption struct {
	index       int
	identity    identity.Identity
	comment     string
	algorithm   string
	size        string
	fingerprint string
	// searchRunes is only read by the matcher in this file.
	searchRunes []rune
}

// identityMatch pairs a visible identity row with its fuzzy-search score.
type identityMatch struct {
	identityOption
	// score is used only to order matches here.
	score int
}

// makeIdentityOptions prepares display rows from SSH identities.
func makeIdentityOptions(identities []identity.Identity) []identityOption {
	options := make([]identityOption, 0, len(identities))
	for index, id := range identities {
		comment := identity.DisplayComment(id.Comment)
		if comment == "" {
			comment = "(no comment)"
		}
		size := identity.DisplayBitSize(id.Blob, id.Algorithm)
		options = append(options, identityOption{
			index: index, identity: id, comment: comment, algorithm: id.Algorithm,
			size: size, fingerprint: id.Fingerprint,
		})
	}
	return options
}

func makeSearchableIdentityOptions(identities []identity.Identity) []identityOption {
	options := makeIdentityOptions(identities)
	for index := range options {
		option := &options[index]
		searchText := strconv.Itoa(index+1) + " " + option.comment + " " + option.algorithm + " " + option.size + " " + option.fingerprint
		option.searchRunes = []rune(strings.ToLower(searchText))
	}
	return options
}

// matchIdentities applies the same case-insensitive fuzzy filter in both picker views.
func matchIdentities(options []identityOption, query string) []identityMatch {
	queryRunes := []rune(strings.ToLower(query))
	matches := make([]identityMatch, 0, len(options))
	if len(queryRunes) == 0 {
		for _, option := range options {
			matches = append(matches, identityMatch{identityOption: option})
		}
		return matches
	}
	for _, option := range options {
		score, matched := identityFuzzyScore(queryRunes, option.searchRunes)
		if matched {
			matches = append(matches, identityMatch{identityOption: option, score: score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	return matches
}

func identityFuzzyScore(queryRunes, candidateRunes []rune) (int, bool) {
	queryIndex := 0
	lastMatch := -2
	score := 0
	for index, r := range candidateRunes {
		if r != queryRunes[queryIndex] {
			continue
		}
		score += 10
		if index == 0 || !unicode.IsLetter(candidateRunes[index-1]) && !unicode.IsDigit(candidateRunes[index-1]) {
			score += 8
		}
		if index == lastMatch+1 {
			score += 12
		} else if lastMatch >= 0 {
			score -= index - lastMatch - 1
		}
		lastMatch = index
		queryIndex++
		if queryIndex == len(queryRunes) {
			return score - len(candidateRunes)/100, true
		}
	}
	return 0, false
}

// identityColumnWidths fits shared identity row columns to the available width.
func identityColumnWidths(options []identityOption, terminalWidth int) (no, comment, keyType, size, fingerprint int) {
	no = len(fmt.Sprintf("%d", max(1, len(options)))) + 1
	comment, keyType, size, fingerprint = len("Comment"), len("Type"), len("Size"), len("Fingerprint")
	for _, option := range options {
		comment = max(comment, utf8.RuneCountInString(option.comment))
		keyType = max(keyType, utf8.RuneCountInString(option.algorithm))
		size = max(size, utf8.RuneCountInString(option.size))
		fingerprint = max(fingerprint, utf8.RuneCountInString(option.fingerprint))
	}
	comment = min(comment, 36)
	size = max(size, 4)
	fullWidth := no + comment + keyType + size + fingerprint + 16
	if terminalWidth <= 0 || fullWidth <= terminalWidth {
		return
	}
	const minimumFingerprintWidth = 20
	comment = max(8, min(comment, terminalWidth-16-no-keyType-size-minimumFingerprintWidth))
	fingerprint = max(minimumFingerprintWidth, terminalWidth-16-no-keyType-size-comment)
	return
}

// fitIdentityCell truncates display text without splitting UTF-8 characters.
func fitIdentityCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
}
