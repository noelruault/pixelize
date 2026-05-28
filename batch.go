package pixelize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sync/errgroup"
)

func defaultWorkers() int { return runtime.NumCPU() }

// BatchJob is one item in a batch run.
type BatchJob struct {
	InputPath  string
	OutputPath string
}

// BatchOptions controls Batch concurrency and file selection.
type BatchOptions struct {
	// Workers is the number of concurrent jobs. Default runtime.NumCPU.
	// Zero or negative uses the default.
	Workers int

	// Extensions filters input files by extension (lowercase, with dot).
	// Default: .png, .jpg, .jpeg, .gif, .webp, .jxl.
	Extensions []string
}

// CollectJobs walks inDir and returns jobs that pair each accepted
// input file with an output path derived as outDir/<basename>.<outExt>.
func CollectJobs(inDir, outDir, outExt string, opts BatchOptions) ([]BatchJob, error) {
	exts := opts.Extensions
	if len(exts) == 0 {
		exts = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".jxl"}
	}
	accept := map[string]bool{}
	for _, e := range exts {
		accept[strings.ToLower(e)] = true
	}

	var jobs []BatchJob
	entries, err := os.ReadDir(inDir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !accept[ext] {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		jobs = append(jobs, BatchJob{
			InputPath:  filepath.Join(inDir, e.Name()),
			OutputPath: filepath.Join(outDir, base+outExt),
		})
	}
	return jobs, nil
}

// RunBatch executes process(job) for every job concurrently, with at
// most opts.Workers goroutines. Returns the first error if any worker
// fails; the rest are canceled.
func RunBatch(ctx context.Context, jobs []BatchJob, opts BatchOptions, process func(context.Context, BatchJob) error) error {
	workers := opts.Workers
	if workers <= 0 {
		workers = defaultWorkers()
	}

	g, gctx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, workers)

	for _, j := range jobs {
		j := j
		select {
		case sem <- struct{}{}:
		case <-gctx.Done():
			return gctx.Err()
		}
		g.Go(func() error {
			defer func() { <-sem }()
			if err := process(gctx, j); err != nil {
				return fmt.Errorf("%s: %w", j.InputPath, err)
			}
			return nil
		})
	}
	return g.Wait()
}
