package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/noelruault/pixelize"
)

// pipelineFlags collects the flag values used by the default, batch,
// and watch commands.
type pipelineFlags struct {
	size     string
	palette  string
	mode     string
	dither   bool
	sizeList string
	gifPath  string
	loop     string
	preview  bool
	stats    bool
	asJSON   bool
	buildMap string
	pieces   string
	output   string
	verbose  int
	lut      bool

	// fastLUT, when set, is a prebuilt Fast-mode table reused across calls
	// (batch/watch build it once for the whole run). Not a flag.
	fastLUT *pixelize.FastLUT
}

func registerPipeline(fs *flag.FlagSet) *pipelineFlags {
	pf := &pipelineFlags{}
	fs.StringVar(&pf.size, "size", "", "target dimensions WxH (or W for square)")
	fs.StringVar(&pf.palette, "palette", "", "palette by NAME or PATH")
	fs.StringVar(&pf.mode, "mode", "nn", "resize mode: nn | avg | bilinear | catmullrom")
	fs.BoolVar(&pf.dither, "dither", false, "enable Floyd-Steinberg dithering")
	fs.StringVar(&pf.sizeList, "size-list", "", "comma-separated sizes for -gif frames")
	fs.StringVar(&pf.gifPath, "gif", "", "output animated GIF over -size-list frames")
	fs.StringVar(&pf.loop, "loop", "none", "gif loop mode: none | reverse | full")
	fs.BoolVar(&pf.preview, "preview", false, "render to terminal")
	fs.BoolVar(&pf.stats, "stats", false, "print histogram")
	fs.BoolVar(&pf.asJSON, "json", false, "emit JSON for -stats")
	fs.StringVar(&pf.buildMap, "build-map", "", "write per-pixel build map to PATH")
	fs.StringVar(&pf.pieces, "pieces", "", "write piece-count CSV to PATH")
	fs.StringVar(&pf.output, "o", "", "output PNG path")
	fs.BoolFunc("v", "verbose (info)", func(string) error { pf.verbose = 1; return nil })
	fs.BoolFunc("vv", "very verbose (debug)", func(string) error { pf.verbose = 2; return nil })
	return pf
}

// parseInterleaved splits args into flag args (parsed via fs) and
// positionals, allowing IMAGE to appear before, between, or after
// flags. Returns the positional args in order.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	return positional, nil
}

func (pf *pipelineFlags) setupLogger() {
	level := slog.LevelWarn
	switch pf.verbose {
	case 1:
		level = slog.LevelInfo
	case 2:
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
}

func parseSize(s string) (w, h int, err error) {
	if s == "" {
		return 0, 0, nil
	}
	if !strings.ContainsAny(s, "xX") {
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0, 0, fmt.Errorf("size: %w", err)
		}
		return v, v, nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == 'x' || r == 'X' })
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("size %q: want WxH", s)
	}
	w, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("size width: %w", err)
	}
	h, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("size height: %w", err)
	}
	return w, h, nil
}

func parseSizeList(s string) ([]int, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("size-list %q: %w", p, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func parseMode(s string) (pixelize.ResizeMode, error) {
	switch strings.ToLower(s) {
	case "", "nn":
		return pixelize.NearestNeighbor, nil
	case "avg", "average", "block":
		return pixelize.BlockAverage, nil
	case "bilinear":
		return pixelize.BiLinear, nil
	case "catmullrom":
		return pixelize.CatmullRom, nil
	}
	return 0, fmt.Errorf("unknown resize mode %q (want nn | avg | bilinear | catmullrom)", s)
}

func parseLoop(s string) (pixelize.LoopMode, error) {
	switch strings.ToLower(s) {
	case "", "none":
		return pixelize.LoopNone, nil
	case "reverse":
		return pixelize.LoopReverse, nil
	case "full":
		return pixelize.LoopFull, nil
	}
	return 0, fmt.Errorf("unknown loop mode %q", s)
}

func defaultOutputPath(inputPath, paletteHint string) string {
	dir := filepath.Dir(inputPath)
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if paletteHint != "" {
		base = base + "_" + paletteHint
	}
	return filepath.Join(dir, base+".png")
}
