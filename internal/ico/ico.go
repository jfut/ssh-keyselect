// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package ico reads and writes Windows ICO files with PNG image frames.
package ico

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
)

const (
	iconDirectoryHeaderSize = 6
	iconDirectoryEntrySize  = 16
)

// EncodePNGFrames packages PNG images as the frames of an ICO file.
func EncodePNGFrames(frames [][]byte) ([]byte, error) {
	if len(frames) == 0 || len(frames) > int(^uint16(0)) {
		return nil, errors.New("ICO must contain between 1 and 65535 frames")
	}

	configs := make([]image.Config, len(frames))
	totalSize := uint64(iconDirectoryHeaderSize + len(frames)*iconDirectoryEntrySize)
	for index, frame := range frames {
		config, err := png.DecodeConfig(bytes.NewReader(frame))
		if err != nil {
			return nil, fmt.Errorf("decode PNG frame %d: %w", index, err)
		}
		if config.Width < 1 || config.Height < 1 || config.Width > 256 || config.Height > 256 || config.Width != config.Height {
			return nil, fmt.Errorf("PNG frame %d must be square and between 1px and 256px", index)
		}
		if uint64(len(frame)) > uint64(^uint32(0)) {
			return nil, fmt.Errorf("PNG frame %d exceeds the ICO frame size limit", index)
		}
		configs[index] = config
		totalSize += uint64(len(frame))
	}
	if totalSize > uint64(int(^uint(0)>>1)) || totalSize > uint64(^uint32(0)) {
		return nil, errors.New("ICO exceeds the supported size limit")
	}

	directorySize := iconDirectoryHeaderSize + len(frames)*iconDirectoryEntrySize
	data := make([]byte, directorySize, int(totalSize))
	binary.LittleEndian.PutUint16(data[2:4], 1)
	binary.LittleEndian.PutUint16(data[4:6], uint16(len(frames)))
	dataOffset := uint32(directorySize)
	for index, frame := range frames {
		config := configs[index]
		width := byte(config.Width)
		if config.Width == 256 {
			width = 0
		}
		entryStart := iconDirectoryHeaderSize + index*iconDirectoryEntrySize
		entry := data[entryStart : entryStart+iconDirectoryEntrySize]
		entry[0], entry[1] = width, width
		binary.LittleEndian.PutUint16(entry[4:6], 1)
		binary.LittleEndian.PutUint16(entry[6:8], 32)
		binary.LittleEndian.PutUint32(entry[8:12], uint32(len(frame)))
		binary.LittleEndian.PutUint32(entry[12:16], dataOffset)
		dataOffset += uint32(len(frame))
	}
	for _, frame := range frames {
		data = append(data, frame...)
	}
	return data, nil
}
