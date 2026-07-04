// Package stderrlog is the doctor's minimal-allocation logger. Info writes
// unconditionally; Debug writes only when verbose. Output destination is
// os.Stderr by default; tests inject an alternative writer via NewWith.
// No structured logging, no levels beyond {info, debug}, no third-party deps
// (D16).
package stderrlog

import (
	"fmt"
	"io"
	"os"
)

type Logger struct {
	w       io.Writer
	verbose bool
}

func New(verbose bool) *Logger {
	return NewWith(os.Stderr, verbose)
}

func NewWith(w io.Writer, verbose bool) *Logger {
	return &Logger{w: w, verbose: verbose}
}

func (l *Logger) Info(format string, args ...any) {
	if l == nil || l.w == nil {
		return
	}
	fmt.Fprintf(l.w, format, args...)
	if n := len(format); n == 0 || format[n-1] != '\n' {
		fmt.Fprintln(l.w)
	}
}

func (l *Logger) Debug(format string, args ...any) {
	if l == nil || !l.verbose || l.w == nil {
		return
	}
	fmt.Fprintf(l.w, format, args...)
	if n := len(format); n == 0 || format[n-1] != '\n' {
		fmt.Fprintln(l.w)
	}
}
