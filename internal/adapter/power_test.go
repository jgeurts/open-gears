package adapter

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/gousb"
	"github.com/jgeurts/open-gears/internal/protocol"
)

type powerStep struct {
	command byte
	payload []byte
	reply   []byte
	err     error
}

func scriptedPower(t *testing.T, steps []powerStep) bicycleExchange {
	t.Helper()
	next := 0
	t.Cleanup(func() {
		if next != len(steps) {
			t.Errorf("consumed %d of %d power commands", next, len(steps))
		}
	})
	return func(ctx context.Context, command byte, payload []byte) (protocol.Frame, error) {
		t.Helper()
		if next >= len(steps) {
			t.Fatalf("unexpected command %02x/%x", command, payload)
		}
		step := steps[next]
		next++
		if command != step.command || !reflect.DeepEqual(payload, step.payload) {
			t.Fatalf("command %d: got %02x/%x, want %02x/%x", next, command, payload, step.command, step.payload)
		}
		return protocol.Frame{Control: command | 0x20, Payload: step.reply}, step.err
	}
}

func skipPowerWait(context.Context, time.Duration) error { return nil }

func TestBicyclePowerByFirmwareAndStatus(t *testing.T) {
	for _, test := range []struct {
		name     string
		firmware byte
		status   byte
		commands []byte
	}{
		{"modern external power", 0x30, 1, []byte{1, 2}},
		{"modern adapter supply", 0x30, 2, []byte{1, 2, 3}},
	} {
		t.Run(test.name, func(t *testing.T) {
			steps := []powerStep{{command: 5, reply: []byte{test.firmware, 1, 0}}}
			for _, command := range test.commands {
				status := byte(0)
				if command == 2 {
					status = test.status
				}
				steps = append(steps, powerStep{command: 0x1a, payload: []byte{command}, reply: []byte{command, status}})
			}
			steps = append(steps, powerStep{command: 3, payload: []byte{0}, reply: []byte{0}})
			var resets int
			var waits []time.Duration
			err := prepareBicycle(context.Background(), func(context.Context) error { resets++; return nil }, scriptedPower(t, steps), func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil })
			if err != nil || resets != 1 || !reflect.DeepEqual(waits, []time.Duration{3 * time.Second, time.Second, time.Second}) {
				t.Fatalf("setup: resets=%d waits=%v err=%v", resets, waits, err)
			}
		})
	}
}

func TestBicycleMasterRetriesCompletePreparationOnce(t *testing.T) {
	var events []string
	reset := func(context.Context) error { events = append(events, "reset"); return nil }
	exchange := func(_ context.Context, command byte, payload []byte) (protocol.Frame, error) {
		events = append(events, fmt.Sprintf("%02x/%x", command, payload))
		var reply []byte
		switch command {
		case 5:
			reply = []byte{0x30, 1, 0}
		case 0x1a:
			reply = []byte{payload[0], 0}
			if payload[0] == 2 {
				reply[1] = 1
			}
		case 3:
			reply = []byte{0x10}
		default:
			t.Fatalf("unexpected command %02x", command)
		}
		return protocol.Frame{Payload: reply}, nil
	}
	wait := func(_ context.Context, d time.Duration) error {
		events = append(events, "wait "+d.String())
		return nil
	}
	if err := prepareBicycle(context.Background(), reset, exchange, wait); err != nil {
		t.Fatal(err)
	}
	one := []string{"reset", "05/", "wait 3s", "1a/01", "1a/02", "wait 1s", "03/00"}
	want := append(append([]string{}, one...), one...)
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("preparation order: %v", events)
	}
}

func TestBicyclePowerDoesNotStartSupplyWhileCharging(t *testing.T) {
	for _, status := range []byte{3, 4} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			exchange := scriptedPower(t, []powerStep{
				{command: 0x1a, payload: []byte{1}, reply: []byte{1, 0}},
				{command: 0x1a, payload: []byte{2}, reply: []byte{2, status}},
			})
			if err := preparePower(context.Background(), exchange, skipPowerWait); err == nil || !strings.Contains(err.Error(), "not ready") {
				t.Fatalf("charging status %d: %v", status, err)
			}
		})
	}
}

func TestBicyclePowerBusyRetryAndLimit(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprint(ready), func(t *testing.T) {
			calls, waits := 0, 0
			exchange := func(_ context.Context, command byte, payload []byte) (protocol.Frame, error) {
				calls++
				status := byte(5)
				if ready && calls == 2 {
					status = 1
				}
				return protocol.Frame{Payload: []byte{2, status}}, nil
			}
			wait := func(_ context.Context, d time.Duration) error {
				waits++
				if d <= 0 || d > 2*time.Second {
					t.Errorf("invalid retry interval %v", d)
				}
				return nil
			}
			status, err := powerStatus(context.Background(), exchange, wait, 2)
			if ready {
				if err != nil || status != 1 || calls != 2 || waits != 1 {
					t.Fatalf("busy then ready: status=%d calls=%d waits=%d err=%v", status, calls, waits, err)
				}
			} else if err == nil || calls != 31 || waits != 30 {
				t.Fatalf("busy bound: calls=%d waits=%d err=%v", calls, waits, err)
			}
		})
	}
}

