// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package ico

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestEncodePNGFramesProducesValidICOEntries(t *testing.T) {
	sizes := []int{16, 24, 32, 48, 256}
	var frames [][]byte
	for _, size := range sizes {
		frame := image.NewNRGBA(image.Rect(0, 0, size, size))
		frame.SetNRGBA(size/2, size/2, color.NRGBA{R: 20, G: 100, B: 220, A: 255})
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, frame); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, encoded.Bytes())
	}

	data, err := EncodePNGFrames(frames)
	if err != nil {
		t.Fatal(err)
	}

	const (
		headerSize = 6
		entrySize  = 16
	)
	if len(data) < headerSize+entrySize*len(sizes) {
		t.Fatalf("ICO length = %d, smaller than its directory", len(data))
	}
	if got := binary.LittleEndian.Uint16(data[:2]); got != 0 {
		t.Fatalf("reserved ICO header field = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint16(data[2:4]); got != 1 {
		t.Fatalf("ICO type = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(data[4:6]); got != uint16(len(sizes)) {
		t.Fatalf("ICO frame count = %d, want %d", got, len(sizes))
	}

	for index, size := range sizes {
		entryStart := headerSize + index*entrySize
		entry := data[entryStart : entryStart+entrySize]
		wantDimension := byte(size)
		if size == 256 {
			wantDimension = 0
		}
		if entry[0] != wantDimension || entry[1] != wantDimension {
			t.Fatalf("ICO frame %d dimensions = %dx%d, want %dx%d", index, entry[0], entry[1], wantDimension, wantDimension)
		}
		if planes, depth := binary.LittleEndian.Uint16(entry[4:6]), binary.LittleEndian.Uint16(entry[6:8]); planes != 1 || depth != 32 {
			t.Fatalf("ICO frame %d planes/depth = %d/%d, want 1/32", index, planes, depth)
		}
		frameSize := binary.LittleEndian.Uint32(entry[8:12])
		frameOffset := binary.LittleEndian.Uint32(entry[12:16])
		frameEnd := uint64(frameOffset) + uint64(frameSize)
		if frameOffset < uint32(headerSize+entrySize*len(sizes)) || frameEnd > uint64(len(data)) {
			t.Fatalf("ICO frame %d range = %d+%d, outside file length %d", index, frameOffset, frameSize, len(data))
		}
		decoded, err := png.Decode(bytes.NewReader(data[int(frameOffset):int(frameEnd)]))
		if err != nil {
			t.Fatalf("decode PNG frame %d: %v", index, err)
		}
		if bounds := decoded.Bounds(); bounds.Dx() != size || bounds.Dy() != size {
			t.Errorf("decoded frame %d size = %dx%d, want %dx%d", index, bounds.Dx(), bounds.Dy(), size, size)
		}
		if got := color.NRGBAModel.Convert(decoded.At(size/2, size/2)).(color.NRGBA); got != (color.NRGBA{R: 20, G: 100, B: 220, A: 255}) {
			t.Errorf("decoded frame %d center = %#v, want opaque blue", index, got)
		}
	}
}

func TestEncodePNGFramesRejectsInvalidDimensions(t *testing.T) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 300, 100))); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodePNGFrames([][]byte{encoded.Bytes()}); err == nil {
		t.Fatal("EncodePNGFrames accepted a non-square oversized frame")
	}
}
