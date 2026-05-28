package preview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
)

// renderKitty emits the Kitty graphics protocol "f=100" (PNG) inline
// transmission with action=transmit-and-display. PNG bytes are
// base64-encoded and chunked into 4096-byte pieces, each terminated
// with "m=1" (more) except the last which carries "m=0".
//
//	\x1b_Ga=T,f=100,m=1;<chunk>\x1b\\
//	...
//	\x1b_Gm=0;<last chunk>\x1b\\
func renderKitty(w io.Writer, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	const chunkSize = 4096
	for i := 0; i < len(encoded); i += chunkSize {
		end := i + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		more := 1
		if end == len(encoded) {
			more = 0
		}
		var header string
		if i == 0 {
			header = fmt.Sprintf("a=T,f=100,m=%d", more)
		} else {
			header = fmt.Sprintf("m=%d", more)
		}
		if _, err := fmt.Fprintf(w, "\x1b_G%s;%s\x1b\\", header, encoded[i:end]); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}
