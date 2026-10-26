package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"

	"github.com/fyne-io/image/ico"
	"github.com/nfnt/resize"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func renderSVG(path string, size int) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	icon, err := oksvg.ReadIconStream(bufio.NewReader(f))
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	rgba := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, rgba, rgba.Bounds())
	raster := rasterx.NewDasher(size, size, scanner)
	icon.Draw(raster, 1.0)
	return rgba, nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func writeICO(path string, imgs []image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return ico.Encode(f, imgs[0])
}

func main() {
	outDir := "assets"
	// Ensure dir exists
	_ = os.MkdirAll(outDir, 0755)

	// Render main logo
	mainSVG := filepath.Join(outDir, "logo.svg")
	faviconSVG := filepath.Join(outDir, "logo-favicon.svg")

	img512, err := renderSVG(mainSVG, 512)
	if err != nil {
		panic(err)
	}
	img256 := resize.Resize(256, 256, img512, resize.Lanczos3)
	img180 := resize.Resize(180, 180, img512, resize.Lanczos3)
	img128 := resize.Resize(128, 128, img512, resize.Lanczos3)

	if err := writePNG(filepath.Join(outDir, "logo-512.png"), img512); err != nil {
		panic(err)
	}
	if err := writePNG(filepath.Join(outDir, "logo-256.png"), img256); err != nil {
		panic(err)
	}
	if err := writePNG(filepath.Join(outDir, "logo-180.png"), img180); err != nil {
		panic(err)
	}
	if err := writePNG(filepath.Join(outDir, "logo-128.png"), img128); err != nil {
		panic(err)
	}
	if err := writePNG(filepath.Join(outDir, "apple-touch-icon-180.png"), img180); err != nil {
		panic(err)
	}

	// Favicon
	img32fav, err := renderSVG(faviconSVG, 32)
	if err != nil {
		// fallback
		img32fav = resize.Resize(32, 32, img512, resize.Lanczos3)
	}
	img16fav := resize.Resize(16, 16, img32fav, resize.Lanczos3)
	if err := writePNG(filepath.Join(outDir, "favicon-32.png"), img32fav); err != nil {
		panic(err)
	}
	if err := writePNG(filepath.Join(outDir, "favicon-16.png"), img16fav); err != nil {
		panic(err)
	}

	// ICO with multiple sizes
	if err := writeICO(filepath.Join(outDir, "logo.ico"), []image.Image{img32fav, img16fav}); err != nil {
		// try simple
		f, _ := os.Create(filepath.Join(outDir, "logo.ico"))
		if f != nil {
			_ = ico.Encode(f, img32fav)
			f.Close()
		}
	}

	// Also emit a simple manifest
	manifest := map[string]any{
		"generated": true,
		"sizes":     []int{16, 32, 128, 180, 256, 512},
	}
	b, _ := json.MarshalIndent(manifest, "", "  ")
	_ = os.WriteFile(filepath.Join(outDir, "manifest.json"), b, 0644)

	// Ensure we use all imports
	_ = binary.LittleEndian
	_ = bufio.NewScanner
	_ = bytes.Buffer{}
	_ = color.Alpha{}
	_ = draw.Draw
}
