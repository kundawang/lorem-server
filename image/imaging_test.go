package image

import (
	"image"
	"image/color"
	"math"
	"testing"
)

var (
	white = color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	red   = color.NRGBA{R: 0xFF, A: 0xFF}
)

// portrait returns a white image of the given size with a red square around
// the given normalized focal point. squareSize is in source pixels.
func portrait(w, h int, centerX, centerY float64, squareSize int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, white)
		}
	}

	cx, cy := int(centerX*float64(w)), int(centerY*float64(h))
	minX, maxX := cx-squareSize/2, cx+squareSize/2
	minY, maxY := cy-squareSize/2, cy+squareSize/2
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			img.SetNRGBA(x, y, red)
		}
	}
	return img
}

func TestFocusCropRectCoordinates(t *testing.T) {
	tests := []struct {
		name   string
		bounds image.Rectangle
		w, h   int
		fx, fy float64
		want   image.Rectangle
	}{
		{
			name:   "center of landscape keeps center window",
			bounds: image.Rect(0, 0, 400, 200),
			w:      100, h: 100,
			fx: 0.5, fy: 0.5,
			want: image.Rect(100, 0, 300, 200),
		},
		{
			name:   "right focus centers the subject",
			bounds: image.Rect(0, 0, 400, 200),
			w:      100, h: 100,
			fx: 0.4, fy: 0.5,
			want: image.Rect(60, 0, 260, 200),
		},
		{
			name:   "right focus is clamped to the right edge",
			bounds: image.Rect(0, 0, 400, 200),
			w:      100, h: 100,
			fx: 0.8, fy: 0.5,
			want: image.Rect(200, 0, 400, 200),
		},
		{
			name:   "left focus clamps to left edge",
			bounds: image.Rect(0, 0, 400, 200),
			w:      100, h: 100,
			fx: 0, fy: 0.5,
			want: image.Rect(0, 0, 200, 200),
		},
		{
			name:   "right focus clamps to right edge",
			bounds: image.Rect(0, 0, 400, 200),
			w:      100, h: 100,
			fx: 1, fy: 0.5,
			want: image.Rect(200, 0, 400, 200),
		},
		{
			name:   "bottom focus is clamped to the bottom edge",
			bounds: image.Rect(0, 0, 200, 400),
			w:      100, h: 100,
			fx: 0.5, fy: 0.8,
			want: image.Rect(0, 200, 200, 400),
		},
		{
			name:   "top focus clamps to top edge",
			bounds: image.Rect(0, 0, 200, 400),
			w:      100, h: 100,
			fx: 0.5, fy: 0,
			want: image.Rect(0, 0, 200, 200),
		},
		{
			name:   "matching aspect ratios keep whole image",
			bounds: image.Rect(0, 0, 400, 200),
			w:      200, h: 100,
			fx: 0.8, fy: 0.2,
			want: image.Rect(0, 0, 400, 200),
		},
		{
			name:   "non-zero bounds origin is preserved",
			bounds: image.Rect(10, 20, 410, 220),
			w:      100, h: 100,
			fx: 0.4, fy: 0.5,
			want: image.Rect(70, 20, 270, 220),
		},
		{
			name:   "wide target on tall image crops height",
			bounds: image.Rect(0, 0, 200, 400),
			w:      200, h: 100,
			fx: 0.5, fy: 0.5,
			want: image.Rect(0, 150, 200, 250),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FocusCropRect(tc.bounds, tc.w, tc.h, tc.fx, tc.fy)
			if got != tc.want {
				t.Fatalf("FocusCropRect() = %v, want %v", got, tc.want)
			}
			if !got.In(tc.bounds) {
				t.Fatalf("crop window %v is not inside source bounds %v", got, tc.bounds)
			}
		})
	}
}

func TestFocusCropRectExtremeRatiosAndTinyImages(t *testing.T) {
	tests := []struct {
		name   string
		bounds image.Rectangle
		w, h   int
		fx, fy float64
	}{
		{"very tall target", image.Rect(0, 0, 400, 200), 8, 2000, 0.5, 0.5},
		{"very wide target", image.Rect(0, 0, 200, 400), 2000, 8, 0.5, 0.5},
		{"one pixel source", image.Rect(0, 0, 1, 1), 8, 8, 0.8, 0.2},
		{"one pixel tall source", image.Rect(0, 0, 400, 1), 8, 2000, 1, 1},
		{"one pixel wide source", image.Rect(0, 0, 1, 400), 2000, 8, 0, 0},
		{"focus out of range is clamped", image.Rect(0, 0, 400, 200), 100, 100, 5, -5},
		{"non-finite focus falls back to center", image.Rect(0, 0, 400, 200), 100, 100, math.NaN(), math.Inf(1)},
		{"extreme target aspect ratio", image.Rect(0, 0, 400, 200), 2000, 8, 0.9, 0.1},
		{"extreme source aspect ratio", image.Rect(0, 0, 2000, 8), 8, 2000, 0.9, 0.1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FocusCropRect(tc.bounds, tc.w, tc.h, tc.fx, tc.fy)
			if got.Empty() {
				t.Fatalf("FocusCropRect() returned an empty window for %v", tc.bounds)
			}
			if got.Dx() > tc.bounds.Dx() || got.Dy() > tc.bounds.Dy() {
				t.Fatalf("crop window %v is larger than source bounds %v", got, tc.bounds)
			}
			if !got.In(tc.bounds) {
				t.Fatalf("crop window %v is not inside source bounds %v", got, tc.bounds)
			}
		})
	}
}

