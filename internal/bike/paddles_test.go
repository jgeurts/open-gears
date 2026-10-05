package bike

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/jgeurts/open-gears/internal/adapter"
	"github.com/jgeurts/open-gears/internal/protocol"
)

func paddleFixture() (Snapshot, PaddlePlan) {
	u := Unit{Slot: 2, Series: 5, Number: 1, Part: 1, PartKnown: true, Model: "ST-R785-L", FirmwareVersion: "3.1.0 (revision 0)"}
	u.Paddles, _ = decodePaddles(u, []byte{0x01, 0xab})
	snapshot := Snapshot{Adapter: adapter.Location{Bus: 0, Address: 12, Path: []int{1, 1, 4}}, Role: "slave", Units: []Unit{{Slot: 0, Series: 5, Number: 0, Model: "SM-BTR2"}, u}}
	a := byte(3)
	plan, err := PlanPaddles(snapshot, 2, &a, nil)
	if err != nil {
		panic(err)
	}
	return snapshot, plan
}

func TestPaddlePlanValidationBeforeUSB(t *testing.T) {
	_, valid := paddleFixture()
	cases := map[string]func(*PaddlePlan){
		"version":                 func(p *PaddlePlan) { p.Version = 2 },
		"physical port missing":   func(p *PaddlePlan) { p.Adapter.Path = nil },
		"invalid port":            func(p *PaddlePlan) { p.Adapter.Path = []int{0} },
		"unknown model":           func(p *PaddlePlan) { p.Changes[0].Series = 0xee },
		"unsupported part":        func(p *PaddlePlan) { p.Changes[0].Part = 9 },
		"missing firmware":        func(p *PaddlePlan) { p.Changes[0].FirmwareVersion = "" },
		"short assignments":       func(p *PaddlePlan) { p.Changes[0].BeforeRaw = "01" },
		"invalid assignments":     func(p *PaddlePlan) { p.Changes[0].BeforeRaw = "zzzz" },
		"unverified cleanup slot": func(p *PaddlePlan) { p.Changes[0].Slot = 31 },
		"duplicate slot":          func(p *PaddlePlan) { p.Changes = append(p.Changes, p.Changes[0]) },
		"unsupported new action":  func(p *PaddlePlan) { p.Changes[0].A = 15 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := valid
			p.Changes = append([]PaddleChange(nil), valid.Changes...)
			mutate(&p)
			if err := ValidatePlan(p); err == nil {
				t.Fatal("accepted invalid plan")
			}
		})
	}
	valid.Changes[0].BeforeRaw = "04ab"
	valid.Changes[0].B = 4
	if err := ValidatePlan(valid); err != nil {
		t.Fatalf("rejected preserved model-specific Y action: %v", err)
	}
	valid.Changes[0].B = 5
	if err := ValidatePlan(valid); err == nil {
		t.Fatal("allowed changing a model-specific Y action")
	}
}

func TestPaddlePlanStrictJSON(t *testing.T) {
	_, plan := paddleFixture()
	raw, _ := json.Marshal(plan)
	if _, err := ReadPlan(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]string{
		"unknown":             strings.Replace(string(raw), `"version":1`, `"version":1,"extra":0`, 1),
		"duplicate":           strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1),
		"case alias":          strings.Replace(string(raw), `"a":3`, `"a":3,"A":2`, 1),
		"unicode case alias":  strings.Replace(string(raw), `"slot":2`, `"slot":2,"ſlot":3`, 1),
		"missing zero action": strings.Replace(string(raw), `"a":3,`, ``, 1),
		"null bus":            strings.Replace(string(raw), `"bus":0`, `"bus":null`, 1),
		"null action":         strings.Replace(string(raw), `"a":3`, `"a":null`, 1),
		"second object":       string(raw) + string(raw),
		"oversized":           strings.Repeat(" ", MaxPlanBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadPlan(strings.NewReader(input)); err == nil {
				t.Fatal("accepted malformed plan")
			}
		})
	}
}

func TestPaddleEditSupportRequiresConventionalController(t *testing.T) {
	snapshot, _ := paddleFixture()
	for _, slot := range []byte{0, 4} {
		snapshot.Units[0].Slot = slot
		annotatePaddleSupport(&snapshot)
		if !snapshot.Units[1].PaddleEditSupported {
			t.Fatalf("known battery at slot%d rejected: %s", slot, snapshot.Units[1].PaddleEditReason)
		}
	}
	snapshot.Units = append(snapshot.Units, Unit{Slot: 5, Number: 0, Series: 0x20})
	annotatePaddleSupport(&snapshot)
	if snapshot.Units[1].PaddleEditSupported {
		t.Fatal("enabled unknown/drive controller system")
	}
	snapshot.Units = snapshot.Units[1:2]
	annotatePaddleSupport(&snapshot)
	if snapshot.Units[0].PaddleEditSupported {
		t.Fatal("enabled system without identified battery controller")
	}
}

