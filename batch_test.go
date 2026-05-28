package pixelize

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestCollectJobs(t *testing.T) {
	in := t.TempDir()
	for _, name := range []string{"a.png", "b.jpg", "c.txt", "d.gif", "e.webp"} {
		if err := os.WriteFile(filepath.Join(in, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := CollectJobs(in, "/out", ".png", BatchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 4 {
		t.Fatalf("jobs = %d, want 4 (.png .jpg .gif .webp)", len(jobs))
	}
}

func TestRunBatch(t *testing.T) {
	jobs := make([]BatchJob, 10)
	for i := range jobs {
		jobs[i] = BatchJob{InputPath: "in", OutputPath: "out"}
	}
	var n int32
	err := RunBatch(context.Background(), jobs, BatchOptions{Workers: 3}, func(ctx context.Context, j BatchJob) error {
		atomic.AddInt32(&n, 1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Fatalf("processed = %d, want 10", n)
	}
}
