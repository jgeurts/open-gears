// Package capture reads offline USB events. It never accesses USB hardware.
package capture

import (
	"bufio"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

const Version = 1

type Setup struct {
	RequestType uint8  `json:"request_type"`
	Request     uint8  `json:"request"`
	Value       uint16 `json:"value"`
	Index       uint16 `json:"index"`
	Length      uint16 `json:"length"`
}

// Event is one captured USB event, not necessarily a complete application packet.
// Submission/completion events are preserved, including failed requests.
type Event struct {
	Version      int    `json:"version"`
	Frame        int    `json:"frame"`
	Time         string `json:"time,omitempty"`
	Bus          int    `json:"bus"`
	Address      int    `json:"address"`
	Endpoint     uint8  `json:"endpoint"`
	Direction    string `json:"direction"`
	TransferType string `json:"transfer_type,omitempty"`
	Stage        string `json:"stage,omitempty"`
	URB          string `json:"urb,omitempty"`
	Status       string `json:"status,omitempty"`
	Payload      string `json:"payload"`
	Setup        *Setup `json:"setup,omitempty"`
}

// Filter uses -1 for any bus/address; bus 0 is valid on macOS.
type Filter struct{ Bus, Address int }

func normalizeHex(s string) (string, error) {
	s = strings.NewReplacer(":", "", " ", "", "\t", "").Replace(s)
	b, err := hex.DecodeString(s)
	if err != nil {
		return "", fmt.Errorf("invalid payload hex: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func validate(e *Event) error {
	if e.Version != Version {
		return fmt.Errorf("unsupported capture version %d (expected %d)", e.Version, Version)
	}
	if e.Frame < 0 || e.Bus < 0 || e.Address < 0 || e.Address > 127 {
		return fmt.Errorf("invalid frame, bus or USB address")
	}
	if e.Endpoint&0x70 != 0 {
		return fmt.Errorf("invalid USB endpoint 0x%02x", e.Endpoint)
	}
	want := "out"
	if e.Endpoint&0x80 != 0 {
		want = "in"
	}
	if e.Endpoint&0x0f == 0 && e.Setup != nil {
		want = "out"
		if e.Setup.RequestType&0x80 != 0 {
			want = "in"
		}
	}
	if e.Direction != want {
		return fmt.Errorf("direction %q disagrees with endpoint/setup (%s)", e.Direction, want)
	}
	p, err := normalizeHex(e.Payload)
	if err != nil {
		return err
	}
	e.Payload = p
	return nil
}

func Read(r io.Reader) ([]Event, error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 4<<20)
	events := []Event{}
	for line := 1; s.Scan(); line++ {
		if strings.TrimSpace(s.Text()) == "" {
			continue
		}
		var e Event
		d := json.NewDecoder(strings.NewReader(s.Text()))
		d.DisallowUnknownFields()
		if err := d.Decode(&e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		var extra any
		if err := d.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("line %d: unexpected trailing data", line)
		}
		if err := validate(&e); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		events = append(events, e)
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("capture contains no events")
	}
	return events, nil
}

func Write(w io.Writer, events []Event) error {
	e := json.NewEncoder(w)
	for _, event := range events {
		if err := validate(&event); err != nil {
			return err
		}
		if err := e.Encode(event); err != nil {
			return err
		}
	}
	return nil
}

// TSVFields is the matching tshark -T fields export order. Header names, rather
// than positions, are used when importing. occurrence=f avoids aggregate fields.
var TSVFields = []string{"frame.number", "frame.time_relative", "usb.bus_id", "usb.device_address", "usb.endpoint_address", "usb.transfer_type", "usb.urb_type", "usb.urb_status", "usb.capdata", "usb.bmRequestType", "usb.setup.bRequest", "usb.setup.wValue", "usb.setup.wIndex", "usb.setup.wLength", "usb.urb_id", "usb.irp_id", "usb.usbpcap_info", "usb.control.Response"}

func number(s string, bits int) (uint64, error) {
	base := 10
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base = 16
		s = s[2:]
	}
	return strconv.ParseUint(s, base, bits)
}

func ImportTSV(r io.Reader, filter Filter) ([]Event, error) {
	reader := csv.NewReader(r)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	head, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("TSV header: %w", err)
	}
	columns := map[string]int{}
	for i, k := range head {
		columns[strings.TrimPrefix(k, "\ufeff")] = i
	}
	for _, k := range []string{"frame.number", "usb.bus_id", "usb.device_address", "usb.endpoint_address", "usb.capdata"} {
		if _, ok := columns[k]; !ok {
			return nil, fmt.Errorf("missing TSV field %s; use capture fields for the export command", k)
		}
	}
	events := []Event{}
	for row := 2; ; row++ {
		values, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", row, err)
		}
		if len(values) != len(head) {
			return nil, fmt.Errorf("row %d: expected %d columns, got %d", row, len(head), len(values))
		}
		field := func(key string) string {
			if i, ok := columns[key]; ok {
				return values[i]
			}
			return ""
		}
		// Frames without a device address are host/bus metadata, not device transfers.
		if field("usb.device_address") == "" {
			continue
		}
		parse := func(key string, bits int) (uint64, error) {
			v, e := number(field(key), bits)
			if e != nil {
				return 0, fmt.Errorf("row %d field %s: %w", row, key, e)
			}
			return v, nil
		}
		bus, err := parse("usb.bus_id", 16)
		if err != nil {
			return nil, err
		}
		addr, err := parse("usb.device_address", 7)
		if err != nil {
			return nil, err
		}
		if filter.Bus >= 0 && int(bus) != filter.Bus || filter.Address >= 0 && int(addr) != filter.Address {
			continue
		}
		frame, err := parse("frame.number", 32)
		if err != nil {
			return nil, err
		}
		ep, err := parse("usb.endpoint_address", 8)
		if err != nil {
			return nil, err
		}
		e := Event{Version: Version, Frame: int(frame), Time: field("frame.time_relative"), Bus: int(bus), Address: int(addr), Endpoint: uint8(ep), Status: field("usb.urb_status"), Payload: field("usb.capdata"), URB: field("usb.urb_id"), Stage: field("usb.urb_type")}
		if e.URB == "" {
			e.URB = field("usb.irp_id")
		}
		if e.Payload == "" {
			e.Payload = field("usb.control.Response")
		}
		if e.Stage == "" && field("usb.usbpcap_info") != "" {
			v, err := parse("usb.usbpcap_info", 8)
			if err != nil {
				return nil, err
			}
			if v&1 != 0 {
				e.Stage = "C"
			} else {
				e.Stage = "S"
			}
		}
		if field("usb.transfer_type") != "" {
			v, err := parse("usb.transfer_type", 8)
			if err != nil {
				return nil, err
			}
			e.TransferType = map[uint64]string{0: "isochronous", 1: "interrupt", 2: "control", 3: "bulk"}[v]
			if e.TransferType == "" {
				e.TransferType = fmt.Sprintf("unknown:%d", v)
			}
		}
		if field("usb.bmRequestType") != "" {
			var v [5]uint64
			for i, k := range []string{"usb.bmRequestType", "usb.setup.bRequest", "usb.setup.wValue", "usb.setup.wIndex", "usb.setup.wLength"} {
				bits := 16
				if i < 2 {
					bits = 8
				}
				v[i], err = parse(k, bits)
				if err != nil {
					return nil, err
				}
			}
			e.Setup = &Setup{uint8(v[0]), uint8(v[1]), uint16(v[2]), uint16(v[3]), uint16(v[4])}
		}
		e.Direction = "out"
		if e.Endpoint&0x80 != 0 || e.Endpoint&0x0f == 0 && e.Setup != nil && e.Setup.RequestType&0x80 != 0 {
			e.Direction = "in"
		}
		if err := validate(&e); err != nil {
			return nil, fmt.Errorf("row %d: %w", row, err)
		}
		events = append(events, e)
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("no events matched; confirm USB bus/address and exported fields")
	}
	return events, nil
}

