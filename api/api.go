package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"image/jpeg"
	"io/ioutil"
	"log"
	"lorem/image"
	"lorem/manager"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Options struct {
	CacheFiles bool

	MinWidth  int
	MaxWidth  int
	MinHeight int
	MaxHeight int

	CDN string
}

const (
	minSize    = 8
	maxSize    = 2000
	normalSize = 500
)

// Focus query parameters. "fx"/"fy" are the canonical names; longer aliases
// are accepted for readability. Names are matched case-insensitively and
// ignore '-' and '_', so fx, focusX, focus-x, focus_x and focal-x are all the
// same parameter. Both coordinates can also be passed together as
// "focus=<fx>,<fy>". All spellings are normalized to the same cache entry.
var (
	focusXParams = []string{"fx", "focusx", "focalx", "fpx", "focalpointx"}
	focusYParams = []string{"fy", "focusy", "focaly", "fpy", "focalpointy"}

	focusPairParams = []string{"focus", "focal", "focuspoint", "focalpoint"}
)

var errInvalidFocus = errors.New("invalid focus")

type API struct {
	mngr *manager.Manager
	pr   image.Processor
	opt  *Options
}

func New(manager *manager.Manager, imageProcessor image.Processor, options *Options) *API {
	if options == nil {
		options = &Options{
			CacheFiles: true,
			MinWidth:   minSize,
			MinHeight:  minSize,
			MaxWidth:   maxSize,
			MaxHeight:  maxSize,
		}
	} else {
		if options.MinWidth == 0 {
			options.MinWidth = minSize
		}

		if options.MinHeight == 0 {
			options.MinHeight = minSize
		}

		if options.MaxWidth == 0 {
			options.MaxWidth = maxSize
		}

		if options.MaxHeight == 0 {
			options.MaxHeight = maxSize
		}
	}

	return &API{
		mngr: manager,
		pr:   imageProcessor,
		opt:  options,
	}
}

func (a *API) SizeHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)

	file := a.mngr.Pick(vars["category"])
	if file == "" {
		handleError(w, http.StatusNotFound, "category not found")
		return
	}

	params := r.URL.Query()
	var err error
	var width int
	if params.Get("w") != "" {
		width, err = strconv.Atoi(params.Get("w"))
		if err != nil || width < a.opt.MinWidth || width > a.opt.MaxWidth {
			handleError(w, http.StatusBadRequest, "invalid width size")
			return
		}
	}

	var height int
	if params.Get("h") != "" {
		height, err = strconv.Atoi(params.Get("h"))
		if err != nil || height < a.opt.MinHeight || height > a.opt.MaxHeight {
			handleError(w, http.StatusBadRequest, "invalid height size")
			return
		}
	}

	if width == 0 && height == 0 {
		width = normalSize
	}

	focusX, focusY, useFocus, err := parseFocus(params)
	if err != nil {
		handleError(w, http.StatusBadRequest, "invalid focus")
		return
	}

	// A focal point only changes the result when the image is cropped, which
	// requires both dimensions. Center is the same as not passing a focal
	// point, so it keeps working with plain resizing.
	if useFocus && (width == 0 || height == 0) {
		handleError(w, http.StatusBadRequest, "focus requires both width and height")
		return
	}

	if a.opt.CacheFiles {
		cp := cachePath(file, width, height, focusX, focusY)
		if _, err := os.Stat(cp); err == nil {
			log.Printf("cached file found: %s", file)

			if a.opt.CDN != "" { // redirect to cdn
				http.Redirect(w, r, fmt.Sprintf("%s/%s", a.opt.CDN, cp), 302)
				return
			}

			err = handleCacheFile(w, cp)
			if err == nil {
				return
			}
			log.Printf("error while writing cached file: %s", file)
		}
	}

	img, err := image.Decode(file)
	if err != nil {
		log.Printf("unable to decode image: %s : %s", file, err)
		handleError(w, http.StatusInternalServerError, "internal error")
		return
	}

	switch {
	case useFocus:
		fp, ok := a.pr.(image.FocusProcessor)
		if !ok {
			log.Printf("image processor does not support focus cropping")
			handleError(w, http.StatusInternalServerError, "internal error")
			return
		}
		img = fp.CropFocus(*img, width, height, focusX, focusY)
	case height != 0 && width != 0:
		img = a.pr.CropCenter(*img, width, height)
	default:
		img = a.pr.Resize(*img, width, height)
	}

	buffer := new(bytes.Buffer)
	if err := jpeg.Encode(buffer, *img, nil); err != nil {
		log.Printf("unable to encode image: %s", err)
		handleError(w, http.StatusInternalServerError, "internal error")
		return
	}

	if a.opt.CacheFiles {
		if a.opt.CDN != "" { // store file & redirect to cdn
			cp := cachePath(file, width, height, focusX, focusY)
			err := cacheFile(cp, buffer.Bytes())
			if err != nil {
				log.Printf("error while caching file: %s : %v", cp, err)
				handleError(w, http.StatusInternalServerError, "internal error")
				return
			}

			http.Redirect(w, r, fmt.Sprintf("%s/%s", a.opt.CDN, cp), 302)
			return
		}

		// Store the file before responding so that a completed request always
		// has a complete cache entry. cacheFile writes atomically, which keeps
		// concurrent requests from reading a partially written file.
		cp := cachePath(file, width, height, focusX, focusY)
		if err := cacheFile(cp, buffer.Bytes()); err != nil {
			log.Printf("error while caching file: %s : %v", cp, err)
		} else {
			log.Printf("file cached: %s", cp)
		}
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(buffer.Bytes())))
	if _, err := w.Write(buffer.Bytes()); err != nil {
		log.Printf("unable to write image: %v", err)
		return
	}
}

