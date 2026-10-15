// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"strconv"
	"strings"
	"unicode/utf16"

	msi "go.digitalxero.dev/go-msi"
)

// go-msi exposes common control bits but not DirectoryCombo volume filters.
const (
	directoryComboFixedVolume  msi.ControlAttr = 0x00020000
	directoryComboRemoteVolume msi.ControlAttr = 0x00040000
)

// addInstallerUI authors the dialogs directly so the license uses a scrollable
// control and the progress dialog stays modeless while Windows Installer runs.
func addInstallerUI(b msi.PackageBuilder, license string) msi.PackageBuilder {
	// BrowseDlg's indirect controls need their install directory resolved before they open.
	b = b.WithProperty("DefaultUIFont", "DlgFont").
		WithProperty("WIXUI_INSTALLDIR", "INSTALLFOLDER").
		WithProperty("_BrowseProperty", "INSTALLFOLDER").
		WithProperty("RETURNINGFROMDIR", "0").
		WithProperty("ErrorDialog", "ErrorDlg").
		UIText("bytes", "bytes").
		UIText("Cancel", "Cancel")
	b.TextStyle("DlgFont", "Tahoma", 8).Done()
	b.TextStyle("DlgFontBold", "Tahoma", 8).Bold().Done()

	// Require license acceptance before opening the install-folder dialog.
	welcome := b.Dialog("WelcomeDlg").
		WithTitle("[ProductName] Setup").
		WithFirstControl("License").
		WithDefaultControl("Next").
		WithCancelControl("Cancel").
		ScheduleInUI(1, "")
	welcome.Control("Title", msi.ControlText).At(15, 15).Size(340, 20).
		WithText("{\\DlgFontBold}Welcome to the [ProductName] Setup").EndControl()
	welcome.Control("License", msi.ControlScrollableText).At(15, 45).Size(340, 150).
		WithAttributes(msi.ControlVisible, msi.ControlEnabled, msi.ControlSunken).
		TabNext("Accept").
		WithText(licenseAsRTF(license)).EndControl()
	welcome.Control("Accept", msi.ControlCheckBox).At(15, 205).Size(340, 18).
		WithProperty("AgreeToLicense").
		TabNext("Next").
		WithText("I &accept the terms in the license agreement").EndControl()
	welcome.Control("Next", msi.ControlPushButton).At(236, 243).Size(56, 17).
		TabNext("Cancel").
		WithText("&Next").
		WithControlCondition("Disable", `AgreeToLicense <> "1"`).
		WithControlCondition("Enable", `AgreeToLicense = "1"`).
		OnEvent("NewDialog", "InstallDirDlg", `AgreeToLicense = "1" AND RETURNINGFROMDIR = "1"`).
		OnEvent("EndDialog", "Return", `AgreeToLicense = "1" AND RETURNINGFROMDIR = "0"`).EndControl()
	welcome.Control("Cancel", msi.ControlPushButton).At(304, 243).Size(56, 17).
		TabNext("License").
		WithText("Cancel").OnEvent("EndDialog", "Exit", "").EndControl()

	installDir := b.Dialog("InstallDirDlg").
		WithTitle("[ProductName] Setup").
		WithFirstControl("Path").
		WithDefaultControl("Install").
		WithCancelControl("Cancel").
		ScheduleInUI(1297, "")
	installDir.Control("Title", msi.ControlText).At(15, 15).Size(340, 20).
		WithText("{\\DlgFontBold}Choose the installation folder").EndControl()
	installDir.Control("Description", msi.ControlText).At(25, 50).Size(320, 35).
		WithText("Enter the folder where SSH KeySelect should be installed.").EndControl()
	installDir.Control("FolderLabel", msi.ControlText).At(25, 100).Size(320, 18).
		WithText("Installation folder:").EndControl()
	installDir.Control("Path", msi.ControlPathEdit).At(25, 122).Size(260, 18).
		WithProperty("WIXUI_INSTALLDIR").
		WithAttributes(msi.ControlVisible, msi.ControlEnabled, msi.ControlIndirect).
		TabNext("Browse").EndControl()
	installDir.Control("Browse", msi.ControlPushButton).At(292, 122).Size(53, 18).
		TabNext("Back").
		WithText("&Browse...").
		OnEvent("SpawnDialog", "BrowseDlg", "").EndControl()
	installDir.Control("Back", msi.ControlPushButton).At(168, 243).Size(56, 17).
		TabNext("Install").
		WithText("&Back").
		OnEvent("[RETURNINGFROMDIR]", "1", "").
		OnEvent("NewDialog", "WelcomeDlg", "").EndControl()
	installDir.Control("Install", msi.ControlPushButton).At(236, 243).Size(56, 17).
		TabNext("Cancel").
		WithText("&Install").
		OnEvent("SetTargetPath", "[WIXUI_INSTALLDIR]", "").
		OnEvent("EndDialog", "Return", "").EndControl()
	installDir.Control("Cancel", msi.ControlPushButton).At(304, 243).Size(56, 17).
		TabNext("Path").
		WithText("Cancel").OnEvent("EndDialog", "Exit", "").EndControl()

	// Windows Installer's directory controls share an indirect property so the
	// browse dialog edits the selected install directory without a custom action.
	browse := b.Dialog("BrowseDlg").
		WithTitle("[ProductName] Setup").
		WithSize(370, 270).
		WithFirstControl("DirectoryCombo").
		WithDefaultControl("OK").
		WithCancelControl("Cancel")
	browse.Control("Title", msi.ControlText).At(15, 6).Size(280, 15).
		WithAttributes(msi.ControlVisible, msi.ControlTransparent, msi.ControlNoPrefix).
		WithText("{\\DlgFontBold}Choose the installation folder").EndControl()
	browse.Control("Description", msi.ControlText).At(25, 23).Size(320, 18).
		WithAttributes(msi.ControlVisible, msi.ControlTransparent, msi.ControlNoPrefix).
		WithText("Select a folder for SSH KeySelect.").EndControl()
	browse.Control("LookInLabel", msi.ControlText).At(25, 58).Size(44, 10).
		WithText("&Look in:").EndControl()
	browse.Control("DirectoryCombo", msi.ControlDirectoryCombo).At(70, 55).Size(220, 80).
		WithProperty("_BrowseProperty").
		WithAttributes(msi.ControlVisible, msi.ControlEnabled, msi.ControlIndirect, directoryComboFixedVolume, directoryComboRemoteVolume).
		TabNext("Up").
		Subscribe("IgnoreChange", "IgnoreChange").EndControl()
	browse.Control("Up", msi.ControlPushButton).At(298, 55).Size(19, 19).
		WithText("Up").TabNext("NewFolder").
		OnEvent("DirectoryListUp", "0", "").EndControl()
	browse.Control("NewFolder", msi.ControlPushButton).At(325, 55).Size(35, 19).
		WithText("New").TabNext("DirectoryList").
		OnEvent("DirectoryListNew", "0", "").EndControl()
	browse.Control("DirectoryList", msi.ControlDirectoryList).At(25, 83).Size(320, 98).
		WithProperty("_BrowseProperty").
		WithAttributes(msi.ControlVisible, msi.ControlEnabled, msi.ControlSunken, msi.ControlIndirect).
		TabNext("PathEdit").EndControl()
	browse.Control("PathLabel", msi.ControlText).At(25, 190).Size(320, 10).
		WithText("&Folder name:").EndControl()
	browse.Control("PathEdit", msi.ControlPathEdit).At(25, 202).Size(320, 18).
		WithProperty("_BrowseProperty").
		WithAttributes(msi.ControlVisible, msi.ControlEnabled, msi.ControlIndirect).
		TabNext("OK").EndControl()
	browse.Control("OK", msi.ControlPushButton).At(240, 243).Size(56, 17).
		TabNext("Cancel").WithText("OK").
		OnEvent("SetTargetPath", "[_BrowseProperty]", "").
		OnEvent("EndDialog", "Return", "").EndControl()
	browse.Control("Cancel", msi.ControlPushButton).At(304, 243).Size(56, 17).
		TabNext("DirectoryCombo").WithText("Cancel").
		OnEvent("Reset", "0", "").
		OnEvent("EndDialog", "Return", "").EndControl()

	// A modeless progress dialog returns control to MSI so ExecuteAction can run.
	progress := b.Dialog("ProgressDlg").
		WithTitle("[ProductName] Setup").
		WithAttributes(msi.DialogVisible, msi.DialogTrackDiskSpace).
		WithFirstControl("Cancel").
		WithDefaultControl("Cancel").
		WithCancelControl("Cancel").
		ScheduleInUI(1298, "")
	progress.Control("Title", msi.ControlText).At(20, 15).Size(330, 20).
		WithText("{\\DlgFontBold}Installing [ProductName]").EndControl()
	progress.Control("StatusText", msi.ControlText).At(35, 100).Size(320, 20).
		Subscribe("ActionText", "Text").EndControl()
	progress.Control("ProgressBar", msi.ControlProgressBar).At(35, 125).Size(300, 12).
		WithAttributes(msi.ControlVisible).
		Subscribe("SetProgress", "Progress").EndControl()
	progress.Control("Cancel", msi.ControlPushButton).At(304, 243).Size(56, 17).
		TabNext("Cancel").
		WithText("Cancel").OnEvent("EndDialog", "Exit", "").EndControl()

	// Keep the completion dialog compact and center its only action button.
	exit := b.Dialog("ExitDialog").
		WithTitle("[ProductName] Setup").
		WithSize(280, 105).
		WithFirstControl("Finish").
		WithDefaultControl("Finish").
		WithCancelControl("Finish").
		ScheduleInUI(-1, "")
	exit.Control("Title", msi.ControlText).At(15, 15).Size(250, 20).
		WithText("{\\DlgFontBold}Completed the [ProductName] Setup").EndControl()
	exit.Control("Description", msi.ControlText).At(15, 50).Size(250, 20).
		WithText("Click the Finish button to exit the Setup.").EndControl()
	exit.Control("Finish", msi.ControlPushButton).At(112, 78).Size(56, 17).
		TabNext("Finish").
		WithText("&Finish").OnEvent("EndDialog", "Return", "").EndControl()

	fatal := b.Dialog("FatalError").
		WithTitle("[ProductName] Setup").
		WithFirstControl("Finish").
		WithDefaultControl("Finish").
		WithCancelControl("Finish").
		ScheduleInUI(-3, "")
	fatal.Control("Title", msi.ControlText).At(15, 15).Size(340, 20).
		WithText("{\\DlgFontBold}[ProductName] Setup ended prematurely").EndControl()
	fatal.Control("Description", msi.ControlText).At(15, 50).Size(340, 40).
		WithText("[ProductName] setup ended prematurely because of an error.").EndControl()
	fatal.Control("Finish", msi.ControlPushButton).At(236, 243).Size(56, 17).
		TabNext("Finish").
		WithText("&Finish").OnEvent("EndDialog", "Exit", "").EndControl()

	userExit := b.Dialog("UserExit").
		WithTitle("[ProductName] Setup").
		WithFirstControl("Finish").
		WithDefaultControl("Finish").
		WithCancelControl("Finish").
		ScheduleInUI(-2, "")
	userExit.Control("Title", msi.ControlText).At(15, 15).Size(340, 20).
		WithText("{\\DlgFontBold}[ProductName] Setup was interrupted").EndControl()
	userExit.Control("Description", msi.ControlText).At(15, 50).Size(340, 40).
		WithText("[ProductName] setup was interrupted. No changes were made.").EndControl()
	userExit.Control("Finish", msi.ControlPushButton).At(236, 243).Size(56, 17).
		TabNext("Finish").
		WithText("&Finish").OnEvent("EndDialog", "Exit", "").EndControl()

	// ErrorDialog is the generic MSI error UI; FatalError remains a separate
	// terminal dialog shown for failures that abort the whole installation.
	error := b.Dialog("ErrorDlg").
		WithTitle("[ProductName] Setup").
		WithAttributes(msi.DialogVisible, msi.DialogModal, msi.DialogError).
		WithFirstControl("ErrorText")
	error.Control("ErrorText", msi.ControlText).At(48, 15).Size(205, 60).
		WithAttributes(msi.ControlVisible, msi.ControlNoPrefix).
		WithText("An installation error occurred.").EndControl()
	buttons := []struct {
		id, label, result string
	}{
		{"Y", "&Yes", "ErrorYes"},
		{"A", "&Abort", "ErrorAbort"},
		{"C", "&Cancel", "ErrorCancel"},
		{"I", "&Ignore", "ErrorIgnore"},
		{"N", "&No", "ErrorNo"},
		{"O", "OK", "ErrorOk"},
		{"R", "&Retry", "ErrorRetry"},
	}
	// Windows Installer creates only the buttons needed for each error, so these
	// controls must not link to one another through the tab-order column.
	for _, button := range buttons {
		error.Control(button.id, msi.ControlPushButton).At(100, 80).Size(56, 17).
			WithText(button.label).
			OnEvent("EndDialog", button.result, "").EndControl()
	}

	return b
}

