package preview

import "os"

// Detect picks the best available terminal protocol based on env vars.
// Order: NO_COLOR off everything; Kitty; iTerm2; ANSI fallback.
func Detect() Protocol {
	if os.Getenv("NO_COLOR") != "" {
		return ProtocolANSI
	}
	if t := os.Getenv("TERM"); t == "xterm-kitty" {
		return ProtocolKitty
	}
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return ProtocolKitty
	}
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app", "WezTerm":
		return ProtocolITerm2
	}
	if os.Getenv("ITERM_SESSION_ID") != "" {
		return ProtocolITerm2
	}
	return ProtocolANSI
}
