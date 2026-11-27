package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"os"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"golang.org/x/image/webp"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestQRAssetBytesPreserved(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		path string
		sum  string
	}{
		{"maya", mayaQRWebP, "assets/maya.webp", "0480510c7fe673a8e769dccd37c5f65f1bacdfae52893b7c0600bcdc06461112"},
		{"gcash", gcashQRWebP, "assets/gcash.webp", "6189ac97a550288f697a496d9039844fa45330d2512ce92f4782823d50af962b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sha256Hex(tc.data); got != tc.sum {
				t.Fatalf("embedded checksum = %s, want %s", got, tc.sum)
			}
			disk, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(disk, tc.data) {
				t.Fatal("embedded bytes differ from the asset file")
			}
			if got := sha256Hex(disk); got != tc.sum {
				t.Fatalf("file checksum = %s, want %s", got, tc.sum)
			}
		})
	}
}

func TestIconAsset(t *testing.T) {
	data, err := os.ReadFile("assets/lanmonitor.ico")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := sha256Hex(data), "15d1bd6937b8cbb20dc5d735e013571aee859f9b159353ce2567cd6198346aaa"; got != want {
		t.Fatalf("icon checksum = %s, want %s", got, want)
	}
	if len(data) < 6 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		t.Fatal("not an ICO file")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count != 7 {
		t.Fatalf("icon has %d images, want 7", count)
	}
	sizes := map[int]bool{}
	for i := 0; i < count; i++ {
		off := 6 + i*16
		w := int(data[off])
		h := int(data[off+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		if w != h {
			t.Fatalf("icon entry %d is %dx%d, want square", i, w, h)
		}
		sizes[w] = true
	}
	for _, size := range []int{16, 24, 32, 48, 64, 128, 256} {
		if !sizes[size] {
			t.Errorf("icon is missing the %dpx image", size)
		}
	}
	if _, err := os.Stat("rsrc_windows_amd64.syso"); err != nil {
		t.Fatalf("icon resource object: %v", err)
	}
}

func TestQRImageDimensionsAndCrops(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		crop image.Rectangle
		want image.Rectangle
	}{
		{"maya", mayaQRWebP, mayaQRCrop, image.Rect(0, 0, 971, 1536)},
		{"gcash", gcashQRWebP, gcashQRCrop, image.Rect(0, 0, 770, 1536)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, err := webp.Decode(bytes.NewReader(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			if src.Bounds() != tc.want {
				t.Fatalf("bounds = %v, want %v", src.Bounds(), tc.want)
			}
			if !tc.crop.In(src.Bounds()) {
				t.Fatalf("crop %v outside image %v", tc.crop, src.Bounds())
			}
			if tc.crop.Dx() != tc.crop.Dy() {
				t.Fatalf("crop is %dx%d, want square", tc.crop.Dx(), tc.crop.Dy())
			}
			assertQuietBorder(t, src, tc.crop, 8)
		})
	}
}

func assertQuietBorder(t *testing.T, img image.Image, r image.Rectangle, band int) {
	t.Helper()
	for y := r.Min.Y; y < r.Min.Y+band; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !isLight(img, x, y) {
				t.Fatalf("top quiet zone is not light at (%d,%d)", x, y)
			}
		}
	}
	for y := r.Max.Y - band; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !isLight(img, x, y) {
				t.Fatalf("bottom quiet zone is not light at (%d,%d)", x, y)
			}
		}
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Min.X+band; x++ {
			if !isLight(img, x, y) {
				t.Fatalf("left quiet zone is not light at (%d,%d)", x, y)
			}
		}
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Max.X - band; x < r.Max.X; x++ {
			if !isLight(img, x, y) {
				t.Fatalf("right quiet zone is not light at (%d,%d)", x, y)
			}
		}
	}
}

func isLight(img image.Image, x, y int) bool {
	r, g, b, _ := img.At(x, y).RGBA()
	return (299*r+587*g+114*b)/1000 > 50000
}

func TestQRCodesDecodeAtDisplaySizes(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		crop image.Rectangle
	}{
		{"maya", mayaQRWebP, mayaQRCrop},
		{"gcash", gcashQRWebP, gcashQRCrop},
	}
	for _, tc := range cases {
		for _, size := range []int{240, 320, 480} {
			t.Run(tc.name, func(t *testing.T) {
				img, err := decodeQRImage(tc.data, tc.crop, size)
				if err != nil {
					t.Fatal(err)
				}
				if img.Bounds() != image.Rect(0, 0, size, size) {
					t.Fatalf("display bounds = %v", img.Bounds())
				}
				bmp, err := gozxing.NewBinaryBitmapFromImage(img)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := qrcode.NewQRCodeReader().Decode(bmp, nil); err != nil {
					t.Fatalf("QR did not decode at %dpx: %v", size, err)
				}
			})
		}
	}
}

func TestAboutText(t *testing.T) {
	if got, want := windowTitle(), "LAN Monitor — Version 3"; got != want {
		t.Fatalf("window title = %q, want %q", got, want)
	}
	if aboutQuote != "Gwapong Pilipino Taas Noo Kahit Kanino!" {
		t.Fatalf("quote = %q", aboutQuote)
	}
	if supportMessage != "This software is completely free. If you find it useful, consider supporting its development." {
		t.Fatalf("support message = %q", supportMessage)
	}
	if projectURL != "https://github.com/tamzrod/lanmonitor" {
		t.Fatalf("project URL = %q", projectURL)
	}
}
