package bike

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/jgeurts/open-gears/internal/protocol"
)

func TestUnitReplyMatchingAndErrors(t *testing.T) {
	f := protocol.Frame{Control: 0x48, Payload: []byte{3, 4, 0x16, 0x32, 0xff, 0, 0}}
	p, match, err := unitReply(f, 3, 4, 0x14, 4)
	if err != nil || !match || len(p) != 4 || p[0] != 0x32 {
		t.Fatalf("reply %x %v %v", p, match, err)
	}
	if _, match, _ := unitReply(f, 2, 4, 0x14, 4); match {
		t.Fatal("matched wrong slot")
	}
	f.Payload[2] = 0x17
	if _, match, err := unitReply(f, 3, 4, 0x14, 4); !match || err == nil {
		t.Fatal("accepted error reply")
	}
}

type fakeTransport struct {
	exchange func(byte, []byte) (protocol.Frame, error)
	send     func(byte, []byte) error
	queue    []protocol.Frame
}

func (f *fakeTransport) Exchange(ctx context.Context, c byte, p []byte) (protocol.Frame, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Frame{}, err
	}
	return f.exchange(c, p)
}
func (f *fakeTransport) Send(ctx context.Context, c byte, p []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.send(c, p)
}
func (f *fakeTransport) Receive(ctx context.Context) (protocol.Frame, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Frame{}, err
	}
	if len(f.queue) == 0 {
		return protocol.Frame{}, errors.New("no scripted response")
	}
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r, nil
}

func TestMasterModeWithNoUnitsDoesNotStartService(t *testing.T) {
	step := 0
	f := &fakeTransport{}
	f.exchange = func(c byte, p []byte) (protocol.Frame, error) {
		step++
		want := []byte{3, 3, 4, 0x1b}[step-1]
		if c != want {
			t.Fatalf("step %d: command %x want %x", step, c, want)
		}
		payload := []byte{0x10}
		switch step {
		case 2:
			if len(p) != 1 || p[0] != 0x20 {
				t.Fatalf("master mode payload %x", p)
			}
			payload = []byte{0}
		case 3:
			payload = []byte{0xa0}
		case 4:
			payload = make([]byte, 4)
		}
		return protocol.Frame{Control: c | 0x20, Payload: payload}, nil
	}
	f.send = func(byte, []byte) error { t.Fatal("started service without a component"); return nil }
	s := New(f)
	snapshot, err := s.Inspect(context.Background())
	if err == nil || snapshot.Role != "master" || len(snapshot.Units) != 0 || step != 4 {
		t.Fatalf("snapshot %+v, %v", snapshot, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSlaveInspectionAndServiceCleanup(t *testing.T) {
	f := &fakeTransport{}
	var ended []byte
	f.exchange = func(c byte, p []byte) (protocol.Frame, error) {
		payload := []byte{0}
		if c == 4 {
			payload = []byte{0x81}
		}
		if c == 6 && (len(p) != 1 || (p[0] != 0 && p[0] != 2)) {
			t.Fatalf("wrong target %x", p)
		}
		return protocol.Frame{Control: c | 0x20, Payload: payload}, nil
	}
	f.send = func(c byte, p []byte) error {
		if c != 0x48 || len(p) < 3 {
			return fmt.Errorf("unexpected command %x/%x", c, p)
		}
		slot, group, cmd := p[0], p[1], p[2]
		var reply []byte
		switch {
		case group == 1 && cmd == 0x14:
			reply = []byte{0, 0, 7, 0, 0, 0}
		case group == 0x32 && cmd == 0x10:
			if p[3] == 1 {
				return nil
			}
			ended = append(ended, slot)
		case group == 0x32 && cmd == 0x30:
			if p[3] != 0x85 {
				return nil
			}
			cmd = 0x10
		case group == 1 && cmd == 0x1c:
			if slot == 0 {
				reply = []byte{5, 0}
			} else {
				reply = []byte{5, 1}
			}
		case group == 4 && cmd == 0x0c:
			reply = []byte{1}
		case group == 1 && cmd == 0x2c:
			reply = []byte{0x34, 5, 0}
		case group == 4 && cmd == 0x14:
			reply = []byte{0x01, 0xab, 0, 0}
		case group == 0x26 && cmd == 0x14:
			reply = []byte{2}
		default:
			return fmt.Errorf("unexpected unit command %x", p)
		}
		f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: append([]byte{slot, group, cmd | 2}, reply...)})
		return nil
	}
	s := New(f)
	snapshot, err := s.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Role != "slave" || len(snapshot.Units) != 2 || snapshot.Units[1].Model != "ST-R785-L" || snapshot.Units[1].Paddles == nil || *snapshot.Units[1].Paddles.C != 10 {
		t.Fatalf("incorrect snapshot %+v", snapshot)
	}
	if err := s.Close(); err != nil || !slices.Equal(ended, []byte{2, 0}) {
		t.Fatalf("cleanup %v, ended %v", err, ended)
	}
	ended = nil
	if err := s.Close(); err != nil || len(ended) != 0 {
		t.Fatal("cleanup repeated")
	}
}

func TestPartialServiceStartGetsFreshCleanupContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fakeTransport{}
	var ended []byte
	f.exchange = func(c byte, p []byte) (protocol.Frame, error) {
		return protocol.Frame{Control: c | 0x20, Payload: []byte{0}}, nil
	}
	f.send = func(c byte, p []byte) error {
		if p[3] == 1 {
			cancel()
			return context.Canceled
		}
		ended = append(ended, p[0])
		f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: []byte{p[0], 0x32, 0x12}})
		return nil
	}
	s := New(f)
	if err := s.startService(ctx, 0, 7, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("start %v", err)
	}
	if err := s.Close(); err != nil || !slices.Equal(ended, []byte{2, 7, 0}) {
		t.Fatalf("cancelled cleanup %v, ended %v", err, ended)
	}
}

