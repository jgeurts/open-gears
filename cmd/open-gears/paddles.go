package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/jgeurts/open-gears/internal/adapter"
	"github.com/jgeurts/open-gears/internal/bike"
)

func paddleAction(name string) (*byte, error) {
	if name == "" {
		return nil, nil
	}
	values := map[string]byte{"front-up": 0, "front-down": 1, "rear-up": 2, "rear-down": 3}
	value, ok := values[name]
	if !ok {
		return nil, fmt.Errorf("unknown paddle action %q; use front-up, front-down, rear-up or rear-down", name)
	}
	return &value, nil
}

func runPaddles(args []string, w io.Writer) error {
	if len(args) == 0 || (args[0] != "plan" && args[0] != "apply") {
		return fmt.Errorf("use bike paddles plan or bike paddles apply")
	}
	command := args[0]
	fail := func(err error) error {
		if command == "apply" {
			return errors.Join(err, writeJSON(w, bike.ApplyResult{Status: "unchanged", Changes: []bike.PaddleChangeResult{}, Error: err.Error()}))
		}
		return err
	}
	f := flags("bike paddles " + command)
	bus := f.Int("bus", -1, "USB bus")
	address := f.Int("address", -1, "USB address")
	firmware := f.String("firmware", "", "OEM controller image")
	trace := f.String("trace", "", "new JSONL trace file")
	var planPath, aName, bName string
	slot := -1
	if command == "plan" {
		f.IntVar(&slot, "slot", -1, "shifter slot")
		f.StringVar(&aName, "a", "", "X paddle action")
		f.StringVar(&bName, "b", "", "Y paddle action")
	} else {
		f.StringVar(&planPath, "plan", "", "paddle plan JSON file")
	}
	if err := f.Parse(args[1:]); err != nil {
		return fail(err)
	}
	if f.NArg() != 0 || *bus < -1 || *address < -1 || *address > 127 {
		return fail(fmt.Errorf("invalid adapter selection or unexpected arguments"))
	}
	var plan bike.PaddlePlan
	var a, b *byte
	if command == "apply" {
		if planPath == "" {
			return fail(fmt.Errorf("apply requires --plan FILE"))
		}
		file, err := os.Open(planPath)
		if err != nil {
			return fail(err)
		}
		plan, err = bike.ReadPlan(file)
		err = errors.Join(err, file.Close())
		if err != nil {
			return fail(err)
		}
	} else {
		if slot < 0 || slot > 30 || (aName == "" && bName == "") {
			return fail(fmt.Errorf("plan requires --slot 0..30 and at least one of --a or --b"))
		}
		var err error
		a, err = paddleAction(aName)
		if err != nil {
			return fail(err)
		}
		b, err = paddleAction(bName)
		if err != nil {
			return fail(err)
		}
	}
	options := adapter.Options{Bus: *bus, Address: *address, Firmware: *firmware}
	var traceFile *os.File
	if *trace != "" {
		var err error
		traceFile, err = os.OpenFile(*trace, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fail(err)
		}
		options.Trace = traceFile
	}
	closeTrace := func() error {
		if traceFile == nil {
			return nil
		}
		return traceFile.Close()
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if command == "plan" {
		snapshot, err := bike.Inspect(ctx, options)
		err = errors.Join(err, closeTrace())
		if err != nil {
			return err
		}
		plan, err = bike.PlanPaddles(snapshot, byte(slot), a, b)
		if err != nil {
			return err
		}
		return writeJSON(w, plan)
	}
	result, err := bike.ApplyPaddles(ctx, options, plan)
	if closeErr := closeTrace(); closeErr != nil {
		err = errors.Join(err, closeErr)
		result.Error = err.Error()
		if result.Status == "verified" {
			result.Status = "partial"
		}
	}
	return errors.Join(err, writeJSON(w, result))
}
