// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"sort"
	"strconv"
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
		comment := identity.DisplayText(id.Comment)
		if comment == "" {
			comment = "(no comment)"
		}
		size := identity.DisplayBitSize(id.Blob, id.Algorithm)
		options = append(options, identityOption{
			index: index, identity: id, comment: comment, algorithm: identity.DisplayText(id.Algorithm),
			size: size, fingerprint: identity.DisplayText(id.Fingerprint),
		})
	}
	return options
}

func makeSearchableIdentityOptions(identities []identity.Identity) []identityOption {
	options := makeIdentityOptions(identities)
	for index := range options {
		option := &options[index]
		number := strconv.Itoa(index + 1)
		capacity := utf8.RuneCountInString(number) + utf8.RuneCountInString(option.comment) +
			utf8.RuneCountInString(option.algorithm) + utf8.RuneCountInString(option.size) +
			utf8.RuneCountInString(option.fingerprint) + 4
		searchRunes := make([]rune, 0, capacity)
		searchRunes = appendLowerRunes(searchRunes, number)
		searchRunes = append(searchRunes, ' ')
		searchRunes = appendLowerRunes(searchRunes, option.comment)
		searchRunes = append(searchRunes, ' ')
		searchRunes = appendLowerRunes(searchRunes, option.algorithm)
		searchRunes = append(searchRunes, ' ')
		searchRunes = appendLowerRunes(searchRunes, option.size)
		searchRunes = append(searchRunes, ' ')
		option.searchRunes = appendLowerRunes(searchRunes, option.fingerprint)
	}
	return options
}

// matchIdentities applies the same case-insensitive fuzzy filter in both picker views.
func matchIdentities(options []identityOption, query string) []identityMatch {
	queryRunes := lowerRunes(query)
	capacity := min(len(options), 32)
	if len(queryRunes) == 0 {
		capacity = len(options)
	}
	matches := make([]identityMatch, 0, capacity)
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

func lowerRunes(value string) []rune {
	runes := make([]rune, 0, utf8.RuneCountInString(value))
	return appendLowerRunes(runes, value)
}

func appendLowerRunes(destination []rune, value string) []rune {
	for _, r := range value {
		destination = append(destination, unicode.ToLower(r))
	}
	return destination
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
	for count := max(1, len(options)); count > 0; count /= 10 {
		no++
	}
	no++
	comment, keyType, size, fingerprint = len("Comment"), len("Type"), len("Size"), len("Fingerprint")
	for _, option := range options {
		comment = max(comment, utf8.RuneCountInString(option.comment))
		keyType = max(keyType, utf8.RuneCountInString(option.algorithm))
		size = max(size, utf8.RuneCountInString(option.size))
		fingerprint = max(fingerprint, utf8.RuneCountInString(option.fingerprint))
	}
	comment = min(comment, 36)
	keyType = min(keyType, 64)
	fingerprint = min(fingerprint, 50)
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
	// Scan only to the column boundary instead of allocating a rune slice for a large comment.
	count, end := 0, 0
	for offset := range value {
		if count == width {
			return value[:end] + "…"
		}
		end = offset
		count++
	}
	return value
}
