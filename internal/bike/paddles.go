package bike

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jgeurts/open-gears/internal/adapter"
)

const MaxPlanBytes = 64 * 1024

type AdapterIdentity struct {
	Bus  int   `json:"bus"`
	Path []int `json:"path"`
}

type PaddleChange struct {
	Slot            byte   `json:"slot"`
	Series          byte   `json:"series"`
	Number          byte   `json:"number"`
	Part            byte   `json:"part"`
	FirmwareVersion string `json:"firmware_version"`
	BeforeRaw       string `json:"before_raw"`
	A               byte   `json:"a"`
	B               byte   `json:"b"`
}

type PaddlePlan struct {
	Version int             `json:"version"`
	Adapter AdapterIdentity `json:"adapter"`
	Changes []PaddleChange  `json:"changes"`
}

type PaddleChangeResult struct {
	Slot         byte   `json:"slot"`
	Status       string `json:"status"`
	BeforeRaw    string `json:"before_raw"`
	RequestedRaw string `json:"requested_raw"`
	ActualRaw    string `json:"actual_raw,omitempty"`
	Error        string `json:"error,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

type ApplyResult struct {
	Status   string               `json:"status"`
	Changes  []PaddleChangeResult `json:"changes"`
	Snapshot *Snapshot            `json:"snapshot,omitempty"`
	Error    string               `json:"error,omitempty"`
}

var firmwareText = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+ \(revision [0-9]+\)$`)

func annotatePaddleSupport(snapshot *Snapshot) {
	conventional := false
	conflictingController := false
	for _, u := range snapshot.Units {
		if u.Number == 0 {
			if u.Series == 5 || u.Series == 0x11 {
				conventional = true
			} else {
				conflictingController = true
			}
		}
	}
	for i := range snapshot.Units {
		u := &snapshot.Units[i]
		u.PaddleEditSupported = false
		switch {
		case u.Slot > 30:
			u.PaddleEditReason = "This slot is outside the verified service cleanup range."
		case !u.PartKnown:
			u.PaddleEditReason = "The component's shifter identity could not be read."
		case !conventional || conflictingController:
			u.PaddleEditReason = "Editing requires an identified SM-BTR2 or BT-DN110 battery controller."
		case !firmwareText.MatchString(u.FirmwareVersion):
			u.PaddleEditReason = "Read the shifter firmware version before editing."
		case u.Paddles == nil:
			u.PaddleEditReason = "Read the current paddle assignments before editing."
		default:
			if _, err := paddleLength(*u); err != nil {
				u.PaddleEditReason = "Paddle editing is not supported for this component."
			} else if raw, err := assignmentBytes(u.Paddles.Raw); err != nil || len(raw) < 2 {
				u.PaddleEditReason = "The current paddle assignment reply is incomplete."
			} else {
				u.PaddleEditSupported = true
				u.PaddleEditReason = ""
			}
		}
	}
}

func assignmentBytes(raw string) ([]byte, error) {
	if len(raw) < 4 || len(raw) > 128 {
		return nil, fmt.Errorf("assignment data must contain 2 to 64 bytes")
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid assignment data: %w", err)
	}
	return b, nil
}

func ValidatePlan(plan PaddlePlan) error {
	if plan.Version != 1 {
		return fmt.Errorf("unsupported paddle plan version %d", plan.Version)
	}
	if plan.Adapter.Bus < 0 || len(plan.Adapter.Path) == 0 || len(plan.Adapter.Path) > 7 {
		return fmt.Errorf("plan requires a physical adapter bus and USB port path")
	}
	for _, port := range plan.Adapter.Path {
		if port < 1 || port > 255 {
			return fmt.Errorf("invalid USB port in plan")
		}
	}
	if len(plan.Changes) == 0 || len(plan.Changes) > 30 {
		return fmt.Errorf("plan must contain 1 to 30 shifter changes")
	}
	seen := map[byte]bool{}
	for _, change := range plan.Changes {
		if change.Slot > 30 || seen[change.Slot] {
			return fmt.Errorf("invalid or duplicate shifter slot %d", change.Slot)
		}
		seen[change.Slot] = true
		before, err := assignmentBytes(change.BeforeRaw)
		if err != nil {
			return fmt.Errorf("slot %d: %w", change.Slot, err)
		}
		if change.A > 15 || change.B > 15 || (change.A != before[0]>>4 && change.A > 3) || (change.B != before[0]&15 && change.B > 3) {
			return fmt.Errorf("slot %d: changed X/Y assignments must be front-up, front-down, rear-up or rear-down", change.Slot)
		}
		if _, err := paddleLength(Unit{Series: change.Series, Number: change.Number, Part: change.Part}); err != nil {
			return err
		}
		if !firmwareText.MatchString(change.FirmwareVersion) {
			return fmt.Errorf("slot %d: plan requires the observed firmware version", change.Slot)
		}
	}
	return nil
}

