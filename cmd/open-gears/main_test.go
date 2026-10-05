package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOfflineCommands(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"version"}, {"capture", "fields"}, {"protocol", "decode", "bb25300100aabb"}} {
		var output bytes.Buffer
		if err := run(args, &output); err != nil || output.Len() == 0 {
			t.Fatalf("%v: %s, %v", args, &output, err)
		}
	}
	var output bytes.Buffer
	if err := run([]string{"protocol", "decode", "bb25300100aabb"}, &output); err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]string
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil || len(decoded) != 1 || decoded[0]["control"] != "0x25" || decoded[0]["payload"] != "300100" {
		t.Fatalf("decode: %s, %v", &output, err)
	}
}

func TestInvalidCommandsFailBeforeHardwareAccess(t *testing.T) {
	for _, args := range [][]string{
		{"no-such-command"}, {"devices", "unexpected"}, {"adapter"}, {"adapter", "write"},
		{"adapter", "info", "--address", "128"}, {"adapter", "info", "extra"},
		{"bike", "write"}, {"capture", "import"}, {"capture", "fields", "extra"},
		{"capture", "diff", "one"}, {"protocol", "decode", "zz"},
		{"protocol", "decode", "bb25300100abbb"}, {"protocol", "decode", "bb25"},
	} {
		if err := run(args, new(bytes.Buffer)); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestTraceDoesNotOverwriteExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"adapter", "info", "--trace", path}, new(bytes.Buffer)); err == nil {
		t.Fatal("overwrote trace")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "keep me" {
		t.Fatalf("trace changed: %q, %v", got, err)
	}
}

func TestCaptureImportAnalyzeAndDiff(t *testing.T) {
	dir := t.TempDir()
	tsvPath, capturePath := filepath.Join(dir, "input.tsv"), filepath.Join(dir, "capture.jsonl")
	tsv := "frame.number\tusb.bus_id\tusb.device_address\tusb.endpoint_address\tusb.capdata\n1\t0\t12\t0x81\tbb25300100aabb\n"
	if err := os.WriteFile(tsvPath, []byte(tsv), 0600); err != nil {
		t.Fatal(err)
	}
	var imported bytes.Buffer
	if err := run([]string{"capture", "import", tsvPath, "--bus", "0"}, &imported); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capturePath, imported.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run([]string{"capture", "analyze", capturePath}, &output); err != nil || !strings.Contains(output.String(), `"events": 1`) {
		t.Fatalf("analysis: %s, %v", &output, err)
	}
	output.Reset()
	if err := run([]string{"capture", "diff", capturePath, capturePath}, &output); err != nil || !strings.Contains(output.String(), `"changes": []`) {
		t.Fatalf("diff: %s, %v", &output, err)
	}
}

func TestPaddleCommandsRejectInvalidInputBeforeUSB(t *testing.T) {
	for _, args := range [][]string{
		{"bike", "paddles"}, {"bike", "paddles", "write"},
		{"bike", "paddles", "plan", "--slot", "2", "--a", "unassigned"},
		{"bike", "paddles", "plan", "--slot", "31", "--a", "front-up"},
		{"bike", "paddles", "plan", "--slot", "2"},
		{"bike", "paddles", "apply"},
		{"bike", "paddles", "apply", "--slot", "2"},
	} {
		var output bytes.Buffer
		if err := run(args, &output); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}

func TestPaddleApplyAlwaysReturnsStructuredPreflightFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.json")
	plan := `{"version":1,"adapter":{"bus":0,"path":[1,1,4]},"changes":[{"slot":2,"series":5,"number":1,"part":1,"firmware_version":"3.1.0 (revision 0)","before_raw":"01ab","a":3,"b":1}]}`
	if err := os.WriteFile(path, []byte(plan), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := run([]string{"bike", "paddles", "apply", "--plan", path, "--bus", "1"}, &output)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unexpected preflight failure: %v", err)
	}
	var result struct {
		Status  string `json:"status"`
		Error   string `json:"error"`
		Changes []struct {
			Status string `json:"status"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatalf("missing structured failure: %s, %v", &output, err)
	}
	if result.Status != "unchanged" || result.Error == "" || len(result.Changes) != 1 || result.Changes[0].Status != "unchanged" {
		t.Fatalf("wrong outcome: %+v", result)
	}
}

func TestPaddleApplyValidatesPlanBeforeCreatingTrace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-plan.json")
	trace := filepath.Join(dir, "trace.jsonl")
	if err := os.WriteFile(path, []byte(`{"version":1,"extra":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := run([]string{"bike", "paddles", "apply", "--plan", path, "--trace", trace}, &output)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("wrong validation error: %v", err)
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatalf("trace created before plan validation: %v", err)
	}
	if !json.Valid(output.Bytes()) || !strings.Contains(output.String(), `"status": "unchanged"`) {
		t.Fatalf("missing structured error: %s", &output)
	}
}
