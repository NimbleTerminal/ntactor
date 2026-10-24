// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestFlockHeadlessDeterminism(t *testing.T) {
	var out1, out2, errBuf bytes.Buffer
	if code := run([]string{"-headless", "-seed", "42", "-count", "60", "-steps", "30"}, &out1, &errBuf); code != 0 {
		t.Fatalf("first flock run failed (%d): %s", code, errBuf.String())
	}
	if code := run([]string{"-headless", "-seed", "42", "-count", "60", "-steps", "30"}, &out2, &errBuf); code != 0 {
		t.Fatalf("second flock run failed (%d): %s", code, errBuf.String())
	}
	if out1.String() != out2.String() {
		t.Fatalf("flock output diverged:\n%s\nvs\n%s", out1.String(), out2.String())
	}
	if !strings.Contains(out1.String(), "60 points packed") {
		t.Fatalf("expected 60 points packed in header, got:\n%s", out1.String())
	}
}
