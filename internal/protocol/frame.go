// Package protocol implements the legacy SM-BCR2 serial framing. Command
// meanings are supported only where independently established from evidence.
package protocol

import "fmt"

const delimiter byte = 0xbb
const escape byte = 0xbd

type Frame struct {
	Control byte   `json:"control"`
	Payload []byte `json:"-"`
}

func Encode(control byte, payload []byte) []byte {
	frame := []byte{delimiter}
	sum := control
	body := append([]byte{control}, payload...)
	for _, b := range payload {
		sum += b
	}
	body = append(body, -sum)
	for _, b := range body {
		if b == delimiter || b == escape {
			frame = append(frame, escape, b^0x20)
		} else {
			frame = append(frame, b)
		}
	}
	return append(frame, delimiter)
}

type Decoder struct {
	body    []byte
	escaped bool
}

// Feed accepts partial USB reads and frames without the optional initial flag.
// A bad frame resets decoder state and returns an error. Callers must not use a
// checksum-invalid response as evidence for a setting or command result.
func (d *Decoder) Feed(data []byte) ([]Frame, error) {
	frames := []Frame{}
	for _, b := range data {
		if b == delimiter {
			body, escaped := d.body, d.escaped
			d.body = nil
			d.escaped = false
			if escaped {
				return frames, fmt.Errorf("frame ended inside escape")
			}
			if len(body) == 0 {
				continue
			}
			if len(body) < 2 {
				return frames, fmt.Errorf("frame too short")
			}
			var sum byte
			for _, c := range body {
				sum += c
			}
			if sum != 0 {
				return frames, fmt.Errorf("invalid frame checksum")
			}
			frames = append(frames, Frame{Control: body[0], Payload: append([]byte{}, body[1:len(body)-1]...)})
			continue
		}
		if d.escaped {
			b ^= 0x20
			d.escaped = false
		} else if b == escape {
			d.escaped = true
			continue
		}
		d.body = append(d.body, b)
		if len(d.body) > 4096 {
			d.body = nil
			d.escaped = false
			return frames, fmt.Errorf("frame exceeds 4096 bytes")
		}
	}
	return frames, nil
}

func (d *Decoder) Finish() error {
	if len(d.body) != 0 || d.escaped {
		return fmt.Errorf("incomplete trailing frame")
	}
	return nil
}
