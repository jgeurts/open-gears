// Package bike implements read-only Di2 discovery and model-specific settings.
package bike

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jgeurts/open-gears/internal/adapter"
	"github.com/jgeurts/open-gears/internal/protocol"
)

type Session struct {
	connection   transport
	pcSlot       byte
	master       bool
	serviceSlots []byte
}

type transport interface {
	Exchange(context.Context, byte, []byte) (protocol.Frame, error)
	Send(context.Context, byte, []byte) error
	Receive(context.Context) (protocol.Frame, error)
}

type Paddles struct {
	A      byte              `json:"a"`
	B      byte              `json:"b"`
	C      *byte             `json:"c,omitempty"`
	S      *byte             `json:"s,omitempty"`
	Raw    string            `json:"raw"`
	Labels map[string]string `json:"labels"`
}

type Unit struct {
	Slot            byte     `json:"slot"`
	Series          byte     `json:"series"`
	Number          byte     `json:"number"`
	Part            byte     `json:"part"`
	PartKnown       bool     `json:"part_known"`
	Model           string   `json:"model"`
	FirmwareVersion string   `json:"firmware_version,omitempty"`
	Paddles         *Paddles `json:"paddles,omitempty"`
	ReadErrors      []string `json:"read_errors,omitempty"`
}

type Snapshot struct {
	CapturedAt      string `json:"captured_at"`
	Role            string `json:"role"`
	PCSlot          byte   `json:"pc_slot"`
	SlotBitmap      string `json:"slot_bitmap"`
	Units           []Unit `json:"units"`
	BatteryLevelRaw *byte  `json:"battery_level_raw,omitempty"`
	Note            string `json:"note"`
}

func unitReply(f protocol.Frame, slot, group, command byte, minLength int) ([]byte, bool, error) {
	if f.Control != 0x48 || len(f.Payload) < 3 || f.Payload[0] != slot || f.Payload[1] != group || f.Payload[2]&0xfe != (command|2)&0xfe {
		return nil, false, nil
	}
	p := f.Payload[3:]
	if f.Payload[2]&1 != 0 {
		return p, true, fmt.Errorf("unit %d group 0x%02x command 0x%02x error: %x", slot, group, command, p)
	}
	if len(p) < minLength {
		return nil, true, fmt.Errorf("unit %d short reply to %02x/%02x: %x", slot, group, command, p)
	}
	return append([]byte{}, p...), true, nil
}

func (s *Session) target(ctx context.Context, slot byte) error {
	f, err := s.connection.Exchange(ctx, 6, []byte{slot})
	if err != nil {
		return err
	}
	if len(f.Payload) < 1 || f.Payload[0] != 0 {
		return fmt.Errorf("adapter rejected target slot %d: %x", slot, f.Payload)
	}
	return nil
}

func (s *Session) receiveUnit(ctx context.Context, slot, group, command byte, minLength int) ([]byte, error) {
	for {
		f, err := s.connection.Receive(ctx)
		if err != nil {
			return nil, fmt.Errorf("unit %d command %02x/%02x: %w", slot, group, command, err)
		}
		p, match, err := unitReply(f, slot, group, command, minLength)
		if match {
			return p, err
		}
	}
}

func (s *Session) command(ctx context.Context, slot, group, command byte, p []byte, minLength int) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := s.target(commandCtx, slot); err != nil {
		return nil, err
	}
	if err := s.connection.Send(commandCtx, 0x48, append([]byte{slot, group, command}, p...)); err != nil {
		return nil, err
	}
	return s.receiveUnit(commandCtx, slot, group, command, minLength)
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Start selects the reported DCAS role, obtains occupied slots, and enters the OEM
// transient PC service session. It does not change stored bicycle settings.
// Caller must Close even if Start fails after beginning service mode.
func New(c transport) *Session { return &Session{connection: c} }

func (s *Session) Start(ctx context.Context) ([]byte, error) {
	probe, err := s.connection.Exchange(ctx, 3, []byte{0})
	if err != nil {
		return nil, err
	}
	if len(probe.Payload) < 1 || (probe.Payload[0] != 0 && probe.Payload[0] != 0x10) {
		return nil, fmt.Errorf("adapter rejected bicycle mode: %x", probe.Payload)
	}
	s.master = probe.Payload[0] == 0x10
	wantState := byte(0x80)
	if s.master {
		if _, err := s.connection.Exchange(ctx, 3, []byte{0x20}); err != nil {
			return nil, err
		}
		wantState = 0xa0
	}
	if err := wait(ctx, time.Second); err != nil {
		return nil, err
	}
	state, err := s.connection.Exchange(ctx, 4, nil)
	if err != nil {
		return nil, err
	}
	if len(state.Payload) < 1 || state.Payload[0]&0xe0 != wantState {
		return nil, fmt.Errorf("bicycle link is not ready (state %x); check bike-side cable, battery and direct USB connection", state.Payload)
	}
	s.pcSlot = state.Payload[0] & 0x1f
	if s.master {
		if err := wait(ctx, 2*time.Second); err != nil {
			return nil, err
		}
		bitmap, err := s.connection.Exchange(ctx, 0x1b, nil)
		if err != nil {
			return nil, err
		}
		if len(bitmap.Payload) < 4 {
			return nil, fmt.Errorf("short adapter slot bitmap: %x", bitmap.Payload)
		}
		return append([]byte{0, 0}, bitmap.Payload[:4]...), nil
	}
	if err := wait(ctx, 500*time.Millisecond); err != nil {
		return nil, err
	}
	bitmap, err := s.command(ctx, 0, 1, 0x14, []byte{0}, 6)
	if err != nil {
		return nil, err
	}
	// Starting the slave master component also enters service mode for occupied
	// components. Record them in reverse order so Close ends slots 1–30 before 0.
	mask := binary.LittleEndian.Uint32(bitmap[2:6])
	var cleanupSlots []byte
	for slot := byte(30); slot > 0; slot-- {
		if slot != s.pcSlot && mask&(1<<slot) != 0 {
			cleanupSlots = append(cleanupSlots, slot)
		}
	}
	if err := s.startService(ctx, 0, cleanupSlots...); err != nil {
		return nil, err
	}
	return bitmap, nil
}