func TestBicyclePowerRejectsFailedSupplyAcknowledgement(t *testing.T) {
	steps := []powerStep{
		{command: 0x1a, payload: []byte{1}, reply: []byte{1, 0}},
		{command: 0x1a, payload: []byte{2}, reply: []byte{2, 2}},
	}
	for range 3 {
		steps = append(steps, powerStep{command: 0x1a, payload: []byte{3}, reply: []byte{3, 1}})
	}
	if err := preparePower(context.Background(), scriptedPower(t, steps), skipPowerWait); err == nil {
		t.Fatal("supply rejection was ignored")
	}
}

func TestBicyclePowerRejectsWrongSubcommandEcho(t *testing.T) {
	for _, operation := range []byte{2, 3} {
		t.Run(fmt.Sprint(operation), func(t *testing.T) {
			var steps []powerStep
			for range 3 {
				// An earlier unlock ACK shares control byte 0x3a.
				steps = append(steps, powerStep{command: 0x1a, payload: []byte{operation}, reply: []byte{1, 0}})
			}
			exchange := scriptedPower(t, steps)
			var err error
			if operation == 2 {
				_, err = powerStatus(context.Background(), exchange, skipPowerWait, operation)
			} else {
				err = powerOperation(context.Background(), exchange, skipPowerWait, operation)
			}
			if err == nil {
				t.Fatal("accepted a reply to a different power subcommand")
			}
		})
	}
}

func TestBicyclePowerCancellationStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	exchange := func(context.Context, byte, []byte) (protocol.Frame, error) {
		calls++
		return protocol.Frame{Payload: []byte{2, 5}}, nil
	}
	wait := func(ctx context.Context, _ time.Duration) error { cancel(); return ctx.Err() }
	_, err := powerStatus(ctx, exchange, wait, 2)
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancellation: calls=%d err=%v", calls, err)
	}
}

func TestBicycleRejectsShortFirmwareBeforePowerCommands(t *testing.T) {
	exchange := scriptedPower(t, []powerStep{{command: 5, reply: []byte{0x30}}})
	err := prepareBicycle(context.Background(), func(context.Context) error { return nil }, exchange, skipPowerWait)
	if err == nil || !strings.Contains(err.Error(), "firmware") {
		t.Fatalf("short firmware: %v", err)
	}
}

func TestBicycleRejectsUnsupportedAdapterFirmwareBeforePower(t *testing.T) {
	exchange := scriptedPower(t, []powerStep{{command: 5, reply: []byte{0x29, 9, 0}}})
	err := prepareBicycle(context.Background(), func(context.Context) error { return nil }, exchange, func(context.Context, time.Duration) error {
		t.Fatal("unsupported adapter entered the power phase")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "3.0.0") {
		t.Fatalf("legacy adapter accepted: %v", err)
	}
}

func TestBicycleRoleRetriesTemporaryState(t *testing.T) {
	exchange := scriptedPower(t, []powerStep{
		{command: 3, payload: []byte{0}, reply: []byte{1}},
		{command: 3, payload: []byte{0}, reply: []byte{0}},
	})
	if master, err := bicycleRole(context.Background(), exchange); err != nil || master {
		t.Fatalf("role: master=%v err=%v", master, err)
	}
}

func TestEndBicycleResetValidationAndDeadline(t *testing.T) {
	for _, test := range []struct {
		name      string
		reply     []byte
		err       error
		wantError bool
	}{
		{"success", []byte{0}, nil, false},
		{"short", nil, nil, true},
		{"rejected", []byte{1}, nil, true},
		{"disconnected", nil, gousb.ErrorNoDevice, true},
		{"stalled", nil, gousb.ErrorPipe, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := endBicycle(context.Background(), func(ctx context.Context, command byte, payload []byte) (protocol.Frame, error) {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 3*time.Second || command != 0x10 || len(payload) != 0 {
					t.Fatalf("unbounded or incorrect disconnect reset")
				}
				return protocol.Frame{Payload: test.reply}, test.err
			})
			if (err != nil) != test.wantError || (test.err != nil && !errors.Is(err, test.err)) {
				t.Fatalf("reset result: %v", err)
			}
		})
	}
	if err := (*Connection)(nil).EndBicycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := (&Connection{}).EndBicycle(context.Background()); err != nil {
		t.Fatal(err)
	}
}
