package preview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
)

// renderITerm2 emits the iTerm2 inline image protocol:
//
//	\x1b]1337;File=inline=1;width=auto;preserveAspectRatio=1:BASE64\x07
//
// Works in iTerm2 and WezTerm. The image is sent as PNG.
func renderITerm2(w io.Writer, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())
	_, err := fmt.Fprintf(w,
		"\x1b]1337;File=inline=1;width=auto;preserveAspectRatio=1:%s\x07\n",
		encoded,
	)
	return err
}
