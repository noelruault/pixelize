package decode

import (
	"fmt"
	"image"
	"os"
	"os/exec"
)

// decodeJXL invokes the djxl binary (libjxl) to convert a .jxl file
// to a temporary PNG and then decodes that PNG with stdlib.
//
// Returns a clear error when djxl is not installed so callers can
// surface a useful message to the user.
func decodeJXL(path string) (image.Image, string, error) {
	if _, err := exec.LookPath("djxl"); err != nil {
		return nil, "", fmt.Errorf("jxl input requires the djxl binary (libjxl) on PATH: %w", err)
	}

	tmp, err := os.CreateTemp("", "pixelize-jxl-*.png")
	if err != nil {
		return nil, "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	cmd := exec.Command("djxl", path, tmpPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, "", fmt.Errorf("djxl failed: %w: %s", err, string(out))
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, "", fmt.Errorf("decode djxl output: %w", err)
	}
	return img, "jxl", nil
}
