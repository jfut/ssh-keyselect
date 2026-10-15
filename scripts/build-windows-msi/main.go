// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Command build-windows-msi packages the Windows CLI, GUI, and release notices as an MSI.
package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	msi "go.digitalxero.dev/go-msi"
)

const (
	productName  = "SSH KeySelect"
	manufacturer = "Jun Futagawa"
	featureID    = "MainFeature"
	componentID  = "MainComponent"
)

var semverPattern = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$`)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 6 {
		return fmt.Errorf("usage: build-windows-msi <version> <amd64|arm64> <commit> <cli-exe> <gui-exe> <output-msi>")
	}

	version, architecture, commit, cliExecutable, guiExecutable, output := args[0], args[1], args[2], args[3], args[4], args[5]
	msiVersion, err := parseVersion(version)
	if err != nil {
		return err
	}

	platform, err := platformFor(architecture)
	if err != nil {
		return err
	}
	if err := requireFile(cliExecutable); err != nil {
		return err
	}
	if err := requireFile(guiExecutable); err != nil {
		return err
	}
	license, err := os.ReadFile("LICENSE")
	if err != nil {
		return fmt.Errorf("read LICENSE: %w", err)
	}
	if err := requireFile("CREDITS"); err != nil {
		return err
	}

	// This MSI installs for all users under Program Files and requires elevation.
	b := msi.NewPackage().
		WithProductName(productName).
		WithManufacturer(manufacturer).
		WithVersion(msiVersion).
		WithProductCode(guidV5("product|"+architecture+"|"+version+"|"+commit)).
		WithUpgradeCode(guidV5("upgrade|"+architecture)).
		WithAllUsers(true).
		WithPlatform(platform).
		InstallToProgramFiles().
		WithProperty("ARPPRODUCTICON", "SSHKeySelectIcon")
	b = addInstallerUI(b, string(license))

	installDir := b.RootDirectory("INSTALLFOLDER", productName)

	component := installDir.Component(componentID).AssociateToFeature(featureID)
	component.WithFilePath("ssh-keyselect-gui.exe", guiExecutable)
	component.WithFilePath("ssh-keyselect.exe", cliExecutable)
	component.WithFilePath("LICENSE", "LICENSE")
	component.WithFilePath("CREDITS", "CREDITS")

	icon, err := msi.FileSourceFromPath(guiExecutable)
	if err != nil {
		return fmt.Errorf("read GUI executable icon: %w", err)
	}
	b.Icon("SSHKeySelectIcon", icon)
	component.Shortcut("SSH KeySelect", "[INSTALLFOLDER]ssh-keyselect-gui.exe").
		InDirectory("ProgramMenuFolder").
		Description("Launch SSH KeySelect").
		Icon("SSHKeySelectIcon", 0)

	b.Feature(featureID).WithTitle(productName).WithLevel(1)
	// MSI stores numeric versions only. Allow an equal-version upgrade so a
	// local -next build can be replaced by the matching tagged release.
	b.MajorUpgrade().AllowSameVersionUpgrades().Done()

	pkg, err := b.Build()
	if err != nil {
		return fmt.Errorf("build MSI package: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return fmt.Errorf("create MSI output directory: %w", err)
	}
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create MSI: %w", err)
	}
	writeErr := pkg.WriteMSI(f)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write MSI: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close MSI: %w", closeErr)
	}
	timestamp, err := buildTimestamp()
	if err != nil {
		return err
	}
	if err := updateMSISummary(output, timestamp); err != nil {
		return err
	}
	return nil
}

func parseVersion(version string) (string, error) {
	match := semverPattern.FindStringSubmatch(version)
	if match == nil {
		return "", fmt.Errorf("version %q must be a semantic version", version)
	}
	return strings.Join(match[1:4], "."), nil
}

func platformFor(architecture string) (msi.Platform, error) {
	switch architecture {
	case "amd64":
		return msi.Platform_x64, nil
	case "arm64":
		return msi.Platform_Arm64, nil
	default:
		return 0, fmt.Errorf("unsupported MSI architecture %q", architecture)
	}
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("%s must be a non-empty regular file", path)
	}
	return nil
}

// Derive stable MSI identities while changing ProductCode for each version and
// keeping UpgradeCode separate for amd64 and arm64 packages.
func guidV5(name string) string {
	namespace := [16]byte{0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	hash := sha1.New()
	_, _ = hash.Write(namespace[:])
	_, _ = hash.Write([]byte(name))
	id := hash.Sum(nil)[:16]
	id[6] = (id[6] & 0x0f) | 0x50
	id[8] = (id[8] & 0x3f) | 0x80
	encoded := strings.ToUpper(hex.EncodeToString(id))
	return fmt.Sprintf("{%s-%s-%s-%s-%s}", encoded[:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:])
}
