package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/noelruault/pixelize"
	"github.com/noelruault/pixelize/decode"
	"github.com/noelruault/pixelize/palettes"
	"github.com/noelruault/pixelize/preview"
)

// loadPalette resolves a palette arg as either a path (contains
// path separators / dot / tilde) or a name (XDG dir, then embedded).
func loadPalette(arg string) (pixelize.Palette[pixelize.EntryMeta], string, error) {
	if arg == "" {
		return nil, "", fmt.Errorf("no palette specified (-palette NAME or PATH)")
	}

	if strings.ContainsAny(arg, "/.~") {
		pal, err := pixelize.LoadFile(arg)
		if err != nil {
			return nil, "", err
		}
		hint := strings.TrimSuffix(filepath.Base(arg), filepath.Ext(arg))
		return pal, hint, nil
	}

	r, err := palettes.Resolve(arg)
	if err != nil {
		return nil, "", err
	}
	defer r.Reader.Close()

	var pal pixelize.Palette[pixelize.EntryMeta]
	switch r.Ext {
	case ".csv":
		pal, err = pixelize.LoadCSV(r.Reader)
	case ".hex":
		pal, err = pixelize.LoadHEX(r.Reader)
	case ".gpl":
		pal, err = pixelize.LoadGPL(r.Reader)
	case ".json":
		pal, err = pixelize.LoadJSON(r.Reader)
	default:
		return nil, "", fmt.Errorf("unsupported palette extension %s", r.Ext)
	}
	if err != nil {
		return nil, "", err
	}
	slog.Info("palette resolved", "name", arg, "source", string(r.Source), "size", len(pal))
	return pal, arg, nil
}

// runSingle handles the default subcommand: pixelize IMAGE [flags].
func runSingle(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pixelize", flag.ContinueOnError)
	pf := registerPipeline(fs)
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	pf.setupLogger()
	if len(rest) != 1 {
		return fmt.Errorf("expected one IMAGE arg, got %d", len(rest))
	}
	return pipeline(ctx, rest[0], pf)
}

func pipeline(ctx context.Context, inPath string, pf *pipelineFlags) error {
	img, format, err := decode.File(inPath)
	if err != nil {
		return err
	}
	slog.Info("decoded", "path", inPath, "format", format,
		"width", img.Bounds().Dx(), "height", img.Bounds().Dy())

	w, h, err := parseSize(pf.size)
	if err != nil {
		return err
	}
	mode, err := parseMode(pf.mode)
	if err != nil {
		return err
	}
	loopMode, err := parseLoop(pf.loop)
	if err != nil {
		return err
	}
	sizeList, err := parseSizeList(pf.sizeList)
	if err != nil {
		return err
	}

	pal, paletteHint, err := loadPalette(pf.palette)
	if err != nil {
		return err
	}

	if pf.gifPath != "" && len(sizeList) > 0 {
		return runGIFSizeList(ctx, img, pal, sizeList, mode, pf.dither, loopMode, pf.gifPath)
	}

	if pf.gifPath != "" && strings.EqualFold(filepath.Ext(inPath), ".gif") {
		return runAnimatedGIF(ctx, inPath, pal, w, h, mode, pf.dither, pf.gifPath)
	}

	opts := pixelize.ApplyOptions{
		Width:  w,
		Height: h,
		Resize: mode,
		Dither: pf.dither,
	}
	switch {
	case pf.fastLUT != nil:
		opts.FastLUT = pf.fastLUT // prebuilt and shared (batch)
	case pf.lut:
		opts.Fast = true
	}
	pat, err := pal.Apply(ctx, img, opts)
	if err != nil {
		return err
	}
	slog.Info("applied", "unique_colors", pat.UniqueColors(),
		"palette_size", len(pat.Palette), "size", fmt.Sprintf("%dx%d", pat.Image.Bounds().Dx(), pat.Image.Bounds().Dy()))

	outPath := pf.output
	if outPath == "" {
		outPath = defaultOutputPath(inPath, paletteHint)
	}
	if err := writePNG(outPath, pat.Image); err != nil {
		return err
	}
	slog.Info("wrote image", "path", outPath)

	if pf.buildMap != "" {
		if err := writeFile(pf.buildMap, func(w io.Writer) error { return pixelize.WriteBuildMap(w, pat) }); err != nil {
			return err
		}
		slog.Info("wrote build map", "path", pf.buildMap)
	}
	if pf.pieces != "" {
		if err := writeFile(pf.pieces, func(w io.Writer) error { return pixelize.WritePiecesCSV(w, pat) }); err != nil {
			return err
		}
		slog.Info("wrote pieces csv", "path", pf.pieces)
	}
	if pf.stats {
		if pf.asJSON {
			if err := pat.WriteStatsJSON(os.Stdout); err != nil {
				return err
			}
		} else {
			printStatsText(os.Stdout, pat)
		}
	}
	if pf.preview {
		proto, err := preview.Render(os.Stdout, pat.Image, preview.Options{MaxWidth: 80})
		if err != nil {
			return err
		}
		slog.Info("preview rendered", "protocol", string(proto))
	}
	return nil
}

