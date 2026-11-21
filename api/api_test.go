package api

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	loremimage "lorem/image"
	"lorem/manager"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// newTestAPI creates an image directory with a single landscape portrait
// marker and returns an API serving it.
func newTestAPI(t *testing.T, w, h int, centerX, centerY float64) (*API, string) {
	t.Helper()

	dir := t.TempDir()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF})
		}
	}
	cx, cy := int(centerX*float64(w)), int(centerY*float64(h))
	half := w / 10
	if h/10 < half {
		half = h / 10
	}
	if half < 1 {
		half = 1
	}
	for y := cy - half; y < cy+half; y++ {
		for x := cx - half; x < cx+half; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 0xFF, A: 0xFF})
		}
	}

	path := filepath.Join(dir, "portrait.jpg")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	m, err := manager.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	return New(m, &loremimage.Imaging{}, &Options{
		CacheFiles: true,
		MinWidth:   8,
		MaxWidth:   2000,
		MinHeight:  8,
		MaxHeight:  2000,
	}), dir
}

func serve(a *API, query string) *httptest.ResponseRecorder {
	r := mux.NewRouter()
	r.HandleFunc("/image/{category}", a.SizeHandler)
	r.HandleFunc("/image", a.SizeHandler)

	req := httptest.NewRequest(http.MethodGet, "/image?"+query, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("response is not a JPEG: %v", err)
	}
	return img
}

func TestFocusCropServesRightFocusedPortrait(t *testing.T) {
	a, _ := newTestAPI(t, 400, 200, 0.8, 0.5)

	center := serve(a, "w=100&h=100")
	right := serve(a, "w=100&h=100&fx=0.8&fy=0.5")

	if center.Code != http.StatusOK || right.Code != http.StatusOK {
		t.Fatalf("status = %d/%d, want 200", center.Code, right.Code)
	}

	for _, rec := range []*httptest.ResponseRecorder{center, right} {
		if got := rec.Header().Get("Content-Type"); got != "image/jpeg" {
			t.Fatalf("Content-Type = %q, want image/jpeg", got)
		}
		if rec.Header().Get("Content-Length") == "" {
			t.Fatal("Content-Length header is missing")
		}
	}

	centerImg := decode(t, center)
	rightImg := decode(t, right)
	if b := centerImg.Bounds(); b.Dx() != 100 || b.Dy() != 100 {
		t.Fatalf("center bounds = %v", b)
	}
	if b := rightImg.Bounds(); b.Dx() != 100 || b.Dy() != 100 {
		t.Fatalf("right bounds = %v", b)
	}

	// The subject is at 80% of the source width. After a center crop it is
	// outside the window; after a right-focused crop the window is clamped to
	// the right edge, so the subject lands at 60% of the output width.
	if isRed(centerImg.At(50, 50)) {
		t.Fatal("center crop should not contain the right-side subject at (50,50)")
	}
	if !isRed(rightImg.At(60, 50)) {
		t.Fatalf("right-focused crop at (60,50) = %v, want red subject", rightImg.At(60, 50))
	}
	if isRed(rightImg.At(10, 50)) {
		t.Fatalf("right-focused crop at (10,50) = %v, want white background", rightImg.At(10, 50))
	}
	if bytes.Equal(center.Body.Bytes(), right.Body.Bytes()) {
		t.Fatal("different focal points returned identical bodies")
	}
}

func isRed(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r > 0xF000 && g < 0x1000 && b < 0x1000
}

