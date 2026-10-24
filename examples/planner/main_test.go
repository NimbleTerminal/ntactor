// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlannerHeadlessDeterminism(t *testing.T) {
	var out1, err1 bytes.Buffer
	if code := run([]string{"-headless", "-steps", "30", "-seed", "42"}, &out1, &err1); code != 0 {
		t.Fatalf("first headless run failed (code=%d): %s", code, err1.String())
	}

	var out2, err2 bytes.Buffer
	if code := run([]string{"-headless", "-steps", "30", "-seed", "42"}, &out2, &err2); code != 0 {
		t.Fatalf("second headless run failed (code=%d): %s", code, err2.String())
	}

	if out1.String() != out2.String() {
		t.Fatalf("headless planner output mismatch:\nrun1:\n%s\nrun2:\n%s", out1.String(), out2.String())
	}
	if !strings.Contains(out1.String(), "COUNTERFACTUAL ROLLOUT RANKING") {
		t.Fatalf("expected rollout ranking in output, got:\n%s", out1.String())
	}
}
