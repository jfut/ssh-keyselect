// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jfut/ssh-keyselect/internal/identity"
)

// TUISelector presents a serialized terminal prompt. /dev/tty keeps protocol traffic off stdin/stdout.
type TUISelector struct {
	TTYPath   string
	queue     chan struct{}
	queueOnce sync.Once
}

func NewTUISelector() *TUISelector {
	return &TUISelector{TTYPath: "/dev/tty"}
}

// Select displays verified host information and returns the highlighted identity on Enter.
func (s *TUISelector) Select(ctx context.Context, identities []identity.Identity, requestContext SelectionContext) ([]identity.Identity, error) {
	if len(identities) == 0 {
		return nil, nil
	}
	s.queueOnce.Do(func() { s.queue = make(chan struct{}, 1) })
	select {
	case s.queue <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.queue }()
	path := s.TTYPath
	if path == "" {
		path = "/dev/tty"
	}
	terminal, err := openTerminal(path)
	if err != nil {
		return nil, fmt.Errorf("open controlling terminal: %w", err)
	}
	defer func() { _ = terminal.Close() }()
	stopClose := context.AfterFunc(ctx, func() { _ = terminal.Close() })
	defer stopClose()

	options := makeSearchableIdentityOptions(identities)
	shownAt := time.Now()
	if terminal.liveEcho {
		return s.selectLive(ctx, terminal, options, requestContext, shownAt)
	}
	return s.selectLineBuffered(ctx, terminal, options, requestContext, shownAt)
}

func tuiEchoInputLine(writer io.Writer, line string) error {
	_, err := io.WriteString(writer, identity.DisplayText(line)+"\r\n")
	return err
}

type tuiInputLine struct {
	line   string
	skipLF bool
	err    error
}

type tuiByteReader interface {
	ReadByte() (byte, error)
}

// tuiSingleByteReader avoids bufio read-ahead across the prompt/SSH handoff.
type tuiSingleByteReader struct {
	reader io.Reader
}

func (r tuiSingleByteReader) ReadByte() (byte, error) {
	var value [1]byte
	if _, err := io.ReadFull(r.reader, value[:]); err != nil {
		return 0, err
	}
	return value[0], nil
}

type tuiTerminalKey struct {
	value byte
	err   error
}

func tuiReadTerminalByte(ctx context.Context, terminal *terminalSession) (byte, error) {
	result := make(chan tuiTerminalKey, 1)
	go func() {
		value, err := (tuiSingleByteReader{reader: terminal.reader}).ReadByte()
		result <- tuiTerminalKey{value: value, err: err}
	}()
	select {
	case <-ctx.Done():
		_ = terminal.Close()
		<-result
		return 0, ctx.Err()
	case key := <-result:
		return key.value, key.err
	}
}