func TestFocusCacheIsolationAndNormalization(t *testing.T) {
	a, dir := newTestAPI(t, 400, 200, 0.8, 0.5)
	cacheRoot := filepath.Join(dir, manager.CacheDir)

	if rec := serve(a, "w=100&h=100&fx=0.8&fy=0.5"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec := serve(a, "w=100&h=100&fx=0.2&fy=0.5"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	entries := []string{
		filepath.Join(cacheRoot, "100x100", "focus", "0.8x0.5", "portrait.jpg"),
		filepath.Join(cacheRoot, "100x100", "focus", "0.2x0.5", "portrait.jpg"),
	}
	for _, e := range entries {
		if _, err := os.Stat(e); err != nil {
			t.Fatalf("missing cache entry %s: %v", e, err)
		}
	}

	// No-focus requests keep the legacy cache location.
	if rec := serve(a, "w=100&h=100"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "100x100", "portrait.jpg")); err != nil {
		t.Fatalf("missing legacy cache entry: %v", err)
	}

	// Equivalent parameter spellings must reuse the same cache entry: a
	// rewritten entry proves the second request read it instead of rendering.
	equiv := filepath.Join(cacheRoot, "100x100", "focus", "0.8x0.5", "portrait.jpg")
	sentinel := []byte("sentinel")
	if err := os.WriteFile(equiv, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		"w=100&h=100&fx=0.80&fy=0.50",
		"w=100&h=100&focus-x=0.8&focus-y=0.5",
		"w=100&h=100&focus_x=0.8&focus_y=0.5",
		"w=100&h=100&fx=.8&fy=.5",
		"w=100&h=100&fx=0.8&fx=0.80&fy=0.5",
		"w=100&h=100&FOCUS_X=0.8&focus_y=0.5",
		"w=100&h=100&fpx=0.8&fpy=0.5",
		"w=100&h=100&focal-x=0.8&focal-y=0.5",
		"w=100&h=100&focalPointX=0.8&focalPointY=0.5",
		"w=100&h=100&focus=0.8,0.5",
		"w=100&h=100&focal=0.80,0.50",
		"w=100&h=100&focus=0.8,0.5&focus-x=0.8&fy=0.5",
	} {
		rec := serve(a, q)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", q, rec.Code)
		}
		if !bytes.Equal(rec.Body.Bytes(), sentinel) {
			t.Fatalf("%s: did not reuse the canonical cache entry", q)
		}
	}

	// A different focus must not reuse it.
	if rec := serve(a, "w=100&h=100&fx=0.7&fy=0.5"); bytes.Equal(rec.Body.Bytes(), sentinel) {
		t.Fatal("different focus reused the 0.8x0.5 cache entry")
	}
}

func TestFocusCacheKeyIsShortAndUnique(t *testing.T) {
	tests := []struct {
		fx, fy float64
		want   string
	}{
		{0.8, 0.5, "0.8x0.5"},
		{0, 1, "0x1"},
		{1e-300, 0.123456789012345, "1e-300x0.123456789012345"},
	}
	for _, tc := range tests {
		key := focusKey(tc.fx, tc.fy)
		if key != tc.want {
			t.Fatalf("focusKey(%v, %v) = %q, want %q", tc.fx, tc.fy, key, tc.want)
		}
		if len(key) > 64 {
			t.Fatalf("focusKey(%v, %v) is %d bytes, too long for a path component", tc.fx, tc.fy, len(key))
		}
	}

	// Distinct values must never collide.
	seen := make(map[string]struct{})
	for _, fx := range []float64{0, 0.1, 0.5, 0.5000000000000001, 0.8, 1} {
		key := focusKey(fx, 0.5)
		if _, ok := seen[key]; ok {
			t.Fatalf("cache key collision for focus %v: %q", fx, key)
		}
		seen[key] = struct{}{}
	}
}

func TestFocusCenterSharesDefaultCache(t *testing.T) {
	a, dir := newTestAPI(t, 400, 200, 0.8, 0.5)

	if rec := serve(a, "w=100&h=100"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	legacy := filepath.Join(dir, manager.CacheDir, "100x100", "portrait.jpg")
	sentinel := []byte("sentinel")
	if err := os.WriteFile(legacy, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		"w=100&h=100&fx=0.5&fy=0.5",
		"w=100&h=100&fx=0.50&fy=.5",
		"w=100&h=100&fy=0.5",
		"w=100&h=100&focus=0.5,0.5",
		"w=100&h=100&focus=0.5,.50&fx=0.5",
	} {
		rec := serve(a, q)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", q, rec.Code)
		}
		if !bytes.Equal(rec.Body.Bytes(), sentinel) {
			t.Fatalf("%s: center focus did not share the default cache entry", q)
		}
	}
}

