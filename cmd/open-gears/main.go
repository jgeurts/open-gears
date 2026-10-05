package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jgeurts/open-gears/internal/adapter"
	"github.com/jgeurts/open-gears/internal/bike"
	"github.com/jgeurts/open-gears/internal/capture"
	"github.com/jgeurts/open-gears/internal/protocol"
	"github.com/jgeurts/open-gears/internal/usb"
)

var version = "dev"
var commit = "unknown"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "open-gears:", err)
		os.Exit(1)
	}
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func run(args []string, w io.Writer) error {
	if len(args) == 1 && (args[0] == "version" || args[0] == "--version") {
		return writeJSON(w, map[string]string{"version": version, "commit": commit})
	}
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		_, err := io.WriteString(w, help)
		return err
	}
	if len(args) > 0 && args[0] == "adapter" {
		return runAdapter(args[1:], w)
	}
	if len(args) > 0 && args[0] == "bike" {
		if len(args) > 1 && args[1] == "paddles" {
			return runPaddles(args[2:], w)
		}
		if len(args) < 2 || args[1] != "inspect" {
			return fmt.Errorf("use bike inspect or bike paddles plan/apply")
		}
		return runAdapter(append([]string{"bike-inspect"}, args[2:]...), w)
	}
	if len(args) > 0 && args[0] == "capture" {
		return runCapture(args[1:], w)
	}
	if len(args) > 0 && args[0] == "protocol" {
		return runProtocol(args[1:], w)
	}
	if len(args) == 0 || args[0] == "devices" || args[0] == "inspect" {
		if len(args) > 1 && !(len(args) == 2 && args[1] == "--json") {
			return fmt.Errorf("devices accepts only --json")
		}
		devices, err := usb.Discover()
		if err != nil {
			return err
		}
		if len(devices) == 0 {
			return fmt.Errorf("SM-BCR2 (1e44:7220) not found; use a USB data cable")
		}
		return writeJSON(w, devices)
	}
	return fmt.Errorf("unknown command %q", args[0])
}

const help = `open-gears — Shimano SM-BCR2 tools for macOS and Linux

  devices [--json]                  Read USB descriptors; no device session
  adapter initialize --firmware FILE Load OEM USB-controller firmware into RAM
  adapter info [--firmware FILE]     Read adapter link and firmware information
  bike inspect [--firmware FILE]     Read component identities and paddle settings
  bike paddles plan --slot N --a ACTION --b ACTION  Preview X/Y changes
  bike paddles apply --plan FILE     Apply a preview and verify assignments
  capture fields                    Show Wireshark export command
  capture import FILE [--bus N --address N]  Import TSV to JSONL on stdout
  capture analyze FILE              Summarize normalized JSONL events
  capture diff BEFORE AFTER         Compare normalized captures
  protocol decode HEX               Decode legacy serial frames offline
  version                           Show build version and commit

Actions: front-up, front-down, rear-up, rear-down. Omit --a or --b to preserve it.
Adapter options: --bus N --address N --trace NEW_FILE
Controller firmware: umpf3410.i51 from the SM-BCR2 Windows driver.
Only the documented SHA256 is accepted. It is not bicycle firmware.
Component firmware updates require further protocol and recovery validation.
`

func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

func runAdapter(args []string, w io.Writer) (result error) {
	if len(args) == 0 {
		return fmt.Errorf("use adapter info or adapter initialize")
	}
	command := args[0]
	if command != "info" && command != "initialize" && command != "bike-inspect" {
		return fmt.Errorf("unknown adapter command %q", command)
	}
	f := flags("adapter " + command)
	bus := f.Int("bus", -1, "USB bus")
	address := f.Int("address", -1, "USB address")
	firmware := f.String("firmware", "", "OEM controller image")
	trace := f.String("trace", "", "new JSONL trace file")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", f.Arg(0))
	}
	if *bus < -1 || *address < -1 || *address > 127 {
		return fmt.Errorf("invalid USB bus/address")
	}
	options := adapter.Options{Bus: *bus, Address: *address, Firmware: *firmware}
	if *trace != "" {
		file, err := os.OpenFile(*trace, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, file.Close()) }()
		options.Trace = file
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "bike-inspect" {
		snapshot, err := bike.Inspect(ctx, options)
		if err != nil {
			return err
		}
		return writeJSON(w, snapshot)
	}
	if command == "initialize" {
		d, err := adapter.Initialize(ctx, options)
		if err != nil {
			return err
		}
		return writeJSON(w, d)
	}
	c, err := adapter.Open(ctx, options)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, c.Close()) }()
	info, err := c.Info(ctx)
	if err != nil {
		return err
	}
	return writeJSON(w, info)
}

func readCapture(path string) ([]capture.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return capture.Read(f)
}

func runCapture(args []string, w io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("use capture fields, import, analyze or diff")
	}
	switch args[0] {
	case "fields":
		if len(args) != 1 {
			return fmt.Errorf("capture fields takes no arguments")
		}
		_, err := fmt.Fprintln(w, "tshark -r capture.pcapng -T fields -E header=y -E separator=/t -E quote=d -E occurrence=f "+strings.Join(func() []string {
			var fields []string
			for _, k := range capture.TSVFields {
				fields = append(fields, "-e "+k)
			}
			return fields
		}(), " ")+" > capture.tsv")
		return err
	case "import":
		if len(args) < 2 {
			return fmt.Errorf("capture import requires a TSV file")
		}
		f := flags("capture import")
		bus := f.Int("bus", -1, "USB bus (-1 means any)")
		addr := f.Int("address", -1, "USB address (-1 means any)")
		if err := f.Parse(args[2:]); err != nil {
			return err
		}
		if f.NArg() != 0 || *bus < -1 || *addr < -1 || *addr > 127 {
			return fmt.Errorf("invalid capture selection")
		}
		file, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer file.Close()
		events, err := capture.ImportTSV(file, capture.Filter{Bus: *bus, Address: *addr})
		if err != nil {
			return err
		}
		return capture.Write(w, events)
	case "analyze":
		if len(args) != 2 {
			return fmt.Errorf("capture analyze requires one JSONL file")
		}
		events, err := readCapture(args[1])
		if err != nil {
			return err
		}
		return writeJSON(w, capture.Analyze(events))
	case "diff":
		if len(args) != 3 {
			return fmt.Errorf("capture diff requires two JSONL files")
		}
		a, err := readCapture(args[1])
		if err != nil {
			return err
		}
		b, err := readCapture(args[2])
		if err != nil {
			return err
		}
		return writeJSON(w, capture.Diff(a, b))
	default:
		return fmt.Errorf("unknown capture command %q", args[0])
	}
}

func runProtocol(args []string, w io.Writer) error {
	if len(args) != 2 || args[0] != "decode" {
		return fmt.Errorf("use protocol decode HEX")
	}
	raw, err := hex.DecodeString(strings.NewReplacer(" ", "", ":", "").Replace(args[1]))
	if err != nil {
		return err
	}
	var decoder protocol.Decoder
	frames, err := decoder.Feed(raw)
	if err != nil {
		return err
	}
	if err := decoder.Finish(); err != nil {
		return err
	}
	output := []map[string]string{}
	for _, f := range frames {
		output = append(output, map[string]string{"control": fmt.Sprintf("0x%02x", f.Control), "payload": hex.EncodeToString(f.Payload)})
	}
	return writeJSON(w, output)
}
