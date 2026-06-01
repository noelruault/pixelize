package main

import (
	"flag"
	"testing"
)

// TestLUTFlagAndAlias verifies that both -lut and its -lookup-table alias set
// the same flag on the batch/watch flagset, and that they default off.
func TestLUTFlagAndAlias(t *testing.T) {
	for _, name := range []string{"-lut", "-lookup-table"} {
		fs := flag.NewFlagSet("batch", flag.ContinueOnError)
		pf := registerPipeline(fs)
		registerLUTFlags(fs, pf, "test")
		if pf.lut {
			t.Fatalf("%s: lut should default false", name)
		}
		if err := fs.Parse([]string{name}); err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if !pf.lut {
			t.Errorf("%s did not set lut", name)
		}
	}
}

// TestSingleImageRejectsLUT guards the contract that the single-image command
// does not accept -lut/-lookup-table: registerPipeline alone (the single-image
// flag surface) must not define them, so passing one errors as undefined.
func TestSingleImageRejectsLUT(t *testing.T) {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	fs.SetOutput(discard{})
	registerPipeline(fs) // no registerLUTFlags: single-image surface
	for _, name := range []string{"lut", "lookup-table"} {
		if fs.Lookup(name) != nil {
			t.Errorf("single-image command unexpectedly defines -%s", name)
		}
	}
	if err := fs.Parse([]string{"-lut"}); err == nil {
		t.Error("single-image command accepted -lut, want undefined-flag error")
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
