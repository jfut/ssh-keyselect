// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package sshwire contains shared readers for SSH wire-format primitives.
package sshwire

import (
	"encoding/binary"
	"errors"
)

var errMalformedString = errors.New("malformed SSH string")

// ReadString reads one SSH string and returns the bytes after it.
func ReadString(data []byte) (value, rest []byte, err error) {
	if len(data) < 4 {
		return nil, nil, errMalformedString
	}
	n := binary.BigEndian.Uint32(data[:4])
	if uint64(n) > uint64(len(data)-4) {
		return nil, nil, errMalformedString
	}
	end := 4 + int(n)
	return data[4:end], data[end:], nil
}
