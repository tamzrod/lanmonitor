package main

import (
	_ "embed"
	"image"
)

//go:generate rsrc -ico assets/lanmonitor.ico -o rsrc_windows_amd64.syso

//go:embed assets/maya.webp
var mayaQRWebP []byte

//go:embed assets/gcash.webp
var gcashQRWebP []byte

// The crops keep the full QR code and its white quiet zone. The source files
// are not modified; the About dialog scales these crops for display.
var (
	mayaQRCrop  = image.Rect(97, 414, 872, 1189)
	gcashQRCrop = image.Rect(133, 452, 636, 955)
)
