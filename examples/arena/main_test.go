// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestArenaHeadlessDeterminism(t *testing.T) {
	var out1, out2, errBuf bytes.Buffer
	if code := run([]string{"-headless", "-seed", "42", "-steps", "40"}, &out1, &errBuf); code != 0 {
		t.Fatalf("first headless run failed (%d): %s", code, errBuf.String())
	}
	if code := run([]string{"-headless", "-seed", "42", "-steps", "40"}, &out2, &errBuf); code != 0 {
		t.Fatalf("second headless run failed (%d): %s", code, errBuf.String())
	}
	if out1.String() != out2.String() {
		t.Fatalf("headless arena output was not deterministic:\nrun1:\n%s\nrun2:\n%s", out1.String(), out2.String())
	}
	if !strings.Contains(out1.String(), "tick=40") {
		t.Fatalf("expected tick=40 in header, got:\n%s", out1.String())
	}
}