type EndpointSummary struct {
	Endpoint     string `json:"endpoint"`
	Events       int    `json:"events"`
	PayloadBytes int    `json:"payload_bytes"`
}
type Summary struct {
	Events       int               `json:"events"`
	PayloadBytes int               `json:"payload_bytes"`
	Endpoints    []EndpointSummary `json:"endpoints"`
	Note         string            `json:"note"`
}

func Analyze(events []Event) Summary {
	s := Summary{Events: len(events), Endpoints: []EndpointSummary{}, Note: "Counts are captured USB events; submission and completion are retained. Payload meaning is unverified."}
	m := map[string]*EndpointSummary{}
	for _, e := range events {
		k := fmt.Sprintf("%d:%d/0x%02x/%s", e.Bus, e.Address, e.Endpoint, e.Direction)
		if m[k] == nil {
			m[k] = &EndpointSummary{Endpoint: k}
		}
		m[k].Events++
		m[k].PayloadBytes += len(e.Payload) / 2
		s.PayloadBytes += len(e.Payload) / 2
	}
	keys := []string{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s.Endpoints = append(s.Endpoints, *m[k])
	}
	return s
}

type Change struct {
	Kind                string `json:"kind"`
	BeforeIndex         int    `json:"before_index"`
	AfterIndex          int    `json:"after_index"`
	Before              *Event `json:"before,omitempty"`
	After               *Event `json:"after,omitempty"`
	FirstByteDifference *int   `json:"first_byte_difference,omitempty"`
}
type Comparison struct {
	Changes []Change `json:"changes"`
	Note    string   `json:"note"`
}

