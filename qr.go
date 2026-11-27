package main

import (
	"bytes"
	"fmt"
	"image"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// decodeQRImage crops a QR code and scales it over a white background. The
// white fill keeps the quiet zone intact when the source has transparency.
func decodeQRImage(data []byte, crop image.Rectangle, size int) (*image.RGBA, error) {
	if size <= 0 {
		return nil, fmt.Errorf("invalid QR size %d", size)
	}
	if crop.Empty() {
		return nil, fmt.Errorf("empty QR crop")
	}
	src, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode QR image: %w", err)
	}
	if !crop.In(src.Bounds()) {
		return nil, fmt.Errorf("QR crop %v outside image %v", crop, src.Bounds())
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	return dst, nil
}