func (a *API) NotFound(w http.ResponseWriter, r *http.Request) {
	handleError(w, http.StatusNotFound, "not found")
}

// parseFocus reads a normalized [0, 1] focal point from the query string.
// Either axis can be given on its own (the other defaults to center) or both
// together as "focus=<fx>,<fy>". Empty values are treated as unspecified.
// When the same axis is given several times, under aliases or combined with a
// single-axis parameter, all values must agree.
func parseFocus(params url.Values) (focusX, focusY float64, set bool, err error) {
	var ax, ay focusValue

	for key, values := range params {
		name := normalizeParamName(key)
		switch {
		case contains(focusXParams, name):
			for _, raw := range values {
				if err = ax.add(raw); err != nil {
					return 0, 0, false, err
				}
			}
		case contains(focusYParams, name):
			for _, raw := range values {
				if err = ay.add(raw); err != nil {
					return 0, 0, false, err
				}
			}
		case contains(focusPairParams, name):
			for _, raw := range values {
				if strings.TrimSpace(raw) == "" {
					continue
				}
				parts := strings.Split(raw, ",")
				if len(parts) != 2 {
					return 0, 0, false, errInvalidFocus
				}
				if err = ax.add(parts[0]); err != nil {
					return 0, 0, false, err
				}
				if err = ay.add(parts[1]); err != nil {
					return 0, 0, false, err
				}
			}
		}
	}

	if !ax.set {
		ax.value = image.DefaultFocus
	}
	if !ay.set {
		ay.value = image.DefaultFocus
	}

	return ax.value, ay.value, ax.set || ay.set, nil
}

// focusValue accumulates one axis of the focal point while requiring every
// occurrence to agree.
type focusValue struct {
	value float64
	set   bool
}

func (f *focusValue) add(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
		return errInvalidFocus
	}

	// Normalize negative zero so that "-0" and "0" share a cache entry.
	if v == 0 {
		v = 0
	}

	if f.set && v != f.value {
		return errInvalidFocus
	}
	f.value, f.set = v, true
	return nil
}

func normalizeParamName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")
	return name
}

func contains(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func handleCacheFile(w http.ResponseWriter, file string) error {
	s, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}

	b := bytes.NewBuffer(s)

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Content-Length", strconv.Itoa(len(b.Bytes())))
	if _, err := w.Write(b.Bytes()); err != nil {
		return err
	}
	return nil
}

func handleError(w http.ResponseWriter, code int, msg string) {
	b, err := json.Marshal(&Error{
		Message: msg,
	})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("error while marshaling response: %s", err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	w.Write(b)
	return
}

func cacheFile(cp string, i []byte) error {
	dir := path(cp)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return err
		}
	}

	// Write to a temporary file in the same directory and rename it into
	// place, so readers never observe a partially written cache entry.
	f, err := os.CreateTemp(dir, "."+fmt.Sprintf("%d-*", os.Getpid()))
	if err != nil {
		return err
	}
	tmpName := f.Name()

	if _, err = f.Write(i); err != nil {
		f.Close()
		os.Remove(tmpName)
		return err
	}

	if err = f.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}

	// Match the permissions os.Create would have produced, so cached files
	// served directly by a CDN or another process stay readable.
	if err = os.Chmod(tmpName, 0644); err != nil {
		os.Remove(tmpName)
		return err
	}

	if err = os.Rename(tmpName, cp); err != nil {
		os.Remove(tmpName)
		return err
	}

	return nil
}

func cachePath(file string, width, height int, focusX, focusY float64) string {
	tmp := strings.Split(file, "/")

	size := fmt.Sprintf("%dx%d", width, height)
	if focusX != image.DefaultFocus || focusY != image.DefaultFocus {
		size = fmt.Sprintf("%s/focus/%s", size, focusKey(focusX, focusY))
	}

	tmp[len(tmp)-1] = fmt.Sprintf("%s/%s/%s", manager.CacheDir, size, tmp[len(tmp)-1])
	return strings.Join(tmp, "/")
}

// focusKey returns the canonical cache directory name for a focal point.
func focusKey(focusX, focusY float64) string {
	return fmt.Sprintf("%sx%s", formatFocus(focusX), formatFocus(focusY))
}

// formatFocus is the canonical representation used in cache paths, so that
// equivalent parameters such as "0.5", "0.50" and ".5" share an entry. The
// shortest round-tripping form also keeps tiny values such as 1e-300 from
// producing path components longer than the filesystem allows.
func formatFocus(focus float64) string {
	if focus == 0 {
		focus = 0 // normalize negative zero
	}
	return strconv.FormatFloat(focus, 'g', -1, 64)
}

func path(file string) string {
	tmp := strings.Split(file, "/")
	return strings.Join(tmp[:len(tmp)-1], "/")
}