func TestFocusBadRequests(t *testing.T) {
	a, _ := newTestAPI(t, 400, 200, 0.8, 0.5)

	for _, q := range []string{
		"w=100&h=100&fx=1.1&fy=0.5",
		"w=100&h=100&fx=-0.1&fy=0.5",
		"w=100&h=100&fx=abc&fy=0.5",
		"w=100&h=100&fx=NaN&fy=0.5",
		"w=100&h=100&fx=Inf&fy=0.5",
		"w=100&h=100&fx=+Inf&fy=0.5",
		"w=100&h=100&fx=0.8&fy=2",
		"w=100&h=100&fx=0.2&fx=0.8&fy=0.5",
		"w=100&h=100&fx=0.8&focus-x=0.3&fy=0.5",
		"w=100&h=100&focus=0.8,0.5&fx=0.2",
		"w=100&h=100&focus=0.8,abc",
		"w=100&h=100&focus=0.8",
		"w=100&h=100&focus=0.8,0.5,0.2",
		"w=100&h=100&focus=,0.5,",
		"w=100&fx=0.8&fy=0.5", // focus without a height cannot be cropped
		"h=100&fx=0.8&fy=0.5",
	} {
		rec := serve(a, q)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400", q, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Fatalf("%s: Content-Type = %q, want JSON", q, got)
		}
		if !strings.Contains(rec.Body.String(), "invalid focus") &&
			!strings.Contains(rec.Body.String(), "focus requires both width and height") {
			t.Fatalf("%s: body = %s", q, rec.Body.String())
		}
	}
}

func TestFocusWorksWithOnlyOneAxis(t *testing.T) {
	a, _ := newTestAPI(t, 200, 400, 0.5, 0.8)

	rec := serve(a, "w=100&h=100&fy=0.8")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !isRed(decode(t, rec).At(50, 60)) {
		// Horizontal focus defaults to center; the vertical focus puts the
		// bottom subject at 60% of the output height (the window is clamped to
		// the bottom edge).
		t.Fatal("expected the bottom subject in the default center-x crop")
	}
}

func TestFocusExtremeRatiosAndTinyImages(t *testing.T) {
	a, _ := newTestAPI(t, 400, 200, 0.8, 0.5)

	for _, tc := range []struct {
		query string
		w, h  int
	}{
		{"w=8&h=2000&fx=1&fy=1", 8, 2000},
		{"w=2000&h=8&fx=0&fy=0", 2000, 8},
		{"w=8&h=8&fx=0.8&fy=0.5", 8, 8},
	} {
		rec := serve(a, tc.query)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.query, rec.Code)
		}
		if b := decode(t, rec).Bounds(); b.Dx() != tc.w || b.Dy() != tc.h {
			t.Fatalf("%s: bounds = %v, want %dx%d", tc.query, b, tc.w, tc.h)
		}
	}
}

func TestFocusTinySourceImage(t *testing.T) {
	// A source smaller than the requested size is upscaled from the focused
	// window instead of failing or returning an empty image.
	a, _ := newTestAPI(t, 8, 8, 0.8, 0.5)

	for _, tc := range []struct {
		query string
		w, h  int
	}{
		{"w=2000&h=8&fx=1&fy=1", 2000, 8},
		{"w=8&h=2000&fx=0&fy=0", 8, 2000},
		{"w=8&h=8&fx=0.8&fy=0.5", 8, 8},
	} {
		rec := serve(a, tc.query)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", tc.query, rec.Code)
		}
		if b := decode(t, rec).Bounds(); b.Dx() != tc.w || b.Dy() != tc.h {
			t.Fatalf("%s: bounds = %v, want %dx%d", tc.query, b, tc.w, tc.h)
		}
	}
}