// licenseAsRTF wraps the plain-text license in the RichEdit format required by
// Windows Installer's ScrollableText control.
func licenseAsRTF(license string) string {
	var out strings.Builder
	out.WriteString(`{\rtf1\ansi\ansicpg1252\deff0\deflang1033{\fonttbl{\f0\fnil\fcharset0 Tahoma;}}\viewkind4\uc1\pard\f0\fs16 `)
	license = strings.ReplaceAll(strings.ReplaceAll(license, "\r\n", "\n"), "\r", "\n")
	headingLines := 0
	leftAlignmentSet := false
	for _, line := range strings.Split(license, "\n") {
		if strings.TrimSpace(line) != "" && headingLines < 3 {
			if headingLines == 0 {
				out.WriteString(`\qc `)
			}
			line = strings.TrimLeft(line, " \t")
			headingLines++
		} else if headingLines == 3 && !leftAlignmentSet {
			// Center the license heading while keeping its legal text left-aligned.
			out.WriteString(`\ql `)
			leftAlignmentSet = true
		}

		for _, r := range line {
			switch r {
			case '\\', '{', '}':
				out.WriteByte('\\')
				out.WriteRune(r)
			case '\t':
				out.WriteString(`\tab `)
			default:
				if r >= 0x20 && r <= 0x7e {
					out.WriteRune(r)
					continue
				}
				for _, codeUnit := range utf16.Encode([]rune{r}) {
					value := int(codeUnit)
					if value >= 0x8000 {
						value -= 0x10000
					}
					out.WriteString(`\u`)
					out.WriteString(strconv.Itoa(value))
					out.WriteByte('?')
				}
			}
		}
		out.WriteString(`\par`)
		out.WriteByte('\n')
	}
	out.WriteByte('}')
	return out.String()
}