func (s *TUISelector) selectLive(ctx context.Context, terminal *terminalSession, options []identityOption, requestContext SelectionContext, shownAt time.Time) ([]identity.Identity, error) {
	query := ""
	pendingUTF8 := make([]byte, 0, utf8.UTFMax)
	matches := matchIdentities(options, query)
	selected := 0
	previousLines := 0
	redraw := func() error {
		frame := tuiRenderSelectionFrame(options, matches, query, selected, terminal.width, requestContext, shownAt)
		if previousLines > 0 {
			if _, err := fmt.Fprintf(terminal.writer, "\x1b[%dA\r\x1b[J", previousLines); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(terminal.writer, frame); err != nil {
			return err
		}
		previousLines = strings.Count(frame, "\r\n")
		return nil
	}
	if err := redraw(); err != nil {
		return nil, fmt.Errorf("write selection prompt: %w", err)
	}

	for {
		value, err := tuiReadTerminalByte(ctx, terminal)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, io.EOF) {
				return nil, ErrCancelled
			}
			return nil, fmt.Errorf("read selection: %w", err)
		}
		switch value {
		case '\r', '\n':
			if len(matches) == 0 {
				_, _ = io.WriteString(terminal.writer, "\a")
				continue
			}
			if _, err := io.WriteString(terminal.writer, "\r\n"); err != nil {
				return nil, fmt.Errorf("write selection result: %w", err)
			}
			return []identity.Identity{matches[selected].identity}, nil
		case '\x03':
			_, _ = io.WriteString(terminal.writer, "^C\r\n")
			return nil, ErrCancelled
		case '\x1b':
			key, isArrow, err := tuiReadEscapeKey(ctx, terminal)
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, fmt.Errorf("read escape sequence: %w", err)
			}
			if !isArrow {
				_, _ = io.WriteString(terminal.writer, "\r\n")
				return nil, ErrCancelled
			}
			switch key {
			case 'A':
				if len(matches) > 0 {
					selected = (selected + len(matches) - 1) % len(matches)
				}
			case 'B':
				if len(matches) > 0 {
					selected = (selected + 1) % len(matches)
				}
			}
		case '\b', '\x7f':
			if len(pendingUTF8) != 0 {
				pendingUTF8 = pendingUTF8[:0]
			} else if query != "" {
				_, size := utf8.DecodeLastRuneInString(query)
				query = query[:len(query)-size]
				matches = matchIdentities(options, query)
				selected = 0
			}
		case '\t':
			if len(matches) > 0 {
				selected = (selected + 1) % len(matches)
			}
		case '\x15':
			query = ""
			pendingUTF8 = pendingUTF8[:0]
			matches = matchIdentities(options, query)
			selected = 0
		default:
			if value >= utf8.RuneSelf {
				pendingUTF8 = append(pendingUTF8, value)
				if !utf8.FullRune(pendingUTF8) {
					continue
				}
				r, size := utf8.DecodeRune(pendingUTF8)
				query += string(r)
				pendingUTF8 = pendingUTF8[size:]
			} else if value >= 0x20 {
				query += string(value)
			} else {
				continue
			}
			matches = matchIdentities(options, query)
			selected = 0
		}
		if err := redraw(); err != nil {
			return nil, fmt.Errorf("update selection prompt: %w", err)
		}
	}
}

// tuiReadEscapeKey distinguishes a standalone Escape press from the terminal's cursor-key sequence.
func tuiReadEscapeKey(ctx context.Context, terminal *terminalSession) (byte, bool, error) {
	result := make(chan tuiTerminalKey, 1)
	go func() {
		value, err := (tuiSingleByteReader{reader: terminal.reader}).ReadByte()
		result <- tuiTerminalKey{value: value, err: err}
	}()
	timer := time.NewTimer(40 * time.Millisecond)
	defer timer.Stop()
	var key tuiTerminalKey
	select {
	case <-ctx.Done():
		_ = terminal.Close()
		<-result
		return 0, false, ctx.Err()
	case <-timer.C:
		// Join the speculative read while keeping output open for the final newline.
		_ = terminal.StopInput()
		<-result
		return 0, false, nil
	case key = <-result:
	}
	if key.err != nil {
		return 0, false, key.err
	}
	if key.value != '[' && key.value != 'O' {
		return 0, false, nil
	}
	for {
		value, err := tuiReadTerminalByte(ctx, terminal)
		if err != nil {
			return 0, false, err
		}
		if value >= 0x40 && value <= 0x7e {
			return value, true, nil
		}
	}
}

func (s *TUISelector) selectLineBuffered(ctx context.Context, terminal *terminalSession, options []identityOption, requestContext SelectionContext, shownAt time.Time) ([]identity.Identity, error) {
	query := ""
	skipLF := false
	matches := matchIdentities(options, query)
	for {
		frame := tuiRenderSelectionFrame(options, matches, query, 0, terminal.width, requestContext, shownAt)
		if _, err := io.WriteString(terminal.writer, frame+"\r\n"); err != nil {
			return nil, fmt.Errorf("write selection prompt: %w", err)
		}
		lineCh := make(chan tuiInputLine, 1)
		go func() {
			line, skipNextLF, err := tuiReadInputLine(tuiSingleByteReader{reader: terminal.reader}, skipLF)
			lineCh <- tuiInputLine{line: line, skipLF: skipNextLF, err: err}
		}()
		select {
		case <-ctx.Done():
			// Interrupt and join the line reader before another prompt can consume stdin.
			_ = terminal.Close()
			<-lineCh
			return nil, ctx.Err()
		case result := <-lineCh:
			skipLF = result.skipLF
			if result.err != nil && ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if result.err != nil {
				if errors.Is(result.err, io.EOF) {
					return nil, ErrCancelled
				}
				return nil, fmt.Errorf("read selection: %w", result.err)
			}
			if result.line == "\x03" {
				return nil, ErrCancelled
			}
			if result.line == "\x1b" {
				return nil, ErrCancelled
			}
			if terminal.echoInput {
				if err := tuiEchoInputLine(terminal.writer, result.line); err != nil {
					return nil, fmt.Errorf("echo filter input: %w", err)
				}
			}
			if result.line == "" {
				if len(matches) == 0 {
					continue
				}
				return []identity.Identity{matches[0].identity}, nil
			}
			query = result.line
			matches = matchIdentities(options, query)
		}
	}
}

