// Copyright (c) 2026 Neomantra Corp.

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReplayCLI(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"-seed", "42", "-steps", "80"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("run failed with exit %d: %s", code, errBuf.String())
	}
	if !strings.Contains(out.String(), "bitExact=true") {
		t.Fatalf("expected bitExact=true in output, got:\n%s", out.String())
	}

	out.Reset()
	errBuf.Reset()
	code = run([]string{"-seed", "42", "-steps", "80", "-json"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("json run failed with exit %d: %s", code, errBuf.String())
	}
	var rep summaryReport
	if err := json.Unmarshal(out.Bytes(), &rep); err != nil {
		t.Fatalf("Unmarshal JSON failed: %v", err)
	}
	if !rep.DeterministicMatch || rep.Actors != 7 || len(rep.SceneNodes) != 7 {
		t.Fatalf("unexpected report: %+v", rep)
	}
}
