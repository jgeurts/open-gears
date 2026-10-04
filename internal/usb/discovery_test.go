package usb

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/gousb"
)

func TestNormalizeDescriptor(t *testing.T) {
	desc := &gousb.DeviceDesc{
		Bus: 3, Address: 7, Port: 2, Path: []int{4, 2},
		Vendor: 0x1e44, Product: 0x7220,
		Spec: 0x0200, Device: 0x0102, Speed: gousb.SpeedFull,
		Class: 0xff, SubClass: 2, Protocol: 3, MaxControlPacketSize: 64,
		Configs: map[int]gousb.ConfigDesc{
			2: {
				Number: 2, SelfPowered: true, RemoteWakeup: true, MaxPower: 100,
				Interfaces: []gousb.InterfaceDesc{
					{Number: 4, AltSettings: []gousb.InterfaceSetting{
						{Number: 4, Alternate: 1, Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{}},
						{Number: 4, Alternate: 0, Class: 0xff, SubClass: 2, Protocol: 3, Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{
							0x82: {Address: 0x82, Number: 2, Direction: gousb.EndpointDirectionIn, TransferType: gousb.TransferTypeBulk, MaxPacketSize: 64, PollInterval: time.Millisecond},
							0x01: {Address: 0x01, Number: 1, Direction: gousb.EndpointDirectionOut, TransferType: gousb.TransferTypeBulk, MaxPacketSize: 32},
						}},
					}},
					{Number: 0, AltSettings: nil},
				},
			},
			1: {Number: 1, Interfaces: []gousb.InterfaceDesc{{Number: 0, AltSettings: []gousb.InterfaceSetting{{Number: 0, Alternate: 0, Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{
				0x01: {Address: 0x01, Number: 1, Direction: gousb.EndpointDirectionOut, TransferType: gousb.TransferTypeBulk, MaxPacketSize: 64},
			}}}}}},
		},
	}

	got := normalizeDescriptor(desc)
	if got.Bus != 3 || got.Address != 7 || got.Port != 2 || !reflect.DeepEqual(got.Path, []int{4, 2}) {
		t.Fatalf("physical identity lost: %+v", got)
	}
	if got.VendorID != "1e44" || got.ProductID != "7220" || got.USBVersion != "2.00" || got.DeviceVersion != "1.02" || got.Speed != "full" {
		t.Fatalf("device identity lost: %+v", got)
	}
	if got.Class != 255 || got.SubClass != 2 || got.Protocol != 3 || got.MaxControlPacketSize != 64 {
		t.Fatalf("device protocol descriptor lost: %+v", got)
	}
	if len(got.Configurations) != 2 || got.Configurations[0].Number != 1 || got.Configurations[1].Number != 2 {
		t.Fatalf("configuration order is unstable: %+v", got.Configurations)
	}
	if got.Configurations[0].Layout != "bulk-out-only" {
		t.Fatalf("single bulk OUT layout = %q", got.Configurations[0].Layout)
	}
	cfg := got.Configurations[1]
	if cfg.Layout != "bidirectional-bulk" {
		t.Fatalf("bulk IN/OUT layout = %q", cfg.Layout)
	}
	if !cfg.SelfPowered || !cfg.RemoteWakeup || cfg.MaxPowerMilliamps != 100 {
		t.Fatalf("configuration capabilities lost: %+v", cfg)
	}
	if cfg.Interfaces[0].Number != 0 || cfg.Interfaces[1].Number != 4 {
		t.Fatalf("interface order is unstable: %+v", cfg.Interfaces)
	}
	settings := cfg.Interfaces[1].Settings
	if settings[0].Alternate != 0 || settings[1].Alternate != 1 {
		t.Fatalf("alternate setting order is unstable: %+v", settings)
	}
	if settings[0].Class != 255 || settings[0].SubClass != 2 || settings[0].Protocol != 3 {
		t.Fatalf("setting protocol descriptor lost: %+v", settings[0])
	}
	eps := settings[0].Endpoints
	if eps[0].Address != "0x01" || eps[1].Address != "0x82" || eps[1].Direction != "in" || eps[0].Direction != "out" || eps[0].TransferType != "bulk" {
		t.Fatalf("endpoint order or transfer identity lost: %+v", eps)
	}
	if eps[0].Number != 1 || eps[0].MaxPacketSize != 32 || eps[1].MaxPacketSize != 64 || eps[1].PollIntervalNanoseconds != int64(time.Millisecond) {
		t.Fatalf("endpoint packet capabilities lost: %+v", eps)
	}

	first, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for range 30 {
		next, err := json.Marshal(normalizeDescriptor(desc))
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(next) {
			t.Fatalf("descriptor JSON order changed:\n%s\n%s", first, next)
		}
	}

	desc.Path[0] = 9
	if got.Path[0] != 4 || desc.Configs[2].Interfaces[0].Number != 4 || desc.Configs[2].Interfaces[0].AltSettings[0].Alternate != 1 {
		t.Fatal("normalization aliases or mutates the source descriptor")
	}
}

func TestUnknownConfigurationDoesNotImplyRuntime(t *testing.T) {
	for _, cfg := range []gousb.ConfigDesc{
		{Number: 99},
		{Number: 2, Interfaces: []gousb.InterfaceDesc{{Number: 0, AltSettings: []gousb.InterfaceSetting{{Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{
			0x81: {Address: 0x81, Direction: gousb.EndpointDirectionIn, TransferType: gousb.TransferTypeInterrupt},
		}}}}}},
		{Number: 3, Interfaces: []gousb.InterfaceDesc{{Number: 0, AltSettings: []gousb.InterfaceSetting{
			{Alternate: 0, Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{
				0x81: {Address: 0x81, Direction: gousb.EndpointDirectionIn, TransferType: gousb.TransferTypeBulk},
			}},
			{Alternate: 1, Endpoints: map[gousb.EndpointAddress]gousb.EndpointDesc{
				0x01: {Address: 0x01, Direction: gousb.EndpointDirectionOut, TransferType: gousb.TransferTypeBulk},
			}},
		}}}},
	} {
		got := normalizeDescriptor(&gousb.DeviceDesc{Configs: map[int]gousb.ConfigDesc{cfg.Number: cfg}})
		if got.Configurations[0].Layout != "unknown" {
			t.Fatalf("unexpected layout for unfamiliar configuration: %+v", got.Configurations[0])
		}
	}
}

func TestEmptyDescriptorUsesArrays(t *testing.T) {
	got := normalizeDescriptor(&gousb.DeviceDesc{Configs: map[int]gousb.ConfigDesc{1: {Number: 1}}})
	if got.Path == nil || got.Configurations == nil || got.Configurations[0].Interfaces == nil {
		t.Fatalf("empty collections must encode as JSON arrays: %+v", got)
	}
}
