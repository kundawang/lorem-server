package image

import (
	"image"
	"os"
)

// DefaultFocus is the normalized focal point used when the caller does not
// specify one. It corresponds to a center crop.
const DefaultFocus = 0.5

type Processor interface {
	CropCenter(img image.Image, width, height int) *image.Image
	Resize(img image.Image, width, height int) *image.Image
}

// FocusProcessor is implemented by processors that support cropping around an
// arbitrary focal point. It is optional so that existing Processor
// implementations keep working unchanged.
type FocusProcessor interface {
	CropFocus(img image.Image, width, height int, focusX, focusY float64) *image.Image
}

func Decode(name string) (*image.Image, error) {
	imagePath, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer imagePath.Close()

	img, _, err := image.Decode(imagePath)
	if err != nil {
		return nil, err
	}

	return &img, nil
}
