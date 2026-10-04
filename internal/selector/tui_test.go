// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestEchoInputLine(t *testing.T) {
	var output bytes.Buffer
	if err := tuiEchoInputLine(&output, "1\x1b[31m"); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "1 [31m\r\n"; got != want {
		t.Fatalf("echoed input = %q, want %q", got, want)
	}
}

func TestReadInputLineAcceptsCRLFAndLF(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("1\r\n2\r3\n"))
	skipLF := false
	for _, want := range []struct {
		line   string
		skipLF bool
	}{
		{line: "1", skipLF: true},
		{line: "2", skipLF: true},
		{line: "3"},
	} {
		line, skipNextLF, err := tuiReadInputLineWithEcho(reader, skipLF, nil)
		if err != nil {
			t.Fatal(err)
		}
		if line != want.line || skipNextLF != want.skipLF {
			t.Fatalf("readInputLine() = %q, skipLF %t; want %q, skipLF %t", line, skipNextLF, want.line, want.skipLF)
		}
		skipLF = skipNextLF
	}
}

func TestReadInputLineErasesWithBackspaceAndDelete(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("12\b\r\n345\x7f\x7f6\n"))
	line, skipLF, err := tuiReadInputLineWithEcho(reader, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if line != "1" || !skipLF {
		t.Fatalf("backspace result = %q, skipLF %t; want %q, skipLF true", line, skipLF, "1")
	}
	line, skipLF, err = tuiReadInputLineWithEcho(reader, skipLF, nil)
	if err != nil {
		t.Fatal(err)
	}
	if line != "36" || skipLF {
		t.Fatalf("delete result = %q, skipLF %t; want %q, skipLF false", line, skipLF, "36")
	}
	line, _, err = tuiReadInputLineWithEcho(bufio.NewReader(strings.NewReader("1あ\x7f\n")), false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if line != "1" {
		t.Fatalf("line after deleting a Unicode rune = %q, want %q", line, "1")
	}
}

func TestReadInputLineWithEchoErasesVisuallyAndReturnsControlC(t *testing.T) {
	var output bytes.Buffer
	line, skipLF, err := tuiReadInputLineWithEcho(bufio.NewReader(strings.NewReader("1\b2\b\x03")), false, &output)
	if err != nil {
		t.Fatal(err)
	}
	if line != "\x03" || skipLF {
		t.Fatalf("interrupted input = %q, skipLF %t; want Ctrl+C and skipLF false", line, skipLF)
	}
	if got, want := output.String(), "1\b \b2\b \b^C\r\n"; got != want {
		t.Fatalf("live input echo = %q, want %q", got, want)
	}
	line, skipLF, err = tuiReadInputLineWithEcho(bufio.NewReader(strings.NewReader("\x1b")), false, nil)
	if err != nil || line != "\x1b" || skipLF {
		t.Fatalf("Escape input = %q, skipLF %t, error %v; want immediate cancellation", line, skipLF, err)
	}
}

func TestSelectionFrameShowsDisplayTimeHostContextAndCancelHint(t *testing.T) {
	shownAt := time.Date(2026, time.September, 27, 12, 34, 56, 789_000_000, time.FixedZone("JST", 9*60*60))
	requestContext := SelectionContext{HostBindings: []HostBinding{
		{
			Algorithm:    "ssh-ed25519",
			Fingerprint:  "SHA256:first-hop",
			KnownHosts:   []string{"gateway.example.test"},
			IsForwarding: true,
		},
		{
			Algorithm:    "ssh-ed25519",
			Fingerprint:  "SHA256:second-hop",
			KnownHosts:   []string{"app.example.test"},
			IsForwarding: true,
		},
		{
			Algorithm:   "ssh-ed25519",
			Fingerprint: "SHA256:destination-host",
			KnownHosts:  []string{"node.example.test"},
		},
	}}
	frame := tuiRenderSelectionFrame(nil, nil, "", 0, 100, requestContext, shownAt)
	if !strings.Contains(frame, "["+identitySelectionBrand+" - 2026-09-27 12:34:56 JST]") {
		t.Errorf("selection frame is missing its display time:\n%s", frame)
	}
	if !strings.Contains(frame, identitySelectionFilterHint) {
		t.Errorf("empty filter should show the cancellation hint:\n%s", frame)
	}
	filteredFrame := tuiRenderSelectionFrame(nil, nil, "agent", 0, 100, requestContext, shownAt)
	if !strings.HasSuffix(filteredFrame, "> agent") {
		t.Errorf("typed filter should replace the hint, got:\n%s", filteredFrame)
	}
	prompt := "\x1b[1;36m" + identitySelectionPrompt + ":\x1b[0m"
	promptAt := strings.Index(frame, prompt)
	tableAt := strings.Index(frame, "| No |")
	if promptAt < 0 || tableAt <= promptAt {
		t.Errorf("selection prompt should precede the key table:\n%s", frame)
	}
	for _, expected := range []string{
		"├─ Forwarding hop 1",
		"│  Host key: ssh-ed25519 SHA256:first-hop",
		"│  known_hosts hints: gateway.example.test",
		"├─ Forwarding hop 2",
		"│  Host key: ssh-ed25519 SHA256:second-hop",
		"│  known_hosts hints: app.example.test",
		"└─ Current target host",
		"   Host key: ssh-ed25519 SHA256:destination-host",
		"   known_hosts hints: node.example.test",
		identitySelectionHint,
	} {
		if !strings.Contains(frame, expected) {
			t.Errorf("selection frame is missing %q:\n%s", expected, frame)
		}
	}
}