// tuiReadInputLine edits buffered input without reading ahead into the SSH process's input.
// Consume a pending CRLF suffix only when the picker needs another line.
func tuiReadInputLine(reader tuiByteReader, skipLF bool) (string, bool, error) {
	var first byte
	hasFirst := false
	if skipLF {
		value, err := reader.ReadByte()
		if err != nil {
			return "", false, err
		}
		if value != '\n' {
			first = value
			hasFirst = true
		}
	}

	line := make([]byte, 0, 16)
	for {
		value := first
		if hasFirst {
			hasFirst = false
		} else {
			var err error
			value, err = reader.ReadByte()
			if err != nil {
				return string(line), false, err
			}
		}
		switch value {
		case '\r':
			return string(line), true, nil
		case '\n':
			return string(line), false, nil
		case '\x03':
			return string(value), false, nil
		case '\x1b':
			return string(value), false, nil
		case '\b', '\x7f':
			if len(line) != 0 {
				_, size := utf8.DecodeLastRune(line)
				line = line[:len(line)-size]
			}
		default:
			line = append(line, value)
		}
	}
}

func tuiRenderSelectionFrame(options []identityOption, matches []identityMatch, query string, selected, terminalWidth int, requestContext SelectionContext, shownAt time.Time) string {
	noWidth, commentWidth, typeWidth, sizeWidth, fingerprintWidth := identityColumnWidths(options, terminalWidth)
	queryRunes := []rune(strings.ToLower(query))
	var frame strings.Builder
	fmt.Fprintf(&frame, "\r\n[%s - %s]\r\n\r\n", identitySelectionBrand, selectionDisplayTime(shownAt))
	tuiWriteSelectionMetadata(&frame, requestContext, terminalWidth)
	fmt.Fprintf(&frame, "\r\n\x1b[1;36m%s:\x1b[0m\r\n\r\n", identitySelectionPrompt)
	writeRow := func(active, highlight bool, number, comment, algorithm, size, fingerprint string) {
		if active {
			frame.WriteString("\x1b[1;33m")
		}
		rowQueryRunes := queryRunes
		if !highlight {
			rowQueryRunes = nil
		}
		queryIndex := 0
		frame.WriteByte('|')
		writeCell := func(value string, width int, rightAlign bool) {
			frame.WriteByte(' ')
			if rightAlign {
				frame.WriteString(strings.Repeat(" ", max(0, width-utf8.RuneCountInString(value))))
			}
			cell := fitIdentityCell(value, width)
			frame.WriteString(tuiHighlightCell(cell, rowQueryRunes, &queryIndex, active))
			if !rightAlign {
				frame.WriteString(strings.Repeat(" ", max(0, width-utf8.RuneCountInString(cell))))
			}
			frame.WriteString(" |")
		}
		writeCell(number, noWidth, false)
		tuiAdvanceMatchSpace(rowQueryRunes, &queryIndex)
		writeCell(comment, commentWidth, false)
		tuiAdvanceMatchSpace(rowQueryRunes, &queryIndex)
		writeCell(algorithm, typeWidth, false)
		tuiAdvanceMatchSpace(rowQueryRunes, &queryIndex)
		writeCell(size, sizeWidth, true)
		tuiAdvanceMatchSpace(rowQueryRunes, &queryIndex)
		writeCell(fingerprint, fingerprintWidth, false)
		frame.WriteString("\x1b[0m\r\n")
	}
	writeRow(false, false, "No", "Comment", "Type", "Size", "Fingerprint")
	fmt.Fprintf(&frame, "|%s|%s|%s|%s|%s|\r\n",
		strings.Repeat("-", noWidth+2), strings.Repeat("-", commentWidth+2),
		strings.Repeat("-", typeWidth+2), strings.Repeat("-", sizeWidth+2),
		strings.Repeat("-", fingerprintWidth+2))
	if len(matches) == 0 {
		frame.WriteString("| No matching identities |\r\n")
	} else {
		for index, match := range matches {
			number := fmt.Sprintf("%d", match.index+1)
			writeRow(index == selected, true, number, match.comment, match.algorithm, match.size, match.fingerprint)
		}
		if fingerprintWidth < tuiMaxFingerprintWidth(options) && selected >= 0 && selected < len(matches) {
			fmt.Fprintf(&frame, "Full fingerprint: %s\r\n", matches[selected].fingerprint)
		}
	}
	frame.WriteString("\r\n")
	fmt.Fprintf(&frame, "%d/%d\r\n", len(matches), len(options))
	frame.WriteString("> ")
	if query == "" {
		fmt.Fprintf(&frame, "\x1b[90m%s\x1b[0m", identitySelectionFilterHint)
		fmt.Fprintf(&frame, "\x1b[%dD", utf8.RuneCountInString(identitySelectionFilterHint))
	} else {
		frame.WriteString(query)
	}
	return frame.String()
}

