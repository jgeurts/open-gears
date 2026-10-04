package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/jgeurts/open-gears/internal/usb"
)

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
	if len(args) == 0 || args[0] == "devices" || args[0] == "inspect" {
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
