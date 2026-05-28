package preview

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"
)

func sample(t *testing.T) image.Image {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	img.SetRGBA(1, 0, color.RGBA{0, 255, 0, 255})
	img.SetRGBA(0, 1, color.RGBA{0, 0, 255, 255})
	img.SetRGBA(1, 1, color.RGBA{255, 255, 0, 255})
	return img
}

func TestDetectNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if got := Detect(); got != ProtocolANSI {
		t.Fatalf("NO_COLOR should force ANSI, got %v", got)
	}
}

func TestDetectKitty(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("ITERM_SESSION_ID", "")
	if got := Detect(); got != ProtocolKitty {
		t.Fatalf("got %v", got)
	}
}

func TestDetectITerm2(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if got := Detect(); got != ProtocolITerm2 {
		t.Fatalf("got %v", got)
	}
}

func TestDetectFallback(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "")
	t.Setenv("KITTY_WINDOW_ID", "")
	t.Setenv("ITERM_SESSION_ID", "")
	if got := Detect(); got != ProtocolANSI {
		t.Fatalf("got %v", got)
	}
}

func TestRenderANSI(t *testing.T) {
	var b bytes.Buffer
	proto, err := Render(&b, sample(t), Options{Force: ProtocolANSI})
	if err != nil {
		t.Fatal(err)
	}
	if proto != ProtocolANSI {
		t.Fatalf("proto = %v", proto)
	}
	out := b.String()
	if !strings.Contains(out, "\x1b[38;2;255;0;0m") {
		t.Fatal("missing red foreground escape")
	}
	if !strings.Contains(out, "▀") {
		t.Fatal("missing half-block char")
	}
}

func TestRenderITerm2(t *testing.T) {
	var b bytes.Buffer
	if _, err := Render(&b, sample(t), Options{Force: ProtocolITerm2}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.HasPrefix(out, "\x1b]1337;File=inline=1;") {
		t.Fatalf("missing iTerm2 prefix: %q", out[:40])
	}
}

func TestRenderKitty(t *testing.T) {
	var b bytes.Buffer
	if _, err := Render(&b, sample(t), Options{Force: ProtocolKitty}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.HasPrefix(out, "\x1b_Ga=T,f=100,") {
		t.Fatalf("missing kitty prefix: %q", out[:40])
	}
}
