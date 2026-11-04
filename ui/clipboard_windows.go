//go:build gui && windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
)

// copyImageToSystem puts a picture on the Windows clipboard.
//
// PowerShell, because the alternative is cgo and the shell API: the .NET
// clipboard class takes an image and does the format conversion itself (#510).
// The process has to be single-threaded-apartment, which is what -STA asks for.
func copyImageToSystem(path string) error {
	picture, err := imagePNG(path)
	if err != nil {
		return err
	}

	file, err := os.CreateTemp("", "cthrone-picture-*.png")
	if err != nil {
		return err
	}

	name := file.Name()
	defer os.Remove(name)

	if _, err := file.Write(picture); err != nil {
		file.Close()

		return err
	}

	file.Close()

	script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$image = [System.Drawing.Image]::FromFile('%s')
[System.Windows.Forms.Clipboard]::SetImage($image)
$image.Dispose()`, name)

	return exec.Command("powershell", "-NoProfile", "-STA", "-Command", script).Run()
}
