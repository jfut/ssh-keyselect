// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package selector provides the interactive identity-selection interface.
package selector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jfut/ssh-keyselect/internal/identity"
)

var ErrCancelled = errors.New("identity selection cancelled")

const (
	identitySelectionBrand      = "ssh-keyselect"
	identitySelectionPrompt     = "Select an SSH key to allow"
	identitySelectionHint       = "If you don't recognize this request, press Esc to cancel."
	identitySelectionFilterHint = "Filter (" + identitySelectionHint + ")"
)

// HostBinding is a server host key verified from OpenSSH's session-bind extension.
type HostBinding struct {
	Algorithm    string
	Fingerprint  string
	KnownHosts   []string
	IsForwarding bool
}

// SelectionContext carries verified connection details to the identity picker.
type SelectionContext struct {
	HostBindings []HostBinding
}

type selectionDetail struct {
	label      string
	keyDetails string
	knownHosts string
}

// selectionTreeLine stores one row of verified host details for both picker views.
type selectionTreeLine struct {
	prefix   string
	label    string
	value    string
	hasValue bool
}

func selectionDisplayTime(shownAt time.Time) string {
	return shownAt.Format("2006-01-02 15:04:05 MST")
}

// selectionRemainingSeconds rounds up so the display never shows zero before the deadline.
func selectionRemainingSeconds(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0, false
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, true
	}
	seconds := int(remaining / time.Second)
	if remaining%time.Second != 0 {
		seconds++
	}
	return seconds, true
}

// selectionDetails keeps the verified host path consistent between terminal and GUI pickers.
func selectionDetails(requestContext SelectionContext) []selectionDetail {
	if len(requestContext.HostBindings) == 0 {
		return []selectionDetail{{label: "Verified host key", keyDetails: "Not provided"}}
	}
	fields := make([]selectionDetail, 0, len(requestContext.HostBindings))
	for index, binding := range requestContext.HostBindings {
		kind := "Current target host"
		if binding.IsForwarding {
			kind = fmt.Sprintf("Forwarding hop %d", index+1)
		}
		keyDetails := strings.TrimSpace(identity.DisplayText(binding.Algorithm) + " " + identity.DisplayText(binding.Fingerprint))
		detail := selectionDetail{label: kind, keyDetails: keyDetails}
		if len(binding.KnownHosts) > 0 {
			names := make([]string, len(binding.KnownHosts))
			for i, name := range binding.KnownHosts {
				names[i] = identity.DisplayText(name)
			}
			detail.knownHosts = strings.Join(names, ", ")
		}
		fields = append(fields, detail)
	}
	return fields
}

// selectionTreeLines builds the shared ASCII hop hierarchy used by the terminal and GUI pickers.
func selectionTreeLines(requestContext SelectionContext) []selectionTreeLine {
	details := selectionDetails(requestContext)
	lines := make([]selectionTreeLine, 0, len(details)*3)
	for index, detail := range details {
		branch := "|--"
		fieldIndent := "|   "
		if index == len(details)-1 {
			branch = "`--"
			fieldIndent = "   "
		}
		lines = append(lines,
			selectionTreeLine{prefix: branch + " ", label: detail.label},
			selectionTreeLine{prefix: fieldIndent, label: "Host key", value: detail.keyDetails, hasValue: true},
		)
		if detail.knownHosts != "" {
			lines = append(lines, selectionTreeLine{prefix: fieldIndent, label: "known_hosts hints", value: detail.knownHosts, hasValue: true})
		}
	}
	return lines
}

// Selector chooses one or more public identities for a client session.
type Selector interface {
	Select(context.Context, []identity.Identity, SelectionContext) ([]identity.Identity, error)
}
