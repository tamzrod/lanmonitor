//go:build windows

package main

import (
	"image"
	"runtime"
	"unsafe"
)

const (
	imageBitmap = 0
	imageIcon   = 1
	lrShared    = 0x00008000

	ssIcon        = 0x00000003
	ssBitmap      = 0x0000000E
	ssCenterImage = 0x00000200

	stmSetIcon  = 0x0170
	stmSetImage = 0x0172

	smCXIcon   = 11
	smCYIcon   = 12
	smCXSmIcon = 49
	smCYSmIcon = 50

	biRGB        = 0
	dibRGBColors = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

// createQRControl creates a static bitmap control and returns the bitmap so the
// About window can delete it on WM_DESTROY. Static controls do not own bitmaps.
func createQRControl(parent uintptr, x, y, size int, data []byte, crop image.Rectangle, fallback string) uintptr {
	bmp := createQRBitmap(data, crop, px(size))
	if bmp == 0 {
		createChildControl(parent, "STATIC", fallback, ssCenter, x, y, size, size, 0)
		return 0
	}
	ctl := createChildControl(parent, "STATIC", "", ssBitmap, x, y, size, size, 0)
	if ctl == 0 {
		call(gdi32, "DeleteObject", bmp)
		return 0
	}
	call(user32, "SendMessageW", ctl, uintptr(stmSetImage), uintptr(imageBitmap), bmp)
	return bmp
}

func createQRBitmap(data []byte, crop image.Rectangle, size int) uintptr {
	img, err := decodeQRImage(data, crop, size)
	if err != nil {
		return 0
	}
	return createDIB(img)
}

func createDIB(img *image.RGBA) uintptr {
	bounds := img.Bounds()
	size := bounds.Dx()
	if size <= 0 || bounds.Dy() != size {
		return 0
	}
	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(size),
		Height:      -int32(size),
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	var bits uintptr
	h := call(gdi32, "CreateDIBSection", uintptr(0), unsafe.Pointer(&bi), uintptr(dibRGBColors), unsafe.Pointer(&bits), uintptr(0), uintptr(0))
	runtime.KeepAlive(&bi)
	if h == 0 || bits == 0 {
		if h != 0 {
			call(gdi32, "DeleteObject", h)
		}
		return 0
	}
	out := unsafe.Slice((*byte)(ptrFromUintptr(bits)), size*size*4)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := img.PixOffset(x, y)
			o := (y*size + x) * 4
			out[o] = img.Pix[i+2]
			out[o+1] = img.Pix[i+1]
			out[o+2] = img.Pix[i]
			out[o+3] = 255
		}
	}
	return h
}

func loadAppIcons() (uintptr, uintptr) {
	cx := int(call(user32, "GetSystemMetrics", uintptr(smCXIcon)))
	cy := int(call(user32, "GetSystemMetrics", uintptr(smCYIcon)))
	sx := int(call(user32, "GetSystemMetrics", uintptr(smCXSmIcon)))
	sy := int(call(user32, "GetSystemMetrics", uintptr(smCYSmIcon)))
	if cx <= 0 {
		cx = 32
	}
	if cy <= 0 {
		cy = 32
	}
	if sx <= 0 {
		sx = 16
	}
	if sy <= 0 {
		sy = 16
	}
	return loadIconSize(cx, cy), loadIconSize(sx, sy)
}

// loadIconSize loads icon resource 1 from the executable. rsrc writes the
// supplied .ico as RT_GROUP_ICON/RT_ICON resource 1.
func loadIconSize(cx, cy int) uintptr {
	if instance == 0 {
		return 0
	}
	h := call(user32, "LoadImageW", instance, uintptr(1), uintptr(imageIcon), uintptr(cx), uintptr(cy), uintptr(lrShared))
	if h == 0 {
		h = call(user32, "LoadIconW", instance, uintptr(1))
	}
	return h
}

func setWindowIcons(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	if appIcon != 0 {
		call(user32, "SendMessageW", hwnd, uintptr(wmSetIcon), uintptr(iconBig), appIcon)
	}
	if appIconSmall != 0 {
		call(user32, "SendMessageW", hwnd, uintptr(wmSetIcon), uintptr(iconSmall), appIconSmall)
	}
}

func deleteBitmap(h *uintptr) {
	if h != nil && *h != 0 {
		call(gdi32, "DeleteObject", *h)
		*h = 0
	}
}
