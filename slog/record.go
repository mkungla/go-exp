// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package slog

import (
	"context"
	"runtime"
	"time"

	"golang.org/x/exp/slices"
)

const nAttrsInline = 5

// A Record holds information about a log event.
// Copies of a Record share state.
// Do not modify a Record after handing out a copy to it.
// Use [Record.Clone] to create a copy with no shared state.
type Record struct {
	// The time at which the output method (Log, Info, etc.) was called.
	Time time.Time

	// The log message.
	Message string

	// The level of the event.
	Level Level

	// The context of the Logger that created the Record. Present
	// solely to provide Handlers access to the context's values.
	// Canceling the context should not affect record processing.
	Context context.Context

	// The pc at the time the record was constructed, as determined
	// by runtime.Callers using the calldepth argument to NewRecord.
	pc uintptr

	// Allocation optimization: an inline array sized to hold
	// the majority of log calls (based on examination of open-source
	// code). It holds the start of the list of Attrs.
	front [nAttrsInline]Attr

	// The number of Attrs in front.
	nFront int

	// The list of Attrs except for those in front.
	// Invariants:
	//   - len(back) > 0 iff nFront == len(front)
	//   - Unused array elements are zero. Used to detect mistakes.
	back []Attr
}

// NewRecord creates a Record from the given arguments.
// Use [Record.AddAttrs] to add attributes to the Record.
// If calldepth is greater than zero, [Record.SourceLine] will
// return the file and line number at that depth,
// where 1 means the caller of NewRecord.
//
// NewRecord is intended for logging APIs that want to support a [Handler] as
// a backend.
func NewRecord(t time.Time, level Level, msg string, calldepth int, ctx context.Context) Record {
	var p uintptr
	if calldepth > 0 {
		p = pc(calldepth + 2)
	}
	return Record{
		Time:    t,
		Message: msg,
		Level:   level,
		Context: ctx,
		pc:      p,
	}
}

// Context returns the context in the Record.
// If the Record was created from a Logger,
// this will be the Logger's context.

// SourceLine returns the file and line of the log event.
// If the Record was created without the necessary information,
// or if the location is unavailable, it returns ("", 0).
func (r Record) SourceLine() (file string, line int) {
	fs := runtime.CallersFrames([]uintptr{r.pc})
	// TODO: error-checking?
	f, _ := fs.Next()
	return f.File, f.Line
}

// Clone returns a copy of the record with no shared state.
// The original record and the clone can both be modified
// without interfering with each other.
func (r Record) Clone() Record {
	r.back = slices.Clip(r.back) // prevent append from mutating shared array
	return r
}

// NumAttrs returns the number of attributes in the Record.
func (r Record) NumAttrs() int {
	return r.nFront + len(r.back)
}

// Attrs calls f on each Attr in the Record.
func (r Record) Attrs(f func(Attr)) {
	for i := 0; i < r.nFront; i++ {
		f(r.front[i])
	}
	for _, a := range r.back {
		f(a)
	}
}

// AddAttrs appends the given attrs to the Record's list of Attrs.
func (r *Record) AddAttrs(attrs ...Attr) {
	n := copy(r.front[r.nFront:], attrs)
	r.nFront += n
	// Check if a copy was modified by slicing past the end
	// and seeing if the Attr there is non-zero.
	if cap(r.back) > len(r.back) {
		end := r.back[:len(r.back)+1][len(r.back)]
		if end != (Attr{}) {
			panic("copies of a slog.Record were both modified")
		}
	}
	r.back = append(r.back, attrs[n:]...)
}

func (r *Record) setAttrsFromArgs(args []any) {
	var a Attr
	for len(args) > 0 {
		a, args = argsToAttr(args)
		if r.nFront < len(r.front) {
			r.front[r.nFront] = a
			r.nFront++
		} else {
			if r.back == nil {
				r.back = make([]Attr, 0, countAttrs(args))
			}
			r.back = append(r.back, a)
		}
	}

}

// countAttrs returns the number of Attrs that would be created from args.
func countAttrs(args []any) int {
	n := 0
	for i := 0; i < len(args); i++ {
		n++
		if _, ok := args[i].(string); ok {
			i++
		}
	}
	return n
}

const badKey = "!BADKEY"

// argsToAttr turns a prefix of the nonempty args slice into an Attr
// and returns the unconsumed portion of the slice.
// If args[0] is an Attr, it returns it.
// If args[0] is a string, it treats the first two elements as
// a key-value pair.
// Otherwise, it treats args[0] as a value with a missing key.
func argsToAttr(args []any) (Attr, []any) {
	switch x := args[0].(type) {
	case string:
		if len(args) == 1 {
			return String(badKey, x), nil
		}
		return Any(x, args[1]), args[2:]

	case Attr:
		return x, args[1:]

	default:
		return Any(badKey, x), args[1:]
	}
}
