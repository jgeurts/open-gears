package capture

import (
	"bytes"
	"strings"
	"testing"
)

// These packets are synthetic USB events, not recordings from a bicycle.
const syntheticTSV = "frame.number\tframe.time_relative\tusb.bus_id\tusb.device_address\tusb.endpoint_address\tusb.transfer_type\tusb.urb_type\tusb.urb_status\tusb.capdata\tusb.bmRequestType\tusb.setup.bRequest\tusb.setup.wValue\tusb.setup.wIndex\tusb.setup.wLength\n" +
	"1\t0.000000\t1\t11\t0x01\t0x03\tS\t0\t01:02:ff\t\t\t\t\t\n" +
	"2\t0.001000\t1\t11\t0x81\t0x03\tC\t0\t10:20\t\t\t\t\t\n" +
	"3\t0.002000\t1\t12\t0x01\t0x03\tS\t0\tde:ad\t\t\t\t\t\n" +
	"4\t0.003000\t1\t11\t0x00\t0x02\tS\t0\t\t0x40\t0x01\t0x1234\t0\t0\n"

func TestImportAndRoundTrip(t *testing.T) {
	events, err := ImportTSV(strings.NewReader(syntheticTSV), Filter{Bus: 1, Address: 11})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[0].Payload != "0102ff" || events[1].Direction != "in" || events[2].Setup == nil || events[2].Setup.Value != 0x1234 {
		t.Fatalf("incorrect import: %+v", events)
	}
	var b bytes.Buffer
	if err := Write(&b, events); err != nil {
		t.Fatal(err)
	}
	roundtrip, err := Read(&b)
	if err != nil || len(roundtrip) != 3 || roundtrip[0] != events[0] {
		t.Fatalf("roundtrip: %+v, %v", roundtrip, err)
	}
	summary := Analyze(roundtrip)
	if summary.Events != 3 || summary.PayloadBytes != 5 {
		t.Fatalf("summary: %+v", summary)
	}
}

func TestMalformedInputIsRejected(t *testing.T) {
	for _, input := range []string{
		`{"version":2,"frame":1,"bus":1,"address":11,"endpoint":1,"direction":"out","payload":"00"}`,
		`{"version":1,"frame":1,"bus":1,"address":11,"endpoint":1,"direction":"out","payload":"0g"}`,
		`{"version":1,"frame":1,"bus":1,"address":11,"endpoint":129,"direction":"out","payload":"00"}`,
		`{"version":1,"frame":1,"bus":1,"address":11,"endpoint":1,"direction":"out","payload":"00","typo":true}`,
	} {
		if _, err := Read(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	bad := strings.Replace(syntheticTSV, "01:02:ff", "01:XX:ff", 1)
	if _, err := ImportTSV(strings.NewReader(bad), Filter{}); err == nil {
		t.Fatal("accepted invalid payload")
	}
}

func TestDiffIgnoresTimingAndFindsInsertions(t *testing.T) {
	a := []Event{{Version: 1, Frame: 1, Bus: 1, Address: 11, Endpoint: 1, Direction: "out", Payload: "0102"}, {Version: 1, Frame: 2, Bus: 1, Address: 11, Endpoint: 129, Direction: "in", Payload: "03"}}
	b := append([]Event(nil), a...)
	b[0].Frame = 100
	b[0].Time = "5.000"
	if d := Diff(a, b); len(d.Changes) != 0 {
		t.Fatalf("timing caused diff: %+v", d)
	}
	b = append(b[:1], append([]Event{{Version: 1, Endpoint: 1, Direction: "out", Payload: "ffff"}}, b[1:]...)...)
	d := Diff(a, b)
	if len(d.Changes) != 1 || d.Changes[0].Kind != "insert" {
		t.Fatalf("insertion: %+v", d)
	}
	b = append([]Event(nil), a...)
	b[0].Payload = "0104"
	d = Diff(a, b)
	if len(d.Changes) != 1 || d.Changes[0].FirstByteDifference == nil || *d.Changes[0].FirstByteDifference != 1 {
		t.Fatalf("byte difference: %+v", d)
	}
}
