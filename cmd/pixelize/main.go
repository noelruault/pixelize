// Command pixelize resizes images and quantizes their colors to a palette.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "palettes":
		err = runPalettes(args)
	case "palette":
		err = runPalette(args)
	case "batch":
		err = runBatch(ctx, args)
	case "watch":
		err = runWatch(ctx, args)
	case "version":
		fmt.Println(versionString())
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		err = runSingle(ctx, append([]string{cmd}, args...))
	}

	if err != nil {
		slog.Error("pixelize", "err", err)
		os.Exit(1)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `pixelize - resize images and quantize their colors to a palette

Usage:
  pixelize IMAGE [flags]                  process a single image
  pixelize batch DIR [flags]              process all images in a directory
  pixelize watch IMAGE [flags]            rerun on file change
  pixelize palettes [list|init|where]     manage example palettes
  pixelize palette NAME [-show PATH]      inspect a palette
  pixelize version                        print version
  pixelize help                           this message

Single-image flags:
  -size WxH         target dimensions (omit to skip resize)
  -palette NAME     palette by name (resolves user dir then embedded)
  -palette PATH     palette from file (.csv .hex .gpl .json)
  -mode MODE        resize mode: nn (default) | avg | bilinear | catmullrom
  -dither           Floyd-Steinberg dithering
  -size-list LIST   comma-separated sizes for -gif (e.g. "16,32,48,64")
  -gif PATH         emit animated GIF over -size-list frames
  -loop MODE        gif loop: none (default) | reverse | full
  -preview          render to terminal (iterm2 / kitty / ansi)
  -stats            print histogram to stdout
  -json             use JSON for -stats output
  -build-map PATH   write per-pixel build map
  -pieces PATH      write piece-count CSV
  -o PATH           output PNG (default: <input>_<palette>.png)
  -v, -vv           verbose logging

Examples:
  pixelize photo.jpg -size 64x64 -palette nes -o photo_nes.png
  pixelize art.png -palette ./custom.csv -stats -json
  pixelize mosaic.jpg -size 48x48 -palette lego -build-map build.txt -pieces parts.csv
  pixelize sprite.png -size-list 8,16,32,64 -palette pico8 -gif growth.gif -loop full
  pixelize palettes init
`)
}

func versionString() string {
	return "pixelize dev"
}