func TestPaddleEditSupportExcludesUnverifiedCleanupSlot(t *testing.T) {
	snapshot, _ := paddleFixture()
	snapshot.Units[1].Slot = 31
	annotatePaddleSupport(&snapshot)
	if snapshot.Units[1].PaddleEditSupported || !strings.Contains(snapshot.Units[1].PaddleEditReason, "cleanup range") {
		t.Fatalf("slot 31 capability %+v", snapshot.Units[1])
	}
}

type paddleTransport struct {
	assignments   map[byte][]byte
	writes        [][]byte
	queue         []protocol.Frame
	writeError    error
	readError     error
	rejectWrite   bool
	ignoreWrite   bool
	alterReadback bool
	cancelOnWrite context.CancelFunc
}

func (f *paddleTransport) Exchange(ctx context.Context, c byte, p []byte) (protocol.Frame, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Frame{}, err
	}
	if c != 6 {
		return protocol.Frame{}, fmt.Errorf("unexpected adapter command %x", c)
	}
	return protocol.Frame{Control: 0x26, Payload: []byte{0}}, nil
}
func (f *paddleTransport) Send(ctx context.Context, c byte, p []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c != 0x48 || len(p) < 3 {
		return fmt.Errorf("unexpected command %x/%x", c, p)
	}
	slot, group, command := p[0], p[1], p[2]
	switch {
	case group == 4 && command == 0x10:
		f.writes = append(f.writes, append([]byte(nil), p...))
		if !f.ignoreWrite && !f.rejectWrite {
			f.assignments[slot] = append([]byte(nil), p[3:5]...)
		}
		if f.cancelOnWrite != nil {
			f.cancelOnWrite()
		}
		if f.writeError != nil {
			return f.writeError
		}
		reply := []byte{slot, 4, 0x12}
		if f.rejectWrite {
			reply = []byte{slot, 4, 0x13, 0x3b, 0}
		}
		// Wrong-slot and unrelated packets must not satisfy the write acknowledgment.
		f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: []byte{slot + 1, 4, 0x12}}, protocol.Frame{Control: 0x48, Payload: reply})
	case group == 4 && command == 0x14:
		if f.readError != nil {
			return f.readError
		}
		raw := append([]byte(nil), f.assignments[slot]...)
		if f.alterReadback {
			raw[0] = 0x22
		}
		f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: append([]byte{slot, 4, 0x16}, raw...)})
	case group == 0x32 && command == 0x10:
		f.queue = append(f.queue, protocol.Frame{Control: 0x48, Payload: []byte{slot, 0x32, 0x12}})
	default:
		return fmt.Errorf("unexpected unit command %x", p)
	}
	return nil
}
func (f *paddleTransport) Receive(ctx context.Context) (protocol.Frame, error) {
	if err := ctx.Err(); err != nil {
		return protocol.Frame{}, err
	}
	if len(f.queue) == 0 {
		return protocol.Frame{}, io.EOF
	}
	frame := f.queue[0]
	f.queue = f.queue[1:]
	return frame, nil
}
func fixtureTransport(snapshot Snapshot) *paddleTransport {
	f := &paddleTransport{assignments: map[byte][]byte{}}
	for _, u := range snapshot.Units {
		if u.Paddles != nil {
			f.assignments[u.Slot], _ = hex.DecodeString(u.Paddles.Raw)
		}
	}
	return f
}

func TestPaddleApplyPreservesChannelsAndVerifiesTwoByteReply(t *testing.T) {
	snapshot, plan := paddleFixture()
	f := fixtureTransport(snapshot)
	result, err := New(f).applyPaddles(context.Background(), plan, snapshot)
	if err != nil || result.Status != "verified" || result.Snapshot == nil {
		t.Fatalf("apply %+v, %v", result, err)
	}
	want := []byte{2, 4, 0x10, 0x31, 0xab, 0, 0}
	if len(f.writes) != 1 || !bytes.Equal(f.writes[0], want) {
		t.Fatalf("write payloads %x", f.writes)
	}
	if result.Changes[0].ActualRaw != "31ab" || result.Snapshot.Units[1].Paddles.Raw != "31ab" {
		t.Fatalf("readback %+v", result)
	}
}

func TestPaddleApplyNoOpDoesNotWrite(t *testing.T) {
	snapshot, plan := paddleFixture()
	plan.Changes[0].A = 0
	f := fixtureTransport(snapshot)
	result, err := New(f).applyPaddles(context.Background(), plan, snapshot)
	if err != nil || result.Status != "unchanged" || len(f.writes) != 0 {
		t.Fatalf("no-op %+v writes%d err%v", result, len(f.writes), err)
	}
}

