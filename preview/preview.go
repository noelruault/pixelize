// Package preview renders an image to the terminal using whichever
// graphics protocol the terminal supports. Falls back to ANSI truecolor.
package preview

import (
	"image"
	"io"
)

// Protocol identifies a rendering backend.
type Protocol string

const (
	ProtocolANSI   Protocol = "ansi"
	ProtocolITerm2 Protocol = "iterm2"
	ProtocolKitty  Protocol = "kitty"
)

// Options configures preview rendering.
type Options struct {
	// MaxWidth caps the rendered width in terminal cells. Zero means
	// no cap (use the natural image width, scaled to cells for ANSI).
	MaxWidth int

	// Force selects a specific protocol. Empty means auto-detect.
	Force Protocol
}

// Render writes a terminal-visible representation of img to w.
// Returns the protocol that was actually used.
func Render(w io.Writer, img image.Image, opts Options) (Protocol, error) {
	protocol := opts.Force
	if protocol == "" {
		protocol = Detect()
	}

	switch protocol {
	case ProtocolITerm2:
		if err := renderITerm2(w, img); err != nil {
			return "", err
		}
		return ProtocolITerm2, nil
	case ProtocolKitty:
		if err := renderKitty(w, img); err != nil {
			return "", err
		}
		return ProtocolKitty, nil
	default:
		if err := renderANSI(w, img, opts.MaxWidth); err != nil {
			return "", err
		}
		return ProtocolANSI, nil
	}
}