func (s *Session) startService(ctx context.Context, slot byte, additionalCleanupSlots ...byte) error {
	serviceCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.target(serviceCtx, slot); err != nil {
		return err
	}
	s.serviceSlots = append(s.serviceSlots, slot) // cleanup covers partial start.
	s.serviceSlots = append(s.serviceSlots, additionalCleanupSlots...)
	if err := s.connection.Send(serviceCtx, 0x48, []byte{slot, 0x32, 0x10, 1, s.pcSlot, 0, 0}); err != nil {
		return err
	}
	if err := wait(serviceCtx, time.Second); err != nil {
		return err
	}
	for _, p := range [][4]byte{{0xa2, 0x2b, 0, 0}, {0x30, 0x0e, 0, 0}, {0x7a, 0x4d, 0, 0}, {0x62, 0x2b, 0, 0}, {0x85, 0xb4, 0, 0}} {
		if err := s.connection.Send(serviceCtx, 0x48, append([]byte{slot, 0x32, 0x30}, p[:]...)); err != nil {
			return err
		}
		if err := wait(serviceCtx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	_, err := s.receiveUnit(serviceCtx, slot, 0x32, 0x10, 0)
	return err
}

func (s *Session) Close() error {
	var result error
	// Bound the complete cleanup even if several component sessions were opened.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for len(s.serviceSlots) > 0 {
		slot := s.serviceSlots[len(s.serviceSlots)-1]
		// Cleanup uses a fresh deadline even when inspection was cancelled.
		_, err := s.command(ctx, slot, 0x32, 0x10, []byte{0, s.pcSlot, 0, 0}, 0)
		result = errors.Join(result, err)
		s.serviceSlots = s.serviceSlots[:len(s.serviceSlots)-1]
		if ctx.Err() != nil {
			return errors.Join(result, fmt.Errorf("service cleanup deadline exceeded; disconnect adapter from bicycle"))
		}
	}
	return result
}

func model(u Unit) string {
	if u.Number == 0 {
		switch u.Series {
		case 5:
			return "SM-BTR2"
		case 0x11:
			return "BT-DN110"
		}
	}
	if u.Number == 1 && u.PartKnown {
		side := ""
		if u.Part == 1 || u.Part == 0x0d {
			side = "-L"
		}
		if u.Part == 2 || u.Part == 0x0c {
			side = "-R"
		}
		if side != "" {
			switch u.Series {
			case 5:
				if u.Part <= 2 {
					return "ST-R785" + side
				}
			case 7:
				if u.Part <= 2 {
					return "ST-6870" + side
				}
			case 0x14:
				if u.Part <= 2 {
					return "ST-R8050" + side
				}
				return "ST-R8070" + side
			case 0x16:
				if u.Part == 0x0c || u.Part == 0x0d {
					return "ST-RX815" + side
				}
			case 0x11:
				if u.Part <= 2 {
					return "ST-R9150" + side
				}
				return "ST-R9170" + side
			}
		}
	}
	if u.Number == 3 {
		switch u.Series {
		case 7:
			return "FD-6870"
		case 0x14:
			return "FD-R8050"
		case 0x16:
			return "FD-RX815"
		}
	}
	if u.Number == 4 {
		switch u.Series {
		case 7:
			return "RD-6870"
		case 0x14:
			if u.PartKnown && u.Part == 3 {
				return "RD-RX805-GS"
			}
			return "RD-R8050"
		case 0x16:
			if u.PartKnown && u.Part == 1 {
				return "RD-RX817"
			}
			if u.PartKnown && u.Part == 4 {
				return "RD-RX815"
			}
		}
	}
	if u.Number == 8 {
		if u.Series == 0x10 {
			return "EW-WU111"
		}
		if u.Series == 0x11 {
			return "EW-WU101"
		}
	}
	return fmt.Sprintf("unknown (%02x/%02x/%02x)", u.Series, u.Number, u.Part)
}

func paddleLength(u Unit) (int, error) {
	if u.Number != 1 {
		return 0, fmt.Errorf("unit is not an identified shifter")
	}
	if u.Series == 5 && (u.Part == 1 || u.Part == 2) {
		return 4, nil
	}
	if u.Series == 7 && (u.Part == 1 || u.Part == 2) {
		return 4, nil
	}
	if u.Series == 0x16 && (u.Part == 0x0c || u.Part == 0x0d) {
		return 4, nil
	}
	if (u.Series == 0x14 || u.Series == 0x11) && (u.Part == 1 || u.Part == 2 || u.Part == 0x0c || u.Part == 0x0d) {
		return 4, nil
	}
	return 0, fmt.Errorf("paddle layout not validated for %02x/%02x/%02x", u.Series, u.Number, u.Part)
}

func label(v byte) string {
	switch v {
	case 0:
		return "front-up"
	case 1:
		return "front-down"
	case 2:
		return "rear-up"
	case 3:
		return "rear-down"
	case 15:
		return "unassigned"
	default:
		return fmt.Sprintf("model-specific (%x)", v)
	}
}

func decodePaddles(u Unit, raw []byte) (*Paddles, error) {
	n, err := paddleLength(u)
	if err != nil {
		return nil, err
	}
	if len(raw) < n {
		return nil, fmt.Errorf("short paddle reply: %x", raw)
	}
	p := &Paddles{A: raw[0] >> 4, B: raw[0] & 15, Raw: hex.EncodeToString(raw[:n])}
	c, s := raw[1]>>4, raw[1]&15
	p.C, p.S = &c, &s
	p.Labels = map[string]string{"a": label(p.A), "b": label(p.B), "c": label(c), "s": label(s)}
	return p, nil
}

func version(p []byte) string {
	return fmt.Sprintf("%d.%d.%d (revision %d)", p[0]>>4, p[0]&15, p[1], p[2])
}

func (s *Session) Inspect(ctx context.Context) (snapshot Snapshot, result error) {
	bitmap, err := s.Start(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot = Snapshot{CapturedAt: time.Now().UTC().Format(time.RFC3339), PCSlot: s.pcSlot, SlotBitmap: hex.EncodeToString(bitmap), Units: []Unit{}, Note: "Live read-only settings. X=A, Y=B on identified road shifters; battery level is raw, not a percentage."}
	snapshot.Role = "slave"
	if s.master {
		snapshot.Role = "master"
	}
	mask := binary.LittleEndian.Uint32(bitmap[2:6])
	for slot := byte(0); slot < 32; slot++ {
		if slot == s.pcSlot || mask&(1<<slot) == 0 {
			continue
		}
		if s.master {
			if err := s.startService(ctx, slot); err != nil {
				return snapshot, err
			}
		}
		stock, err := s.command(ctx, slot, 1, 0x1c, []byte{0}, 2)
		if err != nil {
			return snapshot, err
		}
		u := Unit{Slot: slot, Series: stock[0], Number: stock[1]}
		if u.Number == 1 || u.Number == 3 || u.Number == 4 {
			group, command := byte(0x32), byte(0x9c)
			if u.Number == 1 {
				group = 4
				command = 0x0c
			}
			p, err := s.command(ctx, slot, group, command, []byte{0}, 1)
			if err != nil {
				u.ReadErrors = append(u.ReadErrors, err.Error())
			} else {
				u.Part = p[0]
				u.PartKnown = true
			}
		}
		u.Model = model(u)
		fw, err := s.command(ctx, slot, 1, 0x2c, []byte{0}, 3)
		if err != nil {
			u.ReadErrors = append(u.ReadErrors, err.Error())
		} else {
			u.FirmwareVersion = version(fw)
		}
		if n, err := paddleLength(u); err == nil && u.PartKnown {
			raw, err := s.command(ctx, slot, 4, 0x14, make([]byte, n), n)
			if err != nil {
				u.ReadErrors = append(u.ReadErrors, err.Error())
			} else {
				u.Paddles, err = decodePaddles(u, raw)
				if err != nil {
					u.ReadErrors = append(u.ReadErrors, err.Error())
				}
			}
		}
		if u.Slot == 0 && u.Number == 0 {
			p, err := s.command(ctx, 0, 0x26, 0x14, []byte{0}, 1)
			if err == nil {
				level := p[0]
				snapshot.BatteryLevelRaw = &level
			} else {
				u.ReadErrors = append(u.ReadErrors, err.Error())
			}
		}
		snapshot.Units = append(snapshot.Units, u)
	}
	if len(snapshot.Units) == 0 {
		return snapshot, fmt.Errorf("no bicycle components detected; check the bicycle-side plug and battery")
	}
	return snapshot, nil
}

// Inspect opens and closes both layers, including a partially started service
// session. Cleanup failures must be visible to the caller.
func Inspect(ctx context.Context, options adapter.Options) (snapshot Snapshot, result error) {
	c, err := adapter.Open(ctx, options)
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { result = errors.Join(result, c.Close()) }()
	c, err = c.Prepare(ctx, options)
	if err != nil {
		return Snapshot{}, err
	}
	s := New(c)
	defer func() { result = errors.Join(result, s.Close()) }()
	return s.Inspect(ctx)
}
