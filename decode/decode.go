// Package decode wraps image.Decode and registers extra formats
// (WebP, JXL) so the caller can hand off any supported file.
package decode

import (
	"fmt"
	"image"
	_ "image/gif"  // stdlib
	_ "image/jpeg" // stdlib
	_ "image/png"  // stdlib
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp" // pure-Go WebP decoder
)

// File opens path and decodes it as an image. JXL is handled via a
// djxl subprocess fallback (returns a clear error if djxl is missing).
func File(path string) (image.Image, string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".jxl" {
		return decodeJXL(path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	return Reader(f)
}

// Reader decodes from a reader using the registered decoders.
// Returns the decoded image and the detected format ("png", "jpeg",
// "gif", "webp").
func Reader(r io.Reader) (image.Image, string, error) {
	img, format, err := image.Decode(r)
	if err != nil {
		return nil, "", fmt.Errorf("decode: %w", err)
	}
	return img, format, nil
}
