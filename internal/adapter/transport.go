// Package adapter provides the TI USB serial transport used by SM-BCR2.
// Controller firmware is loaded into volatile RAM, never bicycle flash.
package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/google/gousb"
	"github.com/jgeurts/open-gears/internal/capture"
	"github.com/jgeurts/open-gears/internal/protocol"
	"github.com/jgeurts/open-gears/internal/usb"
)

const FirmwareSHA256 = "2a392186cf3d93b6a56514cdcd483a26097bf20b2bfc744bad7bb2fa1fd8bcd6"

type Options struct {
	Bus      int // -1 means any; bus 0 is valid on macOS.
	Address  int // -1 means any.
	Firmware string
	Trace    io.Writer
	recorder *traceRecorder
}

type traceRecorder struct {
	mu       sync.Mutex
	writer   io.Writer
	started  time.Time
	sequence int
	err      error
}

type Connection struct {
	ctx         *gousb.Context
	dev         *gousb.Device
	config      *gousb.Config
	intf        *gousb.Interface
	in          *gousb.InEndpoint
	out         *gousb.OutEndpoint
	decoder     protocol.Decoder
	pending     []protocol.Frame
	trace       *traceRecorder
	interrupts  *interruptReader
	portStarted bool
}

// Modem-status notifications must be consumed even when the caller is not
// reading UART data. The reader outlives operation contexts so cancellation
// cannot stop it before the UART CLOSE request has completed.
type interruptReader struct {
	cancel context.CancelFunc
	done   <-chan error
}

func readInterrupts(read func(context.Context, []byte) (int, error), packetSize int, record func([]byte)) *interruptReader {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		buf := make([]byte, packetSize)
		for {
			n, err := read(ctx, buf)
			if n > 0 {
				record(buf[:n])
			}
			if err != nil {
				if ctx.Err() != nil && errors.Is(err, gousb.TransferCancelled) {
					err = nil
				}
				done <- err
				return
			}
			if ctx.Err() != nil {
				done <- nil
				return
			}
		}
	}()
	return &interruptReader{cancel: cancel, done: done}
}

func (r *interruptReader) stop() error {
	r.cancel()
	if err := <-r.done; err != nil {
		return fmt.Errorf("read UART status: %w", err)
	}
	return nil
}

func wrapFirmware(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 65535 {
		return nil, fmt.Errorf("controller image size must be 1..65535 bytes")
	}
	image := make([]byte, 3, len(raw)+3)
	binary.LittleEndian.PutUint16(image, uint16(len(raw)))
	for _, b := range raw {
		image[2] += b
	}
	return append(image, raw...), nil
}

func validateFirmware(raw []byte) ([]byte, error) {
	hash := fmt.Sprintf("%x", sha256.Sum256(raw))
	if len(raw) != 14336 || hash != FirmwareSHA256 {
		return nil, firmwareMismatch(int64(len(raw)), hash)
	}
	return wrapFirmware(raw)
}

func firmwareMismatch(size int64, hash string) error {
	return fmt.Errorf("unsupported controller image (size %d, SHA256 %s); expected Shimano umpf3410.i51 SHA256 %s", size, hash, FirmwareSHA256)
}

func loadFirmware(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 14337))
	if err != nil {
		return nil, err
	}
	if len(raw) <= 14336 {
		return validateFirmware(raw)
	}
	// Preserve the size/hash diagnostic without allocating a mistaken large file.
	h := sha256.New()
	h.Write(raw)
	n, err := io.Copy(h, f)
	if err != nil {
		return nil, err
	}
	return nil, firmwareMismatch(int64(len(raw))+n, hex.EncodeToString(h.Sum(nil)))
}

func newContext() (ctx *gousb.Context, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("initialize libusb: %v", r)
		}
	}()
	return gousb.NewContext(), nil
}