func TestSlaveStartupCancellationCleansOccupiedComponents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ended []byte
	f := &fakeTransport{}
	f.exchange = func(c byte, p []byte) (protocol.Frame, error) {
		payload := []byte{0}
		if c == 4 {
			payload = []byte{0x81} // the PC occupies slot 1.
		}
		return protocol.Frame{Control: c | 0x20, Payload: payload}, nil
	}
	f.send = func(c byte, p []byte) error {
		switch {
		case p[1] == 1 && p[2] == 0x14:
			// Include the PC slot and reserved slot 31; neither receives OFF.
			f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: []byte{0, 1, 0x16, 0, 0, 0x87, 0, 0, 0xc0}})
		case p[1] == 0x32 && p[2] == 0x10 && p[3] == 1:
			cancel()
			return context.Canceled
		case p[1] == 0x32 && p[2] == 0x10 && p[3] == 0:
			ended = append(ended, p[0])
			f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: []byte{p[0], 0x32, 0x12}})
		default:
			return fmt.Errorf("unexpected command %x/%x", c, p)
		}
		return nil
	}
	s := New(f)
	if _, err := s.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("start %v", err)
	}
	if err := s.Close(); err != nil || !slices.Equal(ended, []byte{2, 7, 30, 0}) {
		t.Fatalf("cleanup %v, slots %v", err, ended)
	}
}

func TestFailedTargetDoesNotRegisterServiceCleanup(t *testing.T) {
	failure := errors.New("target unavailable")
	f := &fakeTransport{
		exchange: func(byte, []byte) (protocol.Frame, error) { return protocol.Frame{}, failure },
		send:     func(byte, []byte) error { t.Fatal("sent after target failed"); return nil },
	}
	s := New(f)
	if err := s.startService(context.Background(), 0, 7, 2); !errors.Is(err, failure) {
		t.Fatalf("start %v", err)
	}
	if err := s.Close(); err != nil || len(s.serviceSlots) != 0 {
		t.Fatalf("cleanup %v, slots %v", err, s.serviceSlots)
	}
}

func TestOccupiedMasterCleanupContinuesAfterFailure(t *testing.T) {
	failure := errors.New("first OFF send failed")
	var ended []byte
	f := &fakeTransport{}
	f.exchange = func(c byte, p []byte) (protocol.Frame, error) {
		payload := []byte{0}
		switch {
		case c == 3 && p[0] == 0:
			payload = []byte{0x10}
		case c == 4:
			payload = []byte{0xa1}
		case c == 0x1b:
			payload = []byte{0x16, 0, 0, 0} // components 2 and 4, PC slot 1.
		}
		return protocol.Frame{Control: c | 0x20, Payload: payload}, nil
	}
	f.send = func(c byte, p []byte) error {
		if c != 0x48 || len(p) < 3 {
			return fmt.Errorf("unexpected command %x/%x", c, p)
		}
		slot, group, cmd := p[0], p[1], p[2]
		var reply []byte
		switch {
		case group == 0x32 && cmd == 0x10:
			if p[3] == 1 {
				return nil
			}
			ended = append(ended, slot)
			if slot == 4 {
				return failure
			}
		case group == 0x32 && cmd == 0x30:
			if p[3] != 0x85 {
				return nil
			}
			cmd = 0x10
		case group == 1 && cmd == 0x1c:
			reply = []byte{0xee, 8}
		case group == 1 && cmd == 0x2c:
			reply = []byte{0x10, 1, 0}
		default:
			return fmt.Errorf("unexpected command %x", p)
		}
		f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: append([]byte{slot, group, cmd | 2}, reply...)})
		return nil
	}
	s := New(f)
	snapshot, err := s.Inspect(context.Background())
	if err != nil || snapshot.Role != "master" || len(snapshot.Units) != 2 || snapshot.Units[0].Slot != 2 || snapshot.Units[1].Slot != 4 {
		t.Fatalf("inspection %+v, %v", snapshot, err)
	}
	if err := s.Close(); !errors.Is(err, failure) || !slices.Equal(ended, []byte{4, 2}) {
		t.Fatalf("cleanup %v, slots %v", err, ended)
	}
	ended = nil
	if err := s.Close(); err != nil || len(ended) != 0 {
		t.Fatalf("repeated cleanup %v, slots %v", err, ended)
	}
}

func TestPaddlesPreserveUnusedChannels(t *testing.T) {
	u := Unit{Series: 5, Number: 1, Part: 1}
	p, err := decodePaddles(u, []byte{0x01, 0xab, 0, 0})
	if err != nil || p.A != 0 || p.B != 1 || p.C == nil || *p.C != 10 || p.S == nil || *p.S != 11 {
		t.Fatalf("paddles %+v %v", p, err)
	}
	if _, err := decodePaddles(Unit{Series: 0xee, Number: 1, Part: 1}, []byte{0x32}); err == nil {
		t.Fatal("guessed unknown shifter format")
	}
}

func TestGRXPaddleLayoutsRequireEvidencedPartNumbers(t *testing.T) {
	for _, part := range []byte{0x0c, 0x0d} {
		if n, err := paddleLength(Unit{Series: 0x16, Number: 1, Part: part}); err != nil || n != 4 {
			t.Fatalf("GRX part %x: length %d, %v", part, n, err)
		}
	}
	for _, part := range []byte{1, 2} {
		if _, err := paddleLength(Unit{Series: 0x16, Number: 1, Part: part}); err == nil {
			t.Fatalf("accepted unsupported GRX part %x", part)
		}
	}
}
