package protocol

import (
	"bytes"
	"testing"
)

func TestReadOnlyQueryWireBytes(t *testing.T) {
	for _, tt := range []struct {
		control byte
		wire    []byte
	}{{4, []byte{0xbb, 4, 0xfc, 0xbb}}, {5, []byte{0xbb, 5, 0xfb, 0xbb}}} {
		if got := Encode(tt.control, nil); !bytes.Equal(got, tt.wire) {
			t.Fatalf("control %x: %x", tt.control, got)
		}
	}
}

func TestEscapingAndFragmentedFrames(t *testing.T) {
	payload := []byte{0xbb, 0xbd, 0, 0xff}
	wire := Encode(0x48, payload)
	if !bytes.Contains(wire, []byte{0xbd, 0x9b, 0xbd, 0x9d}) {
		t.Fatalf("missing escapes: %x", wire)
	}
	var d Decoder
	var all []Frame
	for _, b := range wire {
		frames, err := d.Feed([]byte{b})
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, frames...)
	}
	if len(all) != 1 || all[0].Control != 0x48 || !bytes.Equal(all[0].Payload, payload) {
		t.Fatalf("decoded: %+v", all)
	}
	frames, err := d.Feed([]byte{5, 0xfb, 0xbb})
	if err != nil || len(frames) != 1 || frames[0].Control != 5 {
		t.Fatalf("omitted leading flag: %+v %v", frames, err)
	}
}

func TestRejectBadChecksumAndRecover(t *testing.T) {
	var d Decoder
	if _, err := d.Feed([]byte{0xbb, 4, 0xfd, 0xbb}); err == nil {
		t.Fatal("accepted bad checksum")
	}
	frames, err := d.Feed(Encode(5, nil))
	if err != nil || len(frames) != 1 {
		t.Fatalf("recovery: %+v %v", frames, err)
	}
	if _, err := d.Feed([]byte{0xbb, 1, 0xbd, 0xbb}); err == nil {
		t.Fatal("accepted dangling escape")
	}
}