func selected(options Options) (usb.Device, error) {
	devices, err := usb.Discover()
	if err != nil {
		return usb.Device{}, err
	}
	var matches []usb.Device
	for _, d := range devices {
		if options.Bus >= 0 && options.Bus != d.Bus || options.Address >= 0 && options.Address != d.Address {
			continue
		}
		matches = append(matches, d)
	}
	if len(matches) == 0 {
		return usb.Device{}, fmt.Errorf("SM-BCR2 not found; use devices to check bus/address")
	}
	if len(matches) != 1 {
		return usb.Device{}, fmt.Errorf("%d SM-BCR2 adapters match; select --bus and --address", len(matches))
	}
	return matches[0], nil
}

func openDevice(target usb.Device) (*Connection, error) {
	ctx, err := newContext()
	if err != nil {
		return nil, err
	}
	devices, err := ctx.OpenDevices(func(d *gousb.DeviceDesc) bool {
		return d.Vendor == usb.VendorID && d.Product == usb.ProductID && d.Bus == target.Bus && d.Address == target.Address && slices.Equal(d.Path, target.Path)
	})
	if err != nil || len(devices) != 1 {
		for _, d := range devices {
			d.Close()
		}
		ctx.Close()
		if err == nil {
			err = fmt.Errorf("adapter disappeared or selection became ambiguous")
		}
		return nil, err
	}
	devices[0].ControlTimeout = time.Second
	return &Connection{ctx: ctx, dev: devices[0]}, nil
}

// Initialize loads only the fingerprinted OEM USB-controller RAM image. It
// performs no EEPROM writes and no Di2 application commands.
func Initialize(ctx context.Context, options Options) (usb.Device, error) {
	if options.Firmware == "" {
		return usb.Device{}, fmt.Errorf("--firmware must name the OEM umpf3410.i51 controller image")
	}
	image, err := loadFirmware(options.Firmware)
	if err != nil {
		return usb.Device{}, err
	}
	target, err := selected(options)
	if err != nil {
		return usb.Device{}, err
	}
	if len(target.Configurations) != 1 || target.Configurations[0].Number != 1 || target.Configurations[0].Layout != "bulk-out-only" {
		return usb.Device{}, fmt.Errorf("adapter is not in the verified TI boot layout; no firmware sent")
	}
	c, err := openDevice(target)
	if err != nil {
		return usb.Device{}, err
	}
	defer c.Close()
	c.config, err = c.dev.Config(1)
	if err != nil {
		return usb.Device{}, err
	}
	c.intf, err = c.config.Interface(0, 0)
	if err != nil {
		return usb.Device{}, err
	}
	c.out, err = c.intf.OutEndpoint(1)
	if err != nil {
		return usb.Device{}, err
	}
	for offset := 0; offset < len(image); {
		end := min(offset+64, len(image))
		chunk := image[offset:end]
		writeCtx, cancel := context.WithTimeout(ctx, time.Second)
		n, err := c.out.WriteContext(writeCtx, chunk)
		cancel()
		if err != nil {
			return usb.Device{}, fmt.Errorf("controller RAM upload at byte %d: %w; unplug/replug before retrying", offset, err)
		}
		if n != len(chunk) {
			return usb.Device{}, fmt.Errorf("short controller upload at byte %d; unplug/replug before retrying", offset)
		}
		// Never include the proprietary controller image in application traces.
		offset = end
	}
	c.intf.Close()
	c.intf = nil
	if err := c.config.Close(); err != nil {
		return usb.Device{}, err
	}
	c.config = nil
	if err := pause(ctx, 100*time.Millisecond); err != nil {
		return usb.Device{}, err
	}
	resetErr := c.dev.Reset()
	if err := c.Close(); err != nil && !errors.Is(err, gousb.ErrorNoDevice) {
		return usb.Device{}, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := pause(ctx, 100*time.Millisecond); err != nil {
			return usb.Device{}, err
		}
		devices, err := usb.Discover()
		if err != nil {
			continue
		}
		for _, d := range devices {
			if d.Bus == target.Bus && slices.Equal(d.Path, target.Path) {
				for _, cfg := range d.Configurations {
					if cfg.Number == 2 && cfg.Layout == "bidirectional-bulk" {
						return d, nil
					}
				}
			}
		}
	}
	return usb.Device{}, fmt.Errorf("controller runtime did not enumerate (reset result %v); unplug/replug adapter", resetErr)
}

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func uartConfig() []byte {
	// TI config fields are big endian; divisor round(923077/38400) = 24.
	return []byte{0, 24, 0x60, 0, 3, 0, 0, 0x11, 0x13, 0}
}

