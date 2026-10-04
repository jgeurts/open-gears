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
