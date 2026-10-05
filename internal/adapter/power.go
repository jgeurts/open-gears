package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/jgeurts/open-gears/internal/protocol"
)

type bicycleExchange func(context.Context, byte, []byte) (protocol.Frame, error)
type bicycleWait func(context.Context, time.Duration) error

// PrepareBicycle initializes the adapter's transient bicycle power and link
// state. The returned connection replaces c, including when an error occurs.
// Normal bicycle connections require SM-BCR2 firmware 3.0.0 or later.
// Call EndBicycle after ending any component service sessions, then Close.
func (c *Connection) PrepareBicycle(ctx context.Context, options Options) (*Connection, error) {
	reset := func(ctx context.Context) error {
		var err error
		c, err = c.Prepare(ctx, options)
		return err
	}
	exchange := func(ctx context.Context, control byte, payload []byte) (protocol.Frame, error) {
		return c.exchangeBicycle(ctx, control, payload)
	}
	err := prepareBicycle(ctx, reset, exchange, pause)
	return c, err
}

func prepareBicycle(ctx context.Context, reset func(context.Context) error, exchange bicycleExchange, wait bicycleWait) error {
	// The OEM BCR2 connection flow repeats reset and power detection once if
	// its first role probe reports that the adapter is the bus master.
	for attempt := 0; attempt < 2; attempt++ {
		if err := reset(ctx); err != nil {
			return err
		}
		firmware, err := exchange(ctx, 5, nil)
		if err != nil {
			return fmt.Errorf("read adapter firmware before power setup: %w", err)
		}
		if len(firmware.Payload) < 3 {
			return fmt.Errorf("short adapter firmware reply: %x", firmware.Payload)
		}
		if firmware.Payload[0]>>4 < 3 {
			return fmt.Errorf("SM-BCR2 firmware %d.%d.%d is unsupported for bicycle connections; version 3.0.0 or later is required", firmware.Payload[0]>>4, firmware.Payload[0]&15, firmware.Payload[1])
		}
		if err := wait(ctx, 3*time.Second); err != nil {
			return err
		}
		if err := preparePower(ctx, exchange, wait); err != nil {
			return err
		}
		if err := wait(ctx, time.Second); err != nil {
			return err
		}
		master, err := bicycleRole(ctx, exchange)
		if err != nil {
			return err
		}
		if !master {
			// Let the bicycle master's link settle before component discovery.
			return wait(ctx, time.Second)
		}
	}
	return nil
}

func preparePower(ctx context.Context, exchange bicycleExchange, wait bicycleWait) error {
	if err := powerOperation(ctx, exchange, wait, 1); err != nil {
		return err
	}
	status, err := powerStatus(ctx, exchange, wait, 2)
	if err != nil {
		return err
	}
	if status == 3 || status == 4 {
		return fmt.Errorf("adapter power is not ready (status 0x%02x); wait before reconnecting", status)
	}
	if status == 2 {
		return powerOperation(ctx, exchange, wait, 3)
	}
	return nil
}

func powerOperation(ctx context.Context, exchange bicycleExchange, wait bicycleWait, operation byte) error {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		started := time.Now()
		reply, err := exchange(ctx, 0x1a, []byte{operation})
		if err == nil && len(reply.Payload) >= 2 && reply.Payload[0] == operation && reply.Payload[1] == 0 {
			return nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("adapter rejected power operation 0x%02x: %x", operation, reply.Payload)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if attempt < 2 {
			if err := wait(ctx, max(0, 2*time.Second-time.Since(started))); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("adapter power operation 0x%02x: %w", operation, lastErr)
}

func powerStatus(ctx context.Context, exchange bicycleExchange, wait bicycleWait, command byte) (byte, error) {
	var lastErr error
	maxAttempts := 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		started := time.Now()
		reply, err := exchange(ctx, 0x1a, []byte{command})
		if err != nil {
			lastErr = err
		} else if len(reply.Payload) < 2 {
			lastErr = fmt.Errorf("short adapter power status: %x", reply.Payload)
		} else if reply.Payload[0] != command {
			lastErr = fmt.Errorf("unexpected adapter power status reply: %x", reply.Payload)
		} else {
			status := reply.Payload[1]
			switch status {
			case 1, 2, 3, 4:
				return status, nil
			case 5:
				// The OEM waits longer while power initialization is busy.
				maxAttempts = 31
			}
			lastErr = fmt.Errorf("adapter power is not ready (status 0x%02x)", status)
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if attempt+1 < maxAttempts {
			if err := wait(ctx, max(0, 2*time.Second-time.Since(started))); err != nil {
				return 0, err
			}
		}
	}
	return 0, lastErr
}

func bicycleRole(ctx context.Context, exchange bicycleExchange) (bool, error) {
	for attempt := 0; attempt < 3; attempt++ {
		reply, err := exchange(ctx, 3, []byte{0})
		if err != nil {
			return false, fmt.Errorf("probe bicycle link: %w", err)
		}
		if len(reply.Payload) < 1 {
			return false, fmt.Errorf("short bicycle mode reply")
		}
		switch reply.Payload[0] {
		case 0:
			return false, nil
		case 0x10:
			return true, nil
		case 1:
			// The OEM retries a temporarily unavailable mode probe.
		default:
			return false, fmt.Errorf("adapter rejected bicycle mode: %x", reply.Payload)
		}
	}
	return false, fmt.Errorf("adapter bicycle link did not become ready")
}

// Power operations can take longer than ordinary adapter queries. Keep their
// five-second OEM timeout local to bicycle preparation.
func (c *Connection) exchangeBicycle(ctx context.Context, control byte, payload []byte) (protocol.Frame, error) {
	timeout := 3 * time.Second
	if control == 0x1a {
		timeout = 5 * time.Second
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := c.Send(queryCtx, control, payload); err != nil {
		return protocol.Frame{}, err
	}
	for {
		reply, err := c.Receive(queryCtx)
		if err != nil {
			return protocol.Frame{}, err
		}
		if reply.Control != control|0x20 {
			continue
		}
		// Observed power replies echo the subcommand. A delayed unlock ACK
		// must not satisfy a later supply-start or status request.
		if control == 0x1a && len(payload) == 1 && len(reply.Payload) > 0 && reply.Payload[0] != payload[0] {
			continue
		}
		return reply, nil
	}
}

// EndBicycle restores the adapter after component service-mode cleanup. The
// caller should supply a fresh cleanup context even if the operation expired.
// Close must still run if the reset fails.
func (c *Connection) EndBicycle(ctx context.Context) error {
	if c == nil || c.dev == nil {
		return nil
	}
	return endBicycle(ctx, c.Exchange)
}

func endBicycle(ctx context.Context, exchange bicycleExchange) error {
	cleanupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	reply, err := exchange(cleanupCtx, 0x10, nil)
	if err != nil {
		return fmt.Errorf("reset adapter after bicycle session: %w", err)
	}
	return resetReply(reply)
}
