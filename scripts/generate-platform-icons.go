// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Command generate-platform-icons creates the platform icon files from the
// shared PNG source so all platforms use the same artwork.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
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

	sizes := []int{16, 24, 32, 48, 256}
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
		if chunkType, ok := icnsChunkType(size); ok {
			icns.WriteString(chunkType)
			var chunkSize [4]byte
			binary.BigEndian.PutUint32(chunkSize[:], uint32(len(iconPNG)+8))
			icns.Write(chunkSize[:])
			icns.Write(iconPNG)
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

func icnsChunkType(size int) (string, bool) {
	switch size {
	case 16:
		return "icp4", true
	case 32:
		return "icp5", true
	case 256:
		return "ic08", true
	default:
		return "", false
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
