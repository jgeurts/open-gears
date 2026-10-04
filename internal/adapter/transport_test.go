package adapter

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/gousb"
	"github.com/jgeurts/open-gears/internal/capture"
)

func TestBootImageHeader(t *testing.T) {
	// Synthetic 8051 bytes; the checksum and little-endian length are TI format.
	got, err := wrapFirmware([]byte{0x02, 0x00, 0x1e, 0xff})
	if err != nil || !bytes.Equal(got, []byte{4, 0, 0x1f, 2, 0, 0x1e, 0xff}) {
		t.Fatalf("header %x, %v", got, err)
	}
	if _, err := wrapFirmware(make([]byte, 65536)); err == nil {
		t.Fatal("accepted oversized image")
	}
	if _, err := validateFirmware([]byte("wrong firmware")); err == nil {
		t.Fatal("accepted unidentified controller firmware")
	}
	if _, err := validateFirmware(make([]byte, 14336)); err == nil {
		t.Fatal("accepted correctly sized image with wrong fingerprint")
	}
}

func TestLargeFirmwareRejectionPreservesFingerprint(t *testing.T) {
	raw := bytes.Repeat([]byte{0xa5}, 200000)
	path := filepath.Join(t.TempDir(), "wrong.i51")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadFirmware(path)
	if err == nil || !strings.Contains(err.Error(), "size 200000") || !strings.Contains(err.Error(), fmt.Sprintf("%x", sha256.Sum256(raw))) {
		t.Fatalf("fingerprint error: %v", err)
	}
}

func TestTraceSequenceSurvivesReconnection(t *testing.T) {
	var output bytes.Buffer
	recorder := &traceRecorder{writer: &output, started: time.Now()}
	first := &Connection{dev: &gousb.Device{Desc: &gousb.DeviceDesc{Bus: 0, Address: 12}}, trace: recorder}
	second := &Connection{dev: &gousb.Device{Desc: &gousb.DeviceDesc{Bus: 0, Address: 13}}, trace: recorder}
	first.record(1, "bulk", []byte{0xbb, 0x10, 0xf0, 0xbb}, nil)
	second.record(0x81, "bulk", []byte{0xbb, 0x30, 0, 0xd0, 0xbb}, nil)
	events, err := capture.Read(&output)
	if err != nil || len(events) != 2 || events[0].Frame != 1 || events[1].Frame != 2 || events[1].Address != 13 {
		t.Fatalf("reconnected trace: %+v, %v", events, err)
	}
}

type failingTrace struct{ writes int }

func (w *failingTrace) Write([]byte) (int, error) {
	w.writes++
	return 0, io.ErrClosedPipe
}

func TestTraceFailureIsDeferredAndSurvivesReconnect(t *testing.T) {
	w := &failingTrace{}
	recorder := &traceRecorder{writer: w, started: time.Now()}
	c := &Connection{dev: &gousb.Device{Desc: &gousb.DeviceDesc{Bus: 0, Address: 12}}, trace: recorder}
	c.record(0, "control", nil, &capture.Setup{RequestType: 0x40, Request: 6})
	if !errors.Is(recorder.err, io.ErrClosedPipe) {
		t.Fatalf("trace failure lost: %v", recorder.err)
	}
	// Once logging fails, later traffic can proceed without retrying storage.
	c.record(1, "bulk", []byte{0xbb, 6, 0, 0xfa, 0xbb}, nil)
	c.record(0x81, "bulk", []byte{0xbb, 0x26, 0, 0xda, 0xbb}, nil)
	if w.writes != 1 {
		t.Fatalf("retried failed trace %d times", w.writes)
	}
	// No real USB handles are used by this test. Reconnect cleanup succeeds;
	// the replacement connection retains the diagnostic for the final caller.
	c.dev = nil
	if err := c.closeHardware(); err != nil {
		t.Fatal(err)
	}
	replacement := &Connection{trace: recorder}
	if err := replacement.Close(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("final trace diagnostic: %v", err)
	}
}

func TestUARTConfig(t *testing.T) {
	if got := uartConfig(); !bytes.Equal(got, []byte{0, 24, 0x60, 0, 3, 0, 0, 0x11, 0x13, 0}) {
		t.Fatalf("38400 8N1 config %x", got)
	}
}