func TestFocusCropRectInvalidDestination(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{0, 100}, {100, 0}, {0, 0}, {-1, 100}, {100, -1}} {
		if got := FocusCropRect(image.Rect(0, 0, 400, 200), tc.w, tc.h, 0.5, 0.5); !got.Empty() {
			t.Fatalf("FocusCropRect(%dx%d) = %v, want empty", tc.w, tc.h, got)
		}
	}
	if got := FocusCropRect(image.Rectangle{}, 100, 100, 0.5, 0.5); !got.Empty() {
		t.Fatalf("FocusCropRect(empty source) = %v, want empty", got)
	}
}

func TestCropFocusKeepsRightFocusedPortrait(t *testing.T) {
	// Landscape source with the subject at 80% of the width.
	src := portrait(400, 200, 0.8, 0.5, 40)
	p := &Imaging{}

	center := *p.CropFocus(src, 100, 100, 0.5, 0.5)
	right := *p.CropFocus(src, 100, 100, 0.8, 0.5)

	if got := center.At(50, 50); got == red {
		t.Fatalf("center crop unexpectedly contains the subject at (50,50): %v", got)
	}
	if got := right.At(60, 50); got != red {
		// The window is clamped to the right edge (x=200..400), so the source
		// focal point at x=320 lands at x=120 of that window, i.e. x=60 in the
		// 100px wide result.
		t.Fatalf("right-focused crop at (60,50) = %v, want subject", got)
	}
	if got := right.At(10, 50); got != white {
		t.Fatalf("right-focused crop at (10,50) = %v, want background", got)
	}

	if right.At(60, 50) == center.At(60, 50) {
		t.Fatal("different focal points produced the same pixel")
	}
}

func TestCropFocusKeepsBottomFocusedPortrait(t *testing.T) {
	// Portrait source with the subject at 80% of the height.
	src := portrait(200, 400, 0.5, 0.8, 40)
	p := &Imaging{}

	square := *p.CropFocus(src, 100, 100, 0.5, 0.5)
	bottom := *p.CropFocus(src, 100, 100, 0.5, 0.8)

	if got := square.At(50, 50); got == red {
		t.Fatalf("center crop unexpectedly contains the subject at (50,50): %v", got)
	}
	if got := bottom.At(50, 60); got != red {
		t.Fatalf("bottom-focused crop at (50,60) = %v, want subject", got)
	}
	if got := bottom.At(50, 10); got != white {
		t.Fatalf("bottom-focused crop at (50,10) = %v, want background", got)
	}
}

func TestFocusCropRectCenterMatchesCenterAnchor(t *testing.T) {
	// imaging.Fill/CropCenter position the window at (size-src)/2. The focus
	// rect must pick the same window when the focal point is center so that
	// requests without a focus keep the old result.
	for _, src := range []image.Rectangle{image.Rect(0, 0, 400, 200), image.Rect(0, 0, 200, 400), image.Rect(3, 5, 401, 203)} {
		for _, size := range []struct{ w, h int }{{100, 100}, {300, 100}, {40, 200}, {8, 2000}, {2000, 8}} {
			got := FocusCropRect(src, size.w, size.h, DefaultFocus, DefaultFocus)

			srcW, srcH := src.Dx(), src.Dy()
			srcAspect := float64(srcW) / float64(srcH)
			dstAspect := float64(size.w) / float64(size.h)

			cropW, cropH := srcW, srcH
			if srcAspect > dstAspect {
				cropW = int(math.Round(float64(srcH) * dstAspect))
			} else {
				cropH = int(math.Round(float64(srcW) / dstAspect))
			}
			if cropW < 1 {
				cropW = 1
			}
			if cropH < 1 {
				cropH = 1
			}
			if cropW > srcW {
				cropW = srcW
			}
			if cropH > srcH {
				cropH = srcH
			}

			x := src.Min.X + (srcW-cropW)/2
			y := src.Min.Y + (srcH-cropH)/2
			want := image.Rect(x, y, x+cropW, y+cropH)

			if got != want {
				t.Fatalf("src=%v dst=%dx%d: rect = %v, want %v", src, size.w, size.h, got, want)
			}
		}
	}
}

func TestCropFocusOutputSize(t *testing.T) {
	p := &Imaging{}
	for _, src := range []*image.NRGBA{
		portrait(400, 200, 0.8, 0.5, 40),
		portrait(1, 1, 0.8, 0.5, 1),
		portrait(1, 400, 0.5, 0.8, 1),
		portrait(400, 1, 0.8, 0.5, 1),
	} {
		for _, size := range []struct{ w, h int }{{8, 8}, {8, 2000}, {2000, 8}, {100, 100}} {
			out := *p.CropFocus(src, size.w, size.h, 0.8, 0.5)
			if got := out.Bounds(); got.Dx() != size.w || got.Dy() != size.h {
				t.Fatalf("CropFocus(%dx%d) on %v bounds = %v", size.w, size.h, src.Bounds(), got)
			}
		}
	}
}