func Open(ctx context.Context, options Options) (*Connection, error) {
	target, err := selected(options)
	if err != nil {
		return nil, err
	}
	if !runtimeLayout(target) {
		if options.Firmware == "" {
			return nil, fmt.Errorf("SM-BCR2 needs its USB controller RAM firmware; provide --firmware path/to/umpf3410.i51 (see README)")
		}
		target, err = Initialize(ctx, options)
		if err != nil {
			return nil, err
		}
	}
	c, err := openDevice(target)
	if err != nil {
		return nil, err
	}
	c.trace = options.recorder
	if c.trace == nil && options.Trace != nil {
		c.trace = &traceRecorder{writer: options.Trace, started: time.Now()}
	}
	fail := func(err error) (*Connection, error) { return nil, errors.Join(err, c.Close()) }
	c.config, err = c.dev.Config(2)
	if err != nil {
		return fail(err)
	}
	c.intf, err = c.config.Interface(0, 0)
	if err != nil {
		return fail(err)
	}
	var inNumber, outNumber, interruptNumber int
	for _, ep := range c.intf.Setting.Endpoints {
		if ep.TransferType == gousb.TransferTypeInterrupt && ep.Direction == gousb.EndpointDirectionIn {
			interruptNumber = ep.Number
		}
		if ep.TransferType == gousb.TransferTypeBulk {
			if ep.Direction == gousb.EndpointDirectionIn {
				inNumber = ep.Number
			} else {
				outNumber = ep.Number
			}
		}
	}
	if inNumber == 0 || outNumber == 0 {
		return fail(fmt.Errorf("runtime interface lacks serial bulk endpoints"))
	}
	if interruptNumber == 0 {
		return fail(fmt.Errorf("runtime interface lacks UART status interrupt endpoint"))
	}
	c.in, err = c.intf.InEndpoint(inNumber)
	if err != nil {
		return fail(err)
	}
	c.out, err = c.intf.OutEndpoint(outNumber)
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	interrupt, err := c.intf.InEndpoint(interruptNumber)
	if err != nil {
		return fail(err)
	}
	c.interrupts = readInterrupts(interrupt.ReadContext, interrupt.Desc.MaxPacketSize, func(data []byte) {
		c.record(byte(interrupt.Desc.Address), "interrupt", data, nil)
	})
	if err := c.control(5, 0, 3, uartConfig()); err != nil {
		return fail(fmt.Errorf("set 38400 baud: %w", err))
	}
	// SET_CONFIG asserts modem lines. Clear RTS/DTR/loopback at UART1 MCR.
	if err := c.control(0x80, 0, 5, []byte{0x30, 1, 1, 0, 0, 0xff, 0xa4, 0x34, 0}); err != nil {
		return fail(fmt.Errorf("clear modem lines: %w", err))
	}
	if err := c.control(6, 0x89, 3, nil); err != nil {
		return fail(fmt.Errorf("open UART: %w", err))
	}
	c.portStarted = true
	if err := c.control(8, 0, 3, nil); err != nil {
		return fail(fmt.Errorf("start UART: %w", err))
	}
	return c, nil
}

func (c *Connection) record(endpoint byte, kind string, payload []byte, setup *capture.Setup) {
	if c.trace == nil {
		return
	}
	c.trace.mu.Lock()
	defer c.trace.mu.Unlock()
	if c.trace.err != nil {
		return
	}
	c.trace.sequence++
	direction := "out"
	if endpoint&0x80 != 0 {
		direction = "in"
	}
	e := capture.Event{Version: capture.Version, Frame: c.trace.sequence, Time: fmt.Sprintf("%.6f", time.Since(c.trace.started).Seconds()), Bus: c.dev.Desc.Bus, Address: c.dev.Desc.Address, Endpoint: endpoint, Direction: direction, TransferType: kind, Stage: "application", Payload: hex.EncodeToString(payload), Setup: setup}
	// Diagnostic storage must never interrupt transport or bicycle cleanup.
	// Keep the first failure for Close, after service-end commands have run.
	if err := json.NewEncoder(c.trace.writer).Encode(e); err != nil {
		c.trace.err = fmt.Errorf("write adapter trace: %w", err)
	}
}