// ReadPlan rejects unknown, missing and duplicate fields before USB is opened.
func ReadPlan(r io.Reader) (PaddlePlan, error) {
	var plan PaddlePlan
	raw, err := io.ReadAll(io.LimitReader(r, MaxPlanBytes+1))
	if err != nil {
		return plan, err
	}
	if len(raw) > MaxPlanBytes {
		return plan, fmt.Errorf("paddle plan exceeds %d bytes", MaxPlanBytes)
	}
	tokenDecoder := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueJSONValue(tokenDecoder, 0); err != nil {
		return plan, err
	}
	if _, err := tokenDecoder.Token(); err != io.EOF {
		return plan, fmt.Errorf("paddle plan must contain exactly one JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return plan, err
	}
	if err := requireFields(object, "version", "adapter", "changes"); err != nil {
		return plan, err
	}
	var adapterObject map[string]json.RawMessage
	if err := json.Unmarshal(object["adapter"], &adapterObject); err != nil {
		return plan, err
	}
	if err := requireFields(adapterObject, "bus", "path"); err != nil {
		return plan, err
	}
	var changes []map[string]json.RawMessage
	if err := json.Unmarshal(object["changes"], &changes); err != nil {
		return plan, err
	}
	for _, change := range changes {
		if err := requireFields(change, "slot", "series", "number", "part", "firmware_version", "before_raw", "a", "b"); err != nil {
			return plan, err
		}
	}
	return plan, ValidatePlan(plan)
}

func requireFields(object map[string]json.RawMessage, names ...string) error {
	if len(object) != len(names) {
		return fmt.Errorf("paddle plan contains missing or unexpected fields")
	}
	for _, name := range names {
		value, ok := object[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("paddle plan requires field %q", name)
		}
	}
	return nil
}

func uniqueJSONValue(decoder *json.Decoder, depth int) error {
	if depth > 8 {
		return fmt.Errorf("paddle plan nesting is too deep")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return err
			}
			key := token.(string)
			// encoding/json matches struct fields case-insensitively. Reject
			// aliases such as "a" and "A" rather than accepting the last one.
			folded := strings.ToLower(key)
			if seen[folded] {
				return fmt.Errorf("duplicate paddle plan field %q", key)
			}
			seen[folded] = true
			if err := uniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

// PlanPaddles builds a preview from observed values, without changing settings.
func PlanPaddles(snapshot Snapshot, slot byte, a, b *byte) (PaddlePlan, error) {
	annotatePaddleSupport(&snapshot)
	for _, u := range snapshot.Units {
		if u.Slot != slot {
			continue
		}
		if !u.PaddleEditSupported {
			return PaddlePlan{}, fmt.Errorf("%s: %s", u.Model, u.PaddleEditReason)
		}
		change := PaddleChange{Slot: u.Slot, Series: u.Series, Number: u.Number, Part: u.Part, FirmwareVersion: u.FirmwareVersion, BeforeRaw: u.Paddles.Raw, A: u.Paddles.A, B: u.Paddles.B}
		if a != nil {
			change.A = *a
		}
		if b != nil {
			change.B = *b
		}
		plan := PaddlePlan{Version: 1, Adapter: AdapterIdentity{Bus: snapshot.Adapter.Bus, Path: append([]int(nil), snapshot.Adapter.Path...)}, Changes: []PaddleChange{change}}
		return plan, ValidatePlan(plan)
	}
	return PaddlePlan{}, fmt.Errorf("shifter slot %d is not in the current bicycle", slot)
}

func newApplyResult(plan PaddlePlan) ApplyResult {
	result := ApplyResult{Status: "unchanged", Changes: []PaddleChangeResult{}}
	for _, change := range plan.Changes {
		requested := ""
		if before, err := assignmentBytes(change.BeforeRaw); err == nil {
			requested = hex.EncodeToString([]byte{change.A<<4 | change.B, before[1], 0, 0})
		}
		result.Changes = append(result.Changes, PaddleChangeResult{Slot: change.Slot, Status: "unchanged", BeforeRaw: change.BeforeRaw, RequestedRaw: requested})
	}
	return result
}

func finishApply(result *ApplyResult, err error) {
	verified, unknown := 0, false
	for _, change := range result.Changes {
		if change.Status == "verified" {
			verified++
		}
		unknown = unknown || change.Status == "unknown"
	}
	switch {
	case verified > 0 && (unknown || err != nil):
		result.Status = "partial"
	case unknown:
		result.Status = "unknown"
	case verified > 0:
		result.Status = "verified"
	default:
		result.Status = "unchanged"
	}
	if err != nil {
		result.Error = err.Error()
	}
}

// applyPaddles assumes Inspect has freshly established the service sessions.
// Validate every requested shifter before the first setting write.
func (s *Session) applyPaddles(ctx context.Context, plan PaddlePlan, snapshot Snapshot) (result ApplyResult, resultErr error) {
	result = newApplyResult(plan)
	defer func() { finishApply(&result, resultErr) }()
	if err := ValidatePlan(plan); err != nil {
		return result, err
	}
	annotatePaddleSupport(&snapshot)
	result.Snapshot = &snapshot
	indices := make([]int, len(plan.Changes))
	for i, change := range plan.Changes {
		index := slices.IndexFunc(snapshot.Units, func(u Unit) bool { return u.Slot == change.Slot })
		if index < 0 {
			return result, fmt.Errorf("slot %d disappeared; read the bicycle again", change.Slot)
		}
		u := snapshot.Units[index]
		if !u.PaddleEditSupported {
			return result, fmt.Errorf("slot %d: %s", change.Slot, u.PaddleEditReason)
		}
		if u.Series != change.Series || u.Number != change.Number || u.Part != change.Part || u.FirmwareVersion != change.FirmwareVersion {
			return result, fmt.Errorf("slot %d identity or firmware changed; create a new preview", change.Slot)
		}
		before, _ := assignmentBytes(change.BeforeRaw)
		actual, _ := assignmentBytes(u.Paddles.Raw)
		// OEM GET can return two or four bytes. Only the first two contain
		// assignments; SET always uses zeros for its two trailing bytes.
		if !bytes.Equal(before[:2], actual[:2]) {
			return result, fmt.Errorf("slot %d assignments changed; create a new preview", change.Slot)
		}
		indices[i] = index
		result.Changes[i].ActualRaw = u.Paddles.Raw
	}
	for i, change := range plan.Changes {
		item := &result.Changes[i]
		before, _ := assignmentBytes(change.BeforeRaw)
		requested := []byte{change.A<<4 | change.B, before[1], 0, 0}
		if bytes.Equal(before[:2], requested[:2]) {
			continue
		}
		if err := ctx.Err(); err != nil {
			item.Error = err.Error()
			return result, err
		}
		writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		if err := s.target(writeCtx, change.Slot); err != nil {
			cancel()
			item.Error = err.Error()
			return result, err
		}
		// A failed send or missing acknowledgment may still have changed settings.
		// Never retry SET; establish the current value with an independent GET.
		writeErr := s.connection.Send(writeCtx, 0x48, append([]byte{change.Slot, 4, 0x10}, requested...))
		if writeErr == nil {
			_, writeErr = s.receiveUnit(writeCtx, change.Slot, 4, 0x10, 0)
		}
		cancel()
		readCtx, readCancel := context.WithTimeout(context.Background(), 5*time.Second)
		actual, readErr := s.command(readCtx, change.Slot, 4, 0x14, make([]byte, 4), 2)
		readCancel()
		if readErr != nil {
			item.Status = "unknown"
			failure := errors.Join(writeErr, fmt.Errorf("readback failed: %w", readErr))
			item.Error = failure.Error()
			return result, failure
		}
		item.ActualRaw = hex.EncodeToString(actual)
		result.Snapshot.Units[indices[i]].Paddles, _ = decodePaddles(result.Snapshot.Units[indices[i]], actual)
		switch {
		case bytes.Equal(actual[:2], requested[:2]):
			item.Status = "verified"
			if writeErr != nil {
				item.Warning = fmt.Sprintf("Write acknowledgment was unavailable; requested assignments were verified by readback: %v", writeErr)
			}
		case bytes.Equal(actual[:2], before[:2]):
			item.Status = "unchanged"
			failure := errors.Join(writeErr, fmt.Errorf("slot %d settings are unchanged after the write attempt", change.Slot))
			item.Error = failure.Error()
			return result, failure
		default:
			item.Status = "unknown"
			failure := errors.Join(writeErr, fmt.Errorf("slot %d readback differs from the requested assignment", change.Slot))
			item.Error = failure.Error()
			return result, failure
		}
	}
	result.Snapshot.CapturedAt = time.Now().UTC().Format(time.RFC3339)
	return result, nil
}

// ApplyPaddles pins the physical adapter, re-inspects all preconditions, writes
// once per changed shifter, verifies readback, and always closes both sessions.
func ApplyPaddles(ctx context.Context, options adapter.Options, plan PaddlePlan) (result ApplyResult, resultErr error) {
	result = newApplyResult(plan)
	defer func() { finishApply(&result, resultErr) }()
	if err := ValidatePlan(plan); err != nil {
		return result, err
	}
	if options.Bus >= 0 && options.Bus != plan.Adapter.Bus {
		return result, fmt.Errorf("selected USB bus does not match the paddle plan")
	}
	options.Bus = plan.Adapter.Bus
	options.Path = append([]int(nil), plan.Adapter.Path...)
	c, err := adapter.Open(ctx, options)
	if err != nil {
		return result, err
	}
	defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
	matches := func() bool {
		location := c.Location()
		return location.Bus == plan.Adapter.Bus && slices.Equal(location.Path, plan.Adapter.Path)
	}
	if !matches() {
		return result, fmt.Errorf("opened adapter does not match the preview's physical USB port")
	}
	defer func() { resultErr = errors.Join(resultErr, c.EndBicycle(context.Background())) }()
	c, err = c.PrepareBicycle(ctx, options)
	if err != nil {
		return result, err
	}
	if !matches() {
		return result, fmt.Errorf("adapter changed physical USB port during preparation")
	}
	s := New(c)
	defer func() { resultErr = errors.Join(resultErr, s.Close()) }()
	snapshot, err := s.Inspect(ctx)
	snapshot.Adapter = c.Location()
	if err != nil {
		return result, err
	}
	return s.applyPaddles(ctx, plan, snapshot)
}