func signature(e Event) string {
	// Addresses and timestamps can change on USB reconnect. Compare the traffic.
	b, _ := json.Marshal(struct {
		Endpoint                                uint8
		Direction, Type, Stage, Status, Payload string
		Setup                                   *Setup
	}{e.Endpoint, e.Direction, e.TransferType, e.Stage, e.Status, e.Payload, e.Setup})
	return string(b)
}

// Diff aligns short insertions/deletions with a bounded lookahead. It describes
// bytes only; USB transfer boundaries may differ between equivalent sessions.
func Diff(a, b []Event) Comparison {
	d := Comparison{Changes: []Change{}, Note: "Indices are zero-based. Alignment uses 32-event lookahead; byte differences do not identify settings."}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		var aSig, bSig string
		if i < len(a) {
			aSig = signature(a[i])
		}
		if j < len(b) {
			bSig = signature(b[j])
		}
		if i < len(a) && j < len(b) && aSig == bSig {
			i++
			j++
			continue
		}
		if i == len(a) {
			d.Changes = append(d.Changes, Change{Kind: "insert", BeforeIndex: i, AfterIndex: j, After: &b[j]})
			j++
			continue
		}
		if j == len(b) {
			d.Changes = append(d.Changes, Change{Kind: "delete", BeforeIndex: i, AfterIndex: j, Before: &a[i]})
			i++
			continue
		}
		insert, del := 0, 0
		for k := 1; k <= 32; k++ {
			if j+k < len(b) && aSig == signature(b[j+k]) {
				insert = k
				break
			}
		}
		for k := 1; k <= 32; k++ {
			if i+k < len(a) && signature(a[i+k]) == bSig {
				del = k
				break
			}
		}
		if insert > 0 && (del == 0 || insert <= del) {
			for k := 0; k < insert; k++ {
				d.Changes = append(d.Changes, Change{Kind: "insert", BeforeIndex: i, AfterIndex: j, After: &b[j]})
				j++
			}
			continue
		}
		if del > 0 {
			for k := 0; k < del; k++ {
				d.Changes = append(d.Changes, Change{Kind: "delete", BeforeIndex: i, AfterIndex: j, Before: &a[i]})
				i++
			}
			continue
		}
		c := Change{Kind: "change", BeforeIndex: i, AfterIndex: j, Before: &a[i], After: &b[j]}
		pa, _ := hex.DecodeString(a[i].Payload)
		pb, _ := hex.DecodeString(b[j].Payload)
		for k := 0; k < len(pa) || k < len(pb); k++ {
			if k >= len(pa) || k >= len(pb) || pa[k] != pb[k] {
				offset := k
				c.FirstByteDifference = &offset
				break
			}
		}
		d.Changes = append(d.Changes, c)
		i++
		j++
	}
	return d
}
