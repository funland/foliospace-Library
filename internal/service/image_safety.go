package service

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"math"
)

const (
	maxThumbnailSourceBytes            = 32 << 20
	maxPageTransformSourceBytes        = 32 << 20
	maxDecodedImagePixels        int64 = 32_000_000
	maxPageTransformOutputPixels       = 24_000_000
	maxImageDimension                  = 32_768
)

func readBoundedImage(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("image source exceeds %d MiB limit", maxBytes>>20)
	}
	return data, nil
}

func decodeBoundedImage(data []byte, maxPixels int64) (image.Image, string, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if err := validateImageDimensions(config.Width, config.Height, maxPixels); err != nil {
		return nil, format, err
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	return img, format, err
}

func validateImageDimensions(width int, height int, maxPixels int64) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("invalid image dimensions %dx%d", width, height)
	}
	if width > maxImageDimension || height > maxImageDimension {
		return fmt.Errorf("image dimensions %dx%d exceed %d pixel edge limit", width, height, maxImageDimension)
	}
	if int64(width)*int64(height) > maxPixels {
		return fmt.Errorf("image dimensions %dx%d exceed %d megapixel limit", width, height, maxPixels/1_000_000)
	}
	return nil
}

func boundedOutputWidth(requested int, sourceWidth int, sourceHeight int, maxPixels int64) int {
	if requested <= 0 || sourceWidth <= 0 || sourceHeight <= 0 {
		return 1
	}
	maxWidth := int(math.Sqrt(float64(maxPixels) * float64(sourceWidth) / float64(sourceHeight)))
	if maxWidth < 1 {
		maxWidth = 1
	}
	if requested > maxWidth {
		return maxWidth
	}
	return requested
}
