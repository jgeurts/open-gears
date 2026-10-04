// Package usb enumerates SM-BCR2 USB descriptors without opening a device.
package usb

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/gousb"
)

const (
	VendorID  = 0x1e44
	ProductID = 0x7220
)

// Device is an observed USB device descriptor. Configurations lists available
// descriptors; enumeration does not query or change the active configuration.
type Device struct {
	Bus                  int             `json:"bus"`
	Address              int             `json:"address"`
	Port                 int             `json:"port"`
	Path                 []int           `json:"path"`
	VendorID             string          `json:"vendor_id"`
	ProductID            string          `json:"product_id"`
	USBVersion           string          `json:"usb_version"`
	DeviceVersion        string          `json:"device_version"`
	Speed                string          `json:"speed"`
	Class                int             `json:"class"`
	SubClass             int             `json:"subclass"`
	Protocol             int             `json:"protocol"`
	MaxControlPacketSize int             `json:"max_control_packet_size"`
	Configurations       []Configuration `json:"configurations"`
}

// Configuration describes a supported configuration, not the current state.
type Configuration struct {
	Number            int  `json:"number"`
	SelfPowered       bool `json:"self_powered"`
	RemoteWakeup      bool `json:"remote_wakeup"`
	MaxPowerMilliamps int  `json:"max_power_milliamps"`
	// Layout summarizes endpoints, without asserting a device operating mode.
	Layout     string      `json:"layout"`
	Interfaces []Interface `json:"interfaces"`
}

type Interface struct {
	Number   int       `json:"number"`
	Settings []Setting `json:"settings"`
}

type Setting struct {
	Alternate int        `json:"alternate"`
	Class     int        `json:"class"`
	SubClass  int        `json:"subclass"`
	Protocol  int        `json:"protocol"`
	Endpoints []Endpoint `json:"endpoints"`
}

type Endpoint struct {
	Address                 string `json:"address"`
	Number                  int    `json:"number"`
	Direction               string `json:"direction"`
	TransferType            string `json:"transfer_type"`
	MaxPacketSize           int    `json:"max_packet_size"`
	PollIntervalNanoseconds int64  `json:"poll_interval_nanoseconds"`
}

// Discover enumerates matching descriptors without opening a device handle,
// claiming an interface, changing a configuration, or sending protocol traffic.
// It may return partial results alongside a USB enumeration error.
func Discover() (devices []Device, err error) {
	devices = []Device{}
	ctx, err := newContext()
	if err != nil {
		return devices, err
	}
	defer func() { err = errors.Join(err, ctx.Close()) }()

	_, err = ctx.OpenDevices(func(desc *gousb.DeviceDesc) bool {
		if desc.Vendor == VendorID && desc.Product == ProductID {
			devices = append(devices, normalizeDescriptor(desc))
		}
		// Returning false is essential: OpenDevices must never open a handle.
		return false
	})
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].Bus != devices[j].Bus {
			return devices[i].Bus < devices[j].Bus
		}
		return devices[i].Address < devices[j].Address
	})
	return devices, err
}

func newContext() (ctx *gousb.Context, err error) {
	// gousb reports libusb initialization failures by panicking, so adapt that
	// API to Discover's error return while retaining the underlying error.
	defer func() {
		if failure := recover(); failure != nil {
			if cause, ok := failure.(error); ok {
				err = fmt.Errorf("initialize USB context: %w", cause)
			} else {
				err = fmt.Errorf("initialize USB context: %v", failure)
			}
		}
	}()
	return gousb.NewContext(), nil
}

func normalizeDescriptor(desc *gousb.DeviceDesc) Device {
	device := Device{
		Bus: desc.Bus, Address: desc.Address, Port: desc.Port,
		Path:     append([]int{}, desc.Path...),
		VendorID: desc.Vendor.String(), ProductID: desc.Product.String(),
		USBVersion: desc.Spec.String(), DeviceVersion: desc.Device.String(),
		Speed: enumName(desc.Speed.String(), int(desc.Speed)),
		Class: int(desc.Class), SubClass: int(desc.SubClass), Protocol: int(desc.Protocol),
		MaxControlPacketSize: desc.MaxControlPacketSize,
		Configurations:       make([]Configuration, 0, len(desc.Configs)),
	}
	for _, source := range desc.Configs {
		cfg := Configuration{
			Number: source.Number, SelfPowered: source.SelfPowered, RemoteWakeup: source.RemoteWakeup,
			MaxPowerMilliamps: int(source.MaxPower), Layout: configurationLayout(source),
			Interfaces: make([]Interface, 0, len(source.Interfaces)),
		}
		for _, sourceInterface := range source.Interfaces {
			intf := Interface{Number: sourceInterface.Number, Settings: make([]Setting, 0, len(sourceInterface.AltSettings))}
			for _, sourceSetting := range sourceInterface.AltSettings {
				setting := Setting{
					Alternate: sourceSetting.Alternate, Class: int(sourceSetting.Class),
					SubClass: int(sourceSetting.SubClass), Protocol: int(sourceSetting.Protocol),
					Endpoints: make([]Endpoint, 0, len(sourceSetting.Endpoints)),
				}
				for _, sourceEndpoint := range sourceSetting.Endpoints {
					setting.Endpoints = append(setting.Endpoints, Endpoint{
						Address: sourceEndpoint.Address.String(), Number: sourceEndpoint.Number,
						Direction:     strings.ToLower(sourceEndpoint.Direction.String()),
						TransferType:  enumName(sourceEndpoint.TransferType.String(), int(sourceEndpoint.TransferType)),
						MaxPacketSize: sourceEndpoint.MaxPacketSize, PollIntervalNanoseconds: int64(sourceEndpoint.PollInterval),
					})
				}
				sort.Slice(setting.Endpoints, func(i, j int) bool { return setting.Endpoints[i].Address < setting.Endpoints[j].Address })
				intf.Settings = append(intf.Settings, setting)
			}
			sort.Slice(intf.Settings, func(i, j int) bool { return intf.Settings[i].Alternate < intf.Settings[j].Alternate })
			cfg.Interfaces = append(cfg.Interfaces, intf)
		}
		sort.Slice(cfg.Interfaces, func(i, j int) bool { return cfg.Interfaces[i].Number < cfg.Interfaces[j].Number })
		device.Configurations = append(device.Configurations, cfg)
	}
	sort.Slice(device.Configurations, func(i, j int) bool { return device.Configurations[i].Number < device.Configurations[j].Number })
	return device
}

func enumName(name string, code int) string {
	if name == "" {
		return fmt.Sprintf("unknown(%d)", code)
	}
	return name
}

func configurationLayout(cfg gousb.ConfigDesc) string {
	if len(cfg.Interfaces) == 1 && len(cfg.Interfaces[0].AltSettings) == 1 {
		endpoints := cfg.Interfaces[0].AltSettings[0].Endpoints
		if len(endpoints) == 1 {
			for _, endpoint := range endpoints {
				if endpoint.TransferType == gousb.TransferTypeBulk && endpoint.Direction == gousb.EndpointDirectionOut {
					return "bulk-out-only"
				}
			}
		}
	}
	for _, intf := range cfg.Interfaces {
		for _, setting := range intf.AltSettings {
			var hasIn, hasOut bool
			for _, endpoint := range setting.Endpoints {
				if endpoint.TransferType != gousb.TransferTypeBulk {
					continue
				}
				if endpoint.Direction == gousb.EndpointDirectionIn {
					hasIn = true
				} else {
					hasOut = true
				}
			}
			if hasIn && hasOut {
				return "bidirectional-bulk"
			}
		}
	}
	return "unknown"
}
