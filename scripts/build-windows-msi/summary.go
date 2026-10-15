// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/abemedia/go-cfb"
	"github.com/abemedia/go-cfb/oleps"
)

const (
	msiSummaryInformationStream = "\x05SummaryInformation"
	msiSummaryCreateTime        = 12
	msiSummarySaveTime          = 13
	msiSummaryWordCount         = 15
	msiSummaryCreatingApp       = 18
	msiCreatingAppName          = "go-msi"
	msiSummaryNoElevationNeeded = 8
)

// buildTimestamp honors reproducible-build callers and otherwise records the
// UTC time at which this MSI is created.
func buildTimestamp() (time.Time, error) {
	if value := os.Getenv("SOURCE_DATE_EPOCH"); value != "" {
		seconds, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse SOURCE_DATE_EPOCH %q: %w", value, err)
		}
		return time.Unix(seconds, 0).UTC(), nil
	}
	return time.Now().UTC(), nil
}

// updateMSISummary replaces go-msi's fixed sample metadata with the actual
// build time and the tool name, while preserving the MSI's other CFB streams.
func updateMSISummary(path string, timestamp time.Time) error {
	source, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open MSI to update summary information: %w", err)
	}
	reader, err := cfb.NewReader(source)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("read MSI compound file: %w", err)
	}
	summaryStream, err := reader.OpenStream(msiSummaryInformationStream)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("open MSI summary information: %w", err)
	}
	summaryData, err := io.ReadAll(summaryStream.Open())
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("read MSI summary information: %w", err)
	}
	summary, err := oleps.Unmarshal(summaryData)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("decode MSI summary information: %w", err)
	}
	if len(summary.PropertySets) != 1 {
		_ = source.Close()
		return fmt.Errorf("MSI summary information has %d property sets, want 1", len(summary.PropertySets))
	}

	updated := map[uint32]bool{}
	for i := range summary.PropertySets[0].Properties {
		property := &summary.PropertySets[0].Properties[i]
		switch property.ID {
		case msiSummaryCreateTime, msiSummarySaveTime:
			property.Value = oleps.FileTime(timestamp)
			updated[property.ID] = true
		case msiSummaryWordCount:
			wordCount, ok := property.Value.(oleps.I4)
			if !ok {
				_ = source.Close()
				return fmt.Errorf("MSI Word Count Summary has type %T, want I4", property.Value)
			}
			// Clear bit 3 so Windows Installer can request elevation for this per-machine package.
			property.Value = wordCount &^ msiSummaryNoElevationNeeded
			updated[property.ID] = true
		case msiSummaryCreatingApp:
			property.Value = oleps.LPSTR(msiCreatingAppName)
			updated[property.ID] = true
		}
	}
	for _, id := range []uint32{msiSummaryCreateTime, msiSummarySaveTime, msiSummaryWordCount, msiSummaryCreatingApp} {
		if !updated[id] {
			_ = source.Close()
			return fmt.Errorf("MSI summary information is missing property %d", id)
		}
	}

	updatedSummary, err := oleps.Marshal(summary)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("encode MSI summary information: %w", err)
	}

	outputInfo, err := os.Stat(path)
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("stat MSI before updating summary information: %w", err)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".msi-summary-*")
	if err != nil {
		_ = source.Close()
		return fmt.Errorf("create temporary MSI: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	writer := cfb.NewWriterV3(temp)
	replaced, err := copyStorage(writer.StorageWriter, reader.Storage, updatedSummary, true)
	if err != nil {
		_ = temp.Close()
		_ = source.Close()
		return fmt.Errorf("copy MSI streams: %w", err)
	}
	if !replaced {
		_ = temp.Close()
		_ = source.Close()
		return fmt.Errorf("MSI summary information stream was not copied")
	}
	if err := writer.Close(); err != nil {
		_ = temp.Close()
		_ = source.Close()
		return fmt.Errorf("write updated MSI compound file: %w", err)
	}
	if err := temp.Chmod(outputInfo.Mode().Perm()); err != nil {
		_ = temp.Close()
		_ = source.Close()
		return fmt.Errorf("set updated MSI permissions: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		_ = source.Close()
		return fmt.Errorf("sync updated MSI: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = source.Close()
		return fmt.Errorf("close updated MSI: %w", err)
	}
	if err := source.Close(); err != nil {
		return fmt.Errorf("close original MSI: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace MSI with updated summary information: %w", err)
	}
	return nil
}

// copyStorage recreates a CFB storage tree and replaces the root summary
// stream when requested.
func copyStorage(destination *cfb.StorageWriter, source *cfb.Storage, summary []byte, replaceSummary bool) (bool, error) {
	destination.CLSID = source.CLSID
	destination.StateBits = source.StateBits
	destination.Created = source.Created
	destination.Modified = source.Modified
	replaced := false
	for _, entry := range source.Entries {
		switch entry := entry.(type) {
		case *cfb.Storage:
			child, err := destination.CreateStorage(entry.Name)
			if err != nil {
				return false, fmt.Errorf("create storage %q: %w", entry.Name, err)
			}
			childReplaced, err := copyStorage(child, entry, nil, false)
			if err != nil {
				return false, err
			}
			replaced = replaced || childReplaced
		case *cfb.Stream:
			stream, err := destination.CreateStream(entry.Name)
			if err != nil {
				return false, fmt.Errorf("create stream %q: %w", entry.Name, err)
			}
			stream.StateBits = entry.StateBits
			var sourceReader io.Reader = entry.Open()
			if replaceSummary && entry.Name == msiSummaryInformationStream {
				sourceReader = bytes.NewReader(summary)
				replaced = true
			}
			if _, err := io.Copy(stream, sourceReader); err != nil {
				_ = stream.Close()
				return false, fmt.Errorf("copy stream %q: %w", entry.Name, err)
			}
			if err := stream.Close(); err != nil {
				return false, fmt.Errorf("close copied stream %q: %w", entry.Name, err)
			}
		default:
			return false, fmt.Errorf("unsupported MSI compound-file entry %T", entry)
		}
	}
	return replaced, nil
}
