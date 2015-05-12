// Copyright 2015 Marc-Antoine Ruel. All rights reserved.
// Use of this source code is governed under the Apache License, Version 2.0
// that can be found in the LICENSE file.

// panicparse: analyzes stack dump of Go processes and simplifies it.
//
// It is mostly useful on servers will large number of identical goroutines,
// making the crash dump harder to read than strictly necesary.
//
// Colors:
//  - Magenta: first goroutine to be listed.
//  - Yellow: main package.
//  - Green: standard library.
//  - Red: other packages.
//
// Bright colors are used for exported symbols.
package internal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/maruel/panicparse/stack"
	"github.com/mgutz/ansi"
)

func CalcLengths(buckets stack.Buckets) (int, int) {
	srcLen := 0
	pkgLen := 0
	for _, bucket := range buckets {
		for _, line := range bucket.Signature.Stack {
			l := len(line.SourceLine())
			if l > srcLen {
				srcLen = l
			}
			l = len(line.Func.PkgName())
			if l > pkgLen {
				pkgLen = l
			}
		}
	}
	return srcLen, pkgLen
}

func PrettyStack(r *stack.Signature, srcLen, pkgLen int) string {
	out := []string{}
	for _, line := range r.Stack {
		c := ansi.Red
		if line.IsStdlib() {
			if line.Func.IsExported() {
				c = ansi.LightGreen
			} else {
				c = ansi.Green
			}
		} else if line.IsPkgMain() {
			c = ansi.LightYellow
		} else if line.Func.IsExported() {
			c = ansi.LightRed
		}
		s := fmt.Sprintf(
			"    %s%-*s%s %-*s %s%s%s(%s)",
			ansi.LightWhite, pkgLen, line.Func.PkgName(), ansi.Reset,
			srcLen, line.SourceLine(),
			c, line.Func.Name(), ansi.Reset, line.Args)
		out = append(out, s)
	}
	if r.StackElided {
		out = append(out, "    (...)")
	}
	return strings.Join(out, "\n")
}

func Process(in io.Reader, out io.Writer) error {
	goroutines, err := stack.ParseDump(in, out)
	if err != nil {
		return err
	}
	buckets := stack.SortBuckets(stack.Bucketize(goroutines, true))
	srcLen, pkgLen := CalcLengths(buckets)
	for _, bucket := range buckets {
		extra := ""
		created := bucket.CreatedBy.Func.PkgDotName()
		if created != "" {
			if srcName := bucket.CreatedBy.SourceLine(); srcName != "" {
				created += " @ " + srcName
			}
			extra += ansi.LightBlack + " [Created by " + created + "]"
		}
		c := ansi.White
		if bucket.First() && len(buckets) > 1 {
			c = ansi.LightMagenta
		}

		fmt.Fprintf(out, "%s%d: %s%s%s\n", c, len(bucket.Routines), bucket.State, extra, ansi.Reset)
		fmt.Fprintf(out, "%s\n", PrettyStack(&bucket.Signature, srcLen, pkgLen))
	}
	return err
}

func Main() error {
	signals := make(chan os.Signal)
	go func() {
		for {
			<-signals
		}
	}()
	signal.Notify(signals, os.Interrupt, syscall.SIGQUIT)

	out := getOut()
	var in *os.File
	if len(os.Args) == 1 {
		in = os.Stdin
	} else if len(os.Args) == 2 {
		var err error
		name := os.Args[1]
		if in, err = os.Open(name); err != nil {
			return fmt.Errorf("did you mean to specify a valid stack dump file name? %s", err)
		}
		defer in.Close()
	} else {
		return errors.New("pipe from stdin or specify a single file")
	}
	return Process(in, out)
}