func TestPaddleApplyPreflightsAllChangesBeforeWriting(t *testing.T) {
	snapshot, plan := paddleFixture()
	second := snapshot.Units[1]
	second.Slot = 3
	snapshot.Units = append(snapshot.Units, second)
	change := plan.Changes[0]
	change.Slot = 3
	plan.Changes = append(plan.Changes, change)
	for name, mutate := range map[string]func(*Snapshot){
		"stale assignment": func(s *Snapshot) { s.Units[2].Paddles = &Paddles{Raw: "02ab"} },
		"firmware changed": func(s *Snapshot) { s.Units[2].FirmwareVersion = "3.2.0 (revision 0)" },
		"part changed":     func(s *Snapshot) { s.Units[2].Part = 2 },
		"missing firmware": func(s *Snapshot) { s.Units[2].FirmwareVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			current := snapshot
			current.Units = append([]Unit(nil), snapshot.Units...)
			mutate(&current)
			f := fixtureTransport(current)
			result, err := New(f).applyPaddles(context.Background(), plan, current)
			if err == nil || result.Status != "unchanged" || len(f.writes) != 0 {
				t.Fatalf("preflight %+v, %v, writes%d", result, err, len(f.writes))
			}
		})
	}
}

func TestPaddleApplyResolvesAmbiguousWriteWithoutRetry(t *testing.T) {
	for _, test := range []struct {
		name      string
		configure func(*paddleTransport)
		status    string
		wantErr   bool
	}{
		{"lost acknowledgment", func(f *paddleTransport) { f.writeError = context.DeadlineExceeded }, "verified", false},
		{"rejected", func(f *paddleTransport) { f.rejectWrite = true }, "unchanged", true},
		{"no change", func(f *paddleTransport) { f.ignoreWrite = true }, "unchanged", true},
		{"mismatch", func(f *paddleTransport) { f.alterReadback = true }, "unknown", true},
		{"readback unavailable", func(f *paddleTransport) { f.readError = io.EOF }, "unknown", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, plan := paddleFixture()
			f := fixtureTransport(snapshot)
			test.configure(f)
			result, err := New(f).applyPaddles(context.Background(), plan, snapshot)
			if (err != nil) != test.wantErr || result.Status != test.status || len(f.writes) != 1 {
				t.Fatalf("result %+v err%v writes%d", result, err, len(f.writes))
			}
			if test.name == "lost acknowledgment" && result.Changes[0].Warning == "" {
				t.Fatal("verified readback discarded ambiguous write diagnostic")
			}
		})
	}
}

func TestPaddleApplyCancellationStillReadsBackAndCleansService(t *testing.T) {
	snapshot, plan := paddleFixture()
	f := fixtureTransport(snapshot)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.cancelOnWrite = cancel
	s := New(f)
	s.serviceSlots = []byte{2}
	result, err := s.applyPaddles(ctx, plan, snapshot)
	if err != nil || result.Status != "verified" || len(f.writes) != 1 {
		t.Fatalf("cancelled write %+v %v", result, err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("cleanup inherited cancellation: %v", err)
	}
}

func TestPaddleApplyReportsPartialAndCleanupFailure(t *testing.T) {
	snapshot, plan := paddleFixture()
	second := snapshot.Units[1]
	second.Slot = 3
	snapshot.Units = append(snapshot.Units, second)
	change := plan.Changes[0]
	change.Slot = 3
	plan.Changes = append(plan.Changes, change)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f := fixtureTransport(snapshot)
	f.cancelOnWrite = cancel
	result, err := New(f).applyPaddles(ctx, plan, snapshot)
	if !errors.Is(err, context.Canceled) || result.Status != "partial" || len(f.writes) != 1 || result.Changes[0].Status != "verified" || result.Changes[1].Status != "unchanged" {
		t.Fatalf("partial %+v %v", result, err)
	}
	result = ApplyResult{Status: "verified", Changes: []PaddleChangeResult{{Status: "verified"}}}
	finishApply(&result, errors.New("service cleanup failed"))
	if result.Status != "partial" || result.Changes[0].Status != "verified" || result.Error == "" {
		t.Fatalf("cleanup error lost %+v", result)
	}
}

func TestPaddleApplyPreservesUnchangedModelSpecificPaddle(t *testing.T) {
	snapshot, plan := paddleFixture()
	snapshot.Units[1].Paddles, _ = decodePaddles(snapshot.Units[1], []byte{0x04, 0xab})
	plan.Changes[0].BeforeRaw = "04ab"
	plan.Changes[0].B = 4
	f := fixtureTransport(snapshot)
	result, err := New(f).applyPaddles(context.Background(), plan, snapshot)
	if err != nil || result.Status != "verified" || len(f.writes) != 1 || !bytes.Equal(f.writes[0], []byte{2, 4, 0x10, 0x34, 0xab, 0, 0}) {
		t.Fatalf("preserved Y action: %+v %v writes%x", result, err, f.writes)
	}
}
