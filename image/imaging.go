package image

import (
	"math"

	"github.com/disintegration/imaging"
	"image"
)

type Imaging struct{}

var (
	_ Processor      = (*Imaging)(nil)
	_ FocusProcessor = (*Imaging)(nil)
)

func (i *Imaging) CropCenter(img image.Image, width, height int) *image.Image {
	img = imaging.Fill(img, width, height, imaging.Center, imaging.Lanczos)
	return &img
}

// CropFocus crops the image to the requested aspect ratio around the given
// focal point and scales the result to width x height. focusX and focusY are
// normalized coordinates in the [0, 1] range; (0, 0) is the top-left corner
// and (1, 1) is the bottom-right corner.
//
// A center focal point produces exactly the same result as CropCenter, which
// keeps requests without a focal point backward compatible.
func (i *Imaging) CropFocus(img image.Image, width, height int, focusX, focusY float64) *image.Image {
	focusX = clampFocus(focusX)
	focusY = clampFocus(focusY)
	if focusX == DefaultFocus && focusY == DefaultFocus {
		return i.CropCenter(img, width, height)
	}

	rect := FocusCropRect(img.Bounds(), width, height, focusX, focusY)
	if rect.Empty() {
		empty := image.NewNRGBA(image.Rect(0, 0, 0, 0))
		var result image.Image = empty
		return &result
	}

	// Crop the source first so the focal point keeps its relative position,
	// then scale the crop to the requested size. FocusCropRect keeps the crop
	// window inside the source bounds, so no out-of-image pixels are used.
	cropped := imaging.Crop(img, rect)
	var result image.Image = imaging.Resize(cropped, width, height, imaging.Lanczos)
	return &result
}

func (i *Imaging) Resize(img image.Image, width, height int) *image.Image {
	img = imaging.Resize(img, width, height, imaging.Lanczos)
	return &img
}

// FocusCropRect returns the region of bounds that is cropped before scaling
// to width x height. The crop window has the requested aspect ratio, is fully
// contained in bounds, and is centered on the focal point (focusX, focusY) as
// far as the bounds allow. Near an edge the window is clamped to the image,
// which moves the focal point towards the center of the result.
//
// An empty rectangle is returned when the destination or source size is not
// positive. focus values are clamped to [0, 1]; non-finite values fall back to
// DefaultFocus.
func FocusCropRect(bounds image.Rectangle, width, height int, focusX, focusY float64) image.Rectangle {
	if width <= 0 || height <= 0 || bounds.Empty() {
		return image.Rectangle{}
	}

	srcW, srcH := bounds.Dx(), bounds.Dy()
	srcAspect := float64(srcW) / float64(srcH)
	dstAspect := float64(width) / float64(height)

	var cropW, cropH int
	if srcAspect > dstAspect {
		// The source is wider than the target: keep the full height and crop
		// the sides.
		cropH = srcH
		cropW = roundClamp(float64(srcH)*dstAspect, 1, srcW)
	} else {
		// The source is taller than (or as tall as) the target: keep the full
		// width and crop the top/bottom.
		cropW = srcW
		cropH = roundClamp(float64(srcW)/dstAspect, 1, srcH)
	}

	x := bounds.Min.X + focusOffset(srcW, cropW, focusX)
	y := bounds.Min.Y + focusOffset(srcH, cropH, focusY)
	return image.Rect(x, y, x+cropW, y+cropH)
}

// roundClamp rounds v to the nearest integer and clamps it to [min, max]. It
// works on the float value before converting so that extreme destination
// aspect ratios cannot overflow the integer conversion, and so that the short
// side of the crop never becomes zero.
func roundClamp(v float64, min, max int) int {
	if math.IsNaN(v) || v <= float64(min) {
		return min
	}
	if v >= float64(max) {
		return max
	}
	return int(math.Round(v))
}

// focusOffset maps a focal point to an integer offset for a crop window of
// cropSize pixels inside a source dimension of srcSize pixels. The window is
// centered on the focal point and then clamped to the source. A center focus
// picks the same offset as imaging.Center, which keeps the behavior of
// requests without a focal point identical to the previous center-crop
// implementation.
func focusOffset(srcSize, cropSize int, focus float64) int {
	span := srcSize - cropSize
	if span <= 0 {
		return 0
	}

	focus = clampFocus(focus)
	if focus == DefaultFocus {
		return span / 2
	}

	offset := focus*float64(srcSize) - float64(cropSize)/2
	if offset <= 0 {
		return 0
	}
	if offset >= float64(span) {
		return span
	}
	return int(math.Round(offset))
}

func clampFocus(focus float64) float64 {
	if math.IsNaN(focus) || math.IsInf(focus, 0) {
		return DefaultFocus
	}
	if focus < 0 {
		return 0
	}
	if focus > 1 {
		return 1
	}
	return focus
}
