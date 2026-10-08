// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Command generate-platform-icons creates the platform icon files from the
// shared PNG source so all platforms use the same artwork.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/ico"
)

func main() {
	const generated = "assets/gui/generated"
	const platform = generated + "/platform"
	if err := os.MkdirAll(platform, 0o755); err != nil {
		fatal(err)
	}

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	frames := make([][]byte, 0, len(sizes))
	var icns bytes.Buffer
	icns.WriteString("icns")
	icns.Write([]byte{0, 0, 0, 0}) // Patched with the final size below.
	for _, size := range sizes {
		iconPNG, err := guiassets.PNG(size)
		if err != nil {
			fatal(err)
		}
		frames = append(frames, iconPNG)
		path := filepath.Join(platform, fmt.Sprintf("%d.png", size))
		if err := os.WriteFile(path, iconPNG, 0o644); err != nil {
			fatal(err)
		}
		for _, chunk := range icnsChunks(size) {
			chunkData := iconPNG
			if chunk.argb {
				chunkData, err = encodeICNSARGB(iconPNG, size)
				if err != nil {
					fatal(err)
				}
			}
			icns.WriteString(chunk.typeCode)
			var chunkSize [4]byte
			binary.BigEndian.PutUint32(chunkSize[:], uint32(len(chunkData)+8))
			icns.Write(chunkSize[:])
			icns.Write(chunkData)
		}
	}
	icoData, err := ico.EncodePNGFrames(frames)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(generated, "ssh-keyselect-icon.ico"), icoData, 0o644); err != nil {
		fatal(err)
	}
	binary.BigEndian.PutUint32(icns.Bytes()[4:8], uint32(icns.Len()))
	if err := os.WriteFile(filepath.Join(platform, "ssh-keyselect.icns"), icns.Bytes(), 0o644); err != nil {
		fatal(err)
	}
}

type icnsChunk struct {
	typeCode string
	argb     bool
}

func icnsChunks(size int) []icnsChunk {
	switch size {
	case 16:
		return []icnsChunk{{typeCode: "ic04", argb: true}}
	case 32:
		return []icnsChunk{
			{typeCode: "ic05", argb: true},
			{typeCode: "ic11"}, // 16px Retina slot; reuse the 32px PNG representation.
		}
	case 64:
		return []icnsChunk{{typeCode: "ic12"}} // 32px Retina slot.
	case 128:
		return []icnsChunk{{typeCode: "ic07"}}
	case 256:
		return []icnsChunk{
			{typeCode: "ic08"},
			{typeCode: "ic13"}, // 128px Retina slot; reuse the 256px PNG representation.
		}
	default:
		return nil
	}
}

// Encode the 1x Finder slots as ARGB because macOS renders PNG payloads in icp4/icp5 as noise.
func encodeICNSARGB(iconPNG []byte, size int) ([]byte, error) {
	icon, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return nil, fmt.Errorf("decode %dpx icon for ICNS: %w", size, err)
	}
	if bounds := icon.Bounds(); bounds.Dx() != size || bounds.Dy() != size {
		return nil, fmt.Errorf("decode %dpx icon for ICNS: got %dx%d", size, bounds.Dx(), bounds.Dy())
	}

	pixelCount := size * size
	planes := [4][]byte{
		make([]byte, pixelCount),
		make([]byte, pixelCount),
		make([]byte, pixelCount),
		make([]byte, pixelCount),
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			pixel := color.NRGBAModel.Convert(icon.At(x, y)).(color.NRGBA)
			index := y*size + x
			planes[0][index] = pixel.A
			planes[1][index] = pixel.R
			planes[2][index] = pixel.G
			planes[3][index] = pixel.B
		}
	}

	var payload bytes.Buffer
	payload.WriteString("ARGB")
	for _, plane := range planes {
		payload.Write(encodeICNSPackBits(plane))
	}
	return payload.Bytes(), nil
}

// PackBits encodes each ICNS color plane using the repeat and literal run limits from the format.
func encodeICNSPackBits(data []byte) []byte {
	var encoded bytes.Buffer
	for index := 0; index < len(data); {
		runLength := icnsRunLength(data, index)
		if runLength >= 3 {
			encoded.WriteByte(0x80 | byte(runLength-3))
			encoded.WriteByte(data[index])
			index += runLength
			continue
		}

		literalStart := index
		index += runLength
		for index < len(data) && index-literalStart < 128 {
			runLength = icnsRunLength(data, index)
			if runLength >= 3 {
				break
			}
			remaining := 128 - (index - literalStart)
			if runLength > remaining {
				index += remaining
				break
			}
			index += runLength
		}

		literal := data[literalStart:index]
		encoded.WriteByte(byte(len(literal) - 1))
		encoded.Write(literal)
	}
	return encoded.Bytes()
}

func icnsRunLength(data []byte, start int) int {
	length := 1
	for start+length < len(data) && data[start+length] == data[start] && length < 130 {
		length++
	}
	return length
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