// tuiMaxFingerprintWidth reports the full fingerprint width before terminal truncation.
func tuiMaxFingerprintWidth(options []identityOption) int {
	maximum := utf8.RuneCountInString("Fingerprint")
	for _, option := range options {
		maximum = max(maximum, utf8.RuneCountInString(option.fingerprint))
	}
	return maximum
}

func tuiWriteSelectionMetadata(frame *strings.Builder, requestContext SelectionContext, terminalWidth int) {
	for _, line := range selectionTreeLines(requestContext) {
		label := line.prefix + line.label
		if line.hasValue {
			tuiWriteWrappedSelectionLine(frame, label, line.value, terminalWidth)
		} else {
			fmt.Fprintf(frame, "%s\r\n", label)
		}
	}
}

func tuiWriteWrappedSelectionLine(frame *strings.Builder, label, value string, terminalWidth int) {
	if terminalWidth < 1 {
		terminalWidth = 80
	}
	prefix := label + ": "
	continuation := strings.Repeat(" ", utf8.RuneCountInString(prefix))
	terminalWidth = max(terminalWidth, utf8.RuneCountInString(prefix)+1)
	line := prefix
	lineWidth := utf8.RuneCountInString(prefix)
	for _, word := range strings.Fields(value) {
		wordRunes := []rune(word)
		for len(wordRunes) > 0 {
			available := terminalWidth - lineWidth
			if lineWidth > utf8.RuneCountInString(continuation) && available > 0 {
				line += " "
				lineWidth++
				available--
			}
			if available <= 0 {
				frame.WriteString(line + "\r\n")
				line = continuation
				lineWidth = utf8.RuneCountInString(continuation)
				continue
			}
			count := min(len(wordRunes), available)
			line += string(wordRunes[:count])
			lineWidth += count
			wordRunes = wordRunes[count:]
			if len(wordRunes) > 0 {
				frame.WriteString(line + "\r\n")
				line = continuation
				lineWidth = utf8.RuneCountInString(continuation)
			}
		}
	}
	frame.WriteString(line + "\r\n")
}

func tuiAdvanceMatchSpace(query []rune, queryIndex *int) {
	if *queryIndex < len(query) && query[*queryIndex] == ' ' {
		*queryIndex++
	}
}

func tuiHighlightCell(value string, query []rune, queryIndex *int, selected bool) string {
	var highlighted strings.Builder
	restoreColor := "\x1b[39m"
	if selected {
		restoreColor = "\x1b[33m"
	}
	for _, r := range value {
		if *queryIndex < len(query) && unicode.ToLower(r) == query[*queryIndex] {
			highlighted.WriteString("\x1b[92m")
			highlighted.WriteRune(r)
			highlighted.WriteString(restoreColor)
			*queryIndex++
			continue
		}
		highlighted.WriteRune(r)
	}
	return highlighted.String()
}