func (c *Connection) control(request byte, value, index uint16, data []byte) error {
	n, err := c.dev.Control(0x40, request, value, index, data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	c.record(0, "control", data, &capture.Setup{RequestType: 0x40, Request: request, Value: value, Index: index, Length: uint16(len(data))})
	return nil
}

func (c *Connection) Send(ctx context.Context, control byte, payload []byte) error {
	wire := protocol.Encode(control, payload)
	n, err := c.out.WriteContext(ctx, wire)
	if err != nil {
		return err
	}
	if n != len(wire) {
		return io.ErrShortWrite
	}
	c.record(byte(c.out.Desc.Address), "bulk", wire, nil)
	return nil
}

func (c *Connection) Receive(ctx context.Context) (protocol.Frame, error) {
	buf := make([]byte, 4096)
	for {
		if len(c.pending) > 0 {
			f := c.pending[0]
			c.pending = c.pending[1:]
			return f, nil
		}
		n, err := c.in.ReadContext(ctx, buf)
		if n > 0 {
			c.record(byte(c.in.Desc.Address), "bulk", buf[:n], nil)
			frames, decodeErr := c.decoder.Feed(buf[:n])
			if decodeErr != nil {
				return protocol.Frame{}, decodeErr
			}
			c.pending = append(c.pending, frames...)
		}
		if len(c.pending) > 0 {
			continue
		}
		if err != nil {
			return protocol.Frame{}, err
		}
		if err := ctx.Err(); err != nil {
			return protocol.Frame{}, err
		}
	}
}

func (c *Connection) Exchange(ctx context.Context, control byte, payload []byte) (protocol.Frame, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := c.Send(queryCtx, control, payload); err != nil {
		return protocol.Frame{}, err
	}
	for {
		f, err := c.Receive(queryCtx)
		if err != nil {
			return protocol.Frame{}, fmt.Errorf("adapter command 0x%02x: %w", control, err)
		}
		if f.Control == control|0x20 {
			return f, nil
		}
	}
}

type Info struct {
	LinkReplyHex     string `json:"link_reply_hex"`
	FirmwareReplyHex string `json:"firmware_reply_hex"`
	FirmwareVersion  string `json:"firmware_version"`
	Note             string `json:"note"`
}

// Prepare follows the legacy BCR2 reset/reconnection and power-unlock sequence.
// The reset affects the adapter, and may require reloading its volatile image.
// The returned connection replaces c and must be closed by the caller.
func (c *Connection) Prepare(ctx context.Context, options Options) (*Connection, error) {
	first, firstErr := c.Exchange(ctx, 0x10, nil)
	if firstErr == nil {
		firstErr = resetReply(first)
	}
	original := c.dev.Desc
	deadline := time.Now().Add(3 * time.Second)
	var target usb.Device
	for time.Now().Before(deadline) {
		if err := pause(ctx, 200*time.Millisecond); err != nil {
			return c, err
		}
		target = usb.Device{}
		devices, err := usb.Discover()
		if err != nil {
			continue
		}
		for _, d := range devices {
			if d.Bus == original.Bus && slices.Equal(d.Path, original.Path) {
				target = d
			}
		}
		if target.VendorID != "" && (target.Address != original.Address || !runtimeLayout(target)) {
			break
		}
	}
	if firstErr != nil || target.VendorID == "" || target.Address != original.Address || !runtimeLayout(target) {
		closeErr := c.closeHardware()
		if closeErr != nil && !errors.Is(closeErr, gousb.ErrorNoDevice) {
			return c, fmt.Errorf("close adapter after reset: %w", closeErr)
		}
		deadline = time.Now().Add(10 * time.Second)
		target = usb.Device{}
		for time.Now().Before(deadline) {
			devices, err := usb.Discover()
			if err == nil {
				for _, d := range devices {
					if d.Bus == original.Bus && slices.Equal(d.Path, original.Path) {
						target = d
					}
				}
			}
			if target.VendorID != "" {
				break
			}
			if err := pause(ctx, 500*time.Millisecond); err != nil {
				return c, err
			}
		}
		if target.VendorID == "" {
			return c, fmt.Errorf("adapter did not reconnect after reset (first reset: %v)", firstErr)
		}
		options.Bus, options.Address = target.Bus, target.Address
		options.recorder = c.trace
		reopened, err := Open(ctx, options)
		if err != nil {
			return c, fmt.Errorf("reopen adapter after reset: %w", err)
		}
		c = reopened
	}
	second, err := c.Exchange(ctx, 0x10, nil)
	if err != nil {
		return c, fmt.Errorf("second adapter reset: %w", err)
	}
	if err := resetReply(second); err != nil {
		return c, err
	}
	if err := pause(ctx, time.Second); err != nil {
		return c, err
	}
	unlock, err := c.Exchange(ctx, 0x1a, []byte{1})
	if err != nil {
		return c, fmt.Errorf("adapter power unlock: %w", err)
	}
	if len(unlock.Payload) < 2 || unlock.Payload[1] != 0 {
		return c, fmt.Errorf("adapter rejected power unlock: %x", unlock.Payload)
	}
	return c, nil
}

func resetReply(f protocol.Frame) error {
	if len(f.Payload) < 1 || f.Payload[0] != 0 {
		return fmt.Errorf("adapter rejected reset: %x", f.Payload)
	}
	return nil
}

func runtimeLayout(d usb.Device) bool {
	for _, cfg := range d.Configurations {
		if cfg.Number == 2 && cfg.Layout == "bidirectional-bulk" {
			return true
		}
	}
	return false
}

func (c *Connection) Info(ctx context.Context) (Info, error) {
	link, err := c.Exchange(ctx, 4, nil)
	if err != nil {
		return Info{}, err
	}
	fw, err := c.Exchange(ctx, 5, nil)
	if err != nil {
		return Info{}, err
	}
	if len(link.Payload) < 1 || len(fw.Payload) < 3 {
		return Info{}, fmt.Errorf("short adapter link/firmware reply")
	}
	return Info{hex.EncodeToString(link.Payload), hex.EncodeToString(fw.Payload), fmt.Sprintf("%d.%d.%d (revision %d)", fw.Payload[0]>>4, fw.Payload[0]&15, fw.Payload[1], fw.Payload[2]), "Firmware version belongs to the SM-BCR2 adapter, not the bike components."}, nil
}

func (c *Connection) Close() error {
	if c == nil {
		return nil
	}
	err := c.closeHardware()
	if c.trace != nil {
		err = errors.Join(err, c.trace.err)
	}
	return err
}

// Reconnection closes the old handle but keeps the shared trace status until
// the final Close. A trace failure must not prevent reopening the adapter.
func (c *Connection) closeHardware() error {
	var err error
	if c.dev != nil && c.portStarted {
		err = c.control(7, 0, 3, nil)
		c.portStarted = false
	}
	// CLOSE can itself need the status endpoint to drain. Cancel only after
	// that control transfer, then join before releasing any USB handles.
	if c.interrupts != nil {
		err = errors.Join(err, c.interrupts.stop())
		c.interrupts = nil
	}
	if c.intf != nil {
		c.intf.Close()
		c.intf = nil
	}
	if c.config != nil {
		err = errors.Join(err, c.config.Close())
		c.config = nil
	}
	if c.dev != nil {
		err = errors.Join(err, c.dev.Close())
		c.dev = nil
	}
	if c.ctx != nil {
		err = errors.Join(err, c.ctx.Close())
		c.ctx = nil
	}
	return err
}
