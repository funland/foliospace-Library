package service

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadBoundedImageRejectsOversizedInput(t *testing.T) {
	_, err := readBoundedImage(strings.NewReader("12345"), 4)
	if err == nil {
		t.Fatal("expected oversized image input to be rejected")
	}
}

func TestReadBoundedImageAcceptsInputAtLimit(t *testing.T) {
	got, err := readBoundedImage(bytes.NewReader([]byte("1234")), 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "1234" {
		t.Fatalf("got %q, want 1234", got)
	}
}

func TestValidateImageDimensionsRejectsMemoryBomb(t *testing.T) {
	if err := validateImageDimensions(12_000, 12_000, maxDecodedImagePixels); err == nil {
		t.Fatal("expected excessive decoded pixel count to be rejected")
	}
	if err := validateImageDimensions(2_000, 12_000, maxDecodedImagePixels); err != nil {
		t.Fatalf("expected ordinary long page to pass: %v", err)
	}
}

func TestBoundedOutputWidthCapsTallPageAllocation(t *testing.T) {
	got := boundedOutputWidth(2_000, 2_000, 30_000, maxPageTransformOutputPixels)
	if got >= 2_000 {
		t.Fatalf("bounded width = %d, want less than 2000", got)
	}
	height := 30_000 * got / 2_000
	if int64(got)*int64(height) > maxPageTransformOutputPixels {
		t.Fatalf("bounded output still exceeds pixel limit: %dx%d", got, height)
	}
}