func printStatsText(w io.Writer, pat *pixelize.Pattern[pixelize.EntryMeta]) {
	s := pat.Stats()
	fmt.Fprintf(w, "size:           %dx%d\n", s.Width, s.Height)
	fmt.Fprintf(w, "total pixels:   %d\n", s.TotalPixels)
	fmt.Fprintf(w, "palette size:   %d\n", s.PaletteSize)
	fmt.Fprintf(w, "unique colors:  %d\n", s.UniqueColors)
	fmt.Fprintln(w, "histogram (top 16):")
	max := 16
	if max > len(s.Histogram) {
		max = len(s.Histogram)
	}
	for i := 0; i < max; i++ {
		b := s.Histogram[i]
		fmt.Fprintf(w, "  %5d  #%-6s  %-30s  %d\n",
			b.Index, b.Entry.Meta.Hex, truncate(b.Entry.Meta.Name, 30), b.Count)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func runGIFSizeList(
	ctx context.Context,
	img image.Image,
	pal pixelize.Palette[pixelize.EntryMeta],
	sizes []int,
	mode pixelize.ResizeMode,
	dither bool,
	loop pixelize.LoopMode,
	outPath string,
) error {
	var frames []*image.RGBA
	maxW := sizes[0]
	for _, s := range sizes {
		if s > maxW {
			maxW = s
		}
	}
	for _, s := range sizes {
		pat, err := pal.Apply(ctx, img, pixelize.ApplyOptions{
			Width: s, Height: s, Resize: mode, Dither: dither,
		})
		if err != nil {
			return err
		}
		// Upscale frame to max size with NN so all frames share bounds.
		if s != maxW {
			pat.Image = upscaleNN(pat.Image, maxW)
		}
		frames = append(frames, pat.Image)
		slog.Info("gif frame", "size", s, "unique_colors", pat.UniqueColors())
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := pixelize.EncodeGIF(out, frames, pixelize.GIFOptions{Loop: loop}); err != nil {
		return err
	}
	slog.Info("wrote gif", "path", outPath, "frames", len(frames))
	return nil
}

// runAnimatedGIF decodes every frame of an animated GIF (composing
// disposal so each frame is the visible image), applies the palette
// to each, and re-encodes preserving per-frame delays and loop count.
func runAnimatedGIF(
	ctx context.Context,
	inPath string,
	pal pixelize.Palette[pixelize.EntryMeta],
	w, h int,
	mode pixelize.ResizeMode,
	dither bool,
	outPath string,
) error {
	anim, err := decode.AllFramesGIF(inPath)
	if err != nil {
		return err
	}
	slog.Info("animated gif decoded", "frames", len(anim.Frames),
		"loop", anim.LoopCount, "input_dims",
		fmt.Sprintf("%dx%d", anim.Frames[0].Bounds().Dx(), anim.Frames[0].Bounds().Dy()))

	out := make([]*image.RGBA, len(anim.Frames))
	for i, frame := range anim.Frames {
		pat, err := pal.Apply(ctx, frame, pixelize.ApplyOptions{
			Width: w, Height: h, Resize: mode, Dither: dither,
		})
		if err != nil {
			return fmt.Errorf("frame %d: %w", i, err)
		}
		out[i] = pat.Image
	}

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := pixelize.EncodeAnimatedGIF(f, out, anim.DelaysCS, anim.LoopCount); err != nil {
		return err
	}
	slog.Info("wrote animated gif", "path", outPath, "frames", len(out))
	return nil
}

// upscaleNN scales src to a width-by-width square via nearest neighbor.
func upscaleNN(src *image.RGBA, side int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	b := src.Bounds()
	for y := 0; y < side; y++ {
		sy := b.Min.Y + y*b.Dy()/side
		for x := 0; x < side; x++ {
			sx := b.Min.X + x*b.Dx()/side
			out.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return out
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func writeFile(path string, fn func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return fn(f)
}

// runPalettes handles `pixelize palettes [list|init|where]`.
func runPalettes(args []string) error {
	sub := "list"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "list", "":
		return runPalettesList(args)
	case "init":
		res, err := palettes.Init()
		if err != nil {
			return err
		}
		fmt.Printf("dir:     %s\n", res.Dir)
		fmt.Printf("copied:  %d\n", len(res.Copied))
		for _, n := range res.Copied {
			fmt.Printf("  + %s\n", n)
		}
		if len(res.Skipped) > 0 {
			fmt.Printf("skipped: %d (already existed)\n", len(res.Skipped))
			for _, n := range res.Skipped {
				fmt.Printf("  · %s\n", n)
			}
		}
		return nil
	case "where":
		dir, err := palettes.UserDir()
		if err != nil {
			return err
		}
		fmt.Println(dir)
		return nil
	}
	return fmt.Errorf("unknown palettes subcommand %q (want list | init | where)", sub)
}

func runPalettesList(args []string) error {
	fs := flag.NewFlagSet("palettes list", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text | json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	listing, err := palettes.List()
	if err != nil {
		return err
	}
	if *format == "json" {
		return json.NewEncoder(os.Stdout).Encode(listing)
	}
	for _, l := range listing {
		fmt.Printf("%-30s (%s)\n", l.Name, string(l.Source))
	}
	return nil
}

// runPalette handles `pixelize palette NAME [-show PATH]`.
func runPalette(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("palette: need a NAME")
	}
	name := args[0]
	fs := flag.NewFlagSet("palette", flag.ContinueOnError)
	show := fs.String("show", "", "render palette as PNG to PATH")
	cellSize := fs.Int("cell", 32, "cell size (with -show)")
	cols := fs.Int("cols", 8, "columns (with -show)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	pal, _, err := loadPalette(name)
	if err != nil {
		return err
	}

	if *show != "" {
		swatch := pal.Swatch(pixelize.SwatchOptions{CellSize: *cellSize, Columns: *cols})
		return writePNG(*show, swatch)
	}

	fmt.Printf("palette: %s (%d entries)\n", name, len(pal))
	for i, e := range pal {
		fmt.Printf("  %3d  #%-6s  %s\n", i, e.Meta.Hex, e.Meta.Name)
	}
	return nil
}

// runBatch handles `pixelize batch DIR [flags]`.
func runBatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("batch", flag.ContinueOnError)
	pf := registerPipeline(fs)
	fs.BoolVar(&pf.lut, "lut", false, "use a precomputed color lookup table: built once, reused across every image (~2-6% non-nearest, big speedup over a fixed palette)")
	fs.BoolVar(&pf.lut, "lookup-table", false, "alias of -lut")
	workers := fs.Int("workers", 0, "concurrent workers (default NumCPU)")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	pf.setupLogger()
	if len(rest) != 1 {
		return fmt.Errorf("batch needs one DIR arg")
	}
	inDir := rest[0]
	if pf.output == "" {
		return fmt.Errorf("batch requires -o DIR for output dir")
	}
	outDir := pf.output

	jobs, err := pixelize.CollectJobs(inDir, outDir, ".png", pixelize.BatchOptions{})
	if err != nil {
		return err
	}
	slog.Info("batch", "input_dir", inDir, "output_dir", outDir, "jobs", len(jobs))

	if err := enableFastLUT(pf); err != nil {
		return err
	}

	return pixelize.RunBatch(ctx, jobs, pixelize.BatchOptions{Workers: *workers}, func(ctx context.Context, j pixelize.BatchJob) error {
		jpf := *pf
		jpf.output = j.OutputPath
		return pipeline(ctx, j.InputPath, &jpf)
	})
}

// runWatch handles `pixelize watch IMAGE [flags]`.
func runWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	pf := registerPipeline(fs)
	fs.BoolVar(&pf.lut, "lut", false, "use a precomputed color lookup table: built once, reused across re-renders (~2-6% non-nearest)")
	fs.BoolVar(&pf.lut, "lookup-table", false, "alias of -lut")
	rest, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	pf.setupLogger()
	if len(rest) != 1 {
		return fmt.Errorf("watch needs one IMAGE arg")
	}
	inPath := rest[0]
	// The palette is fixed for the session, so build the Fast LUT once and
	// reuse it across every re-render.
	if err := enableFastLUT(pf); err != nil {
		return err
	}
	return pixelize.Watch(ctx, inPath, func() error {
		slog.Info("watch fired", "input", inPath)
		return pipeline(ctx, inPath, pf)
	})
}

// enableFastLUT builds the shared color lookup table once (the palette is
// fixed for the run), so batch and watch reuse it across every image -- the
// only place the approximate LUT is actually faster than the exact path.
// No-op unless -lut was given.
func enableFastLUT(pf *pipelineFlags) error {
	if !pf.lut {
		return nil
	}
	pal, _, err := loadPalette(pf.palette)
	if err != nil {
		return err
	}
	pf.fastLUT = pal.NewFastLUT()
	slog.Info("lut: built shared lookup table", "palette_size", len(pal))
	return nil
}
