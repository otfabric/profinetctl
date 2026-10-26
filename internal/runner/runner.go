// SPDX-License-Identifier: MIT

// Package runner runs a command's work and renders its result or its fatal
// error in the output format of the invocation, and records the exit code.
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/otfabric/profinetctl/internal/format"
)

type invocationKey struct{}

// Invocation holds the output format and the exit code of one invocation.
type Invocation struct {
	OutputFormat    format.OutputFormat
	HasOutputFormat bool
	SuccessExitCode int
	SuccessExitSet  bool
}

// SetOutputFormat records the stdout format.
func (inv *Invocation) SetOutputFormat(f format.OutputFormat) {
	inv.OutputFormat = f
	inv.HasOutputFormat = true
}

// SetSuccessExit records the exit code of a command that did not fail.
func (inv *Invocation) SetSuccessExit(code int) {
	inv.SuccessExitCode = code
	inv.SuccessExitSet = true
}

// WithInvocation attaches inv to ctx.
func WithInvocation(ctx context.Context, inv *Invocation) context.Context {
	return context.WithValue(ctx, invocationKey{}, inv)
}

// InvocationFrom returns the invocation of ctx, or nil.
func InvocationFrom(ctx context.Context) *Invocation {
	v, _ := ctx.Value(invocationKey{}).(*Invocation)
	return v
}

// Output parses the output format and records it on the invocation of ctx.
func Output(ctx context.Context, raw string) (format.OutputFormat, error) {
	f, err := format.Parse(raw)
	if err != nil {
		return "", errs.FormatInvalid(err)
	}
	if inv := InvocationFrom(ctx); inv != nil {
		inv.SetOutputFormat(f)
	}
	return f, nil
}

// Write renders the result v to w and records the exit code (ExitOK, or
// what exit returns for v).
func Write(ctx context.Context, w io.Writer, f format.OutputFormat, v any, exit func(any) int) error {
	if err := format.Write(w, f, v); err != nil {
		if f == format.FormatTable {
			return errs.Output(errs.CodeTableRenderFailed, err)
		}
		return errs.Output(errs.CodeOutputWriteFailed, err)
	}
	code := errs.ExitOK
	if exit != nil {
		code = exit(v)
	}
	if inv := InvocationFrom(ctx); inv != nil {
		inv.SetSuccessExit(code)
	}
	return nil
}

// ErrorInfo is the structured error object of JSON output.
type ErrorInfo struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorEnvelope is the top-level JSON document of a fatal error.
type ErrorEnvelope struct {
	Error ErrorInfo `json:"error"`
}

// Info returns the structured form of err.
func Info(err error) ErrorInfo {
	if e, ok := errs.As(err); ok {
		msg := e.Message
		if msg == "" {
			msg = err.Error()
		}
		return ErrorInfo{Kind: string(e.Kind), Code: e.Code, Message: msg}
	}
	return ErrorInfo{Kind: string(errs.KindInternal), Code: errs.CodeInternalUnexpected, Message: err.Error()}
}

// RenderFatal renders a fatal error (JSON on stdout with --format json,
// text on stderr otherwise) and returns the exit code.
func RenderFatal(inv *Invocation, err error, stdout, stderr io.Writer, debug bool) int {
	code := errs.ExitInternal
	if e, ok := errs.As(err); ok {
		code = errs.ExitCodeForKind(e.Kind)
	}
	if inv != nil && inv.HasOutputFormat && inv.OutputFormat == format.FormatJSON {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		_ = enc.Encode(ErrorEnvelope{Error: Info(err)})
		if _, werr := stdout.Write(buf.Bytes()); werr != nil {
			_, _ = fmt.Fprintf(stderr, "error: write stdout: %v\n", werr)
			return errs.ExitInternal
		}
	} else {
		_, _ = fmt.Fprintf(stderr, "error: %s\n", Info(err).Message)
	}
	if debug {
		for c := errors.Unwrap(err); c != nil; c = errors.Unwrap(c) {
			_, _ = fmt.Fprintf(stderr, "cause: %v\n", c)
		}
	}
	return code
}

// FormatArg returns the value of the last --format (or -o) in args, up to
// a "--" that ends the flags: the format of an invocation Cobra rejected
// before the command could record it.
func FormatArg(args []string) (string, bool) {
	var value string
	var found bool
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return value, found
		case (a == "--format" || a == "-o") && i+1 < len(args):
			value, found = args[i+1], true
			i++
		case strings.HasPrefix(a, "--format="):
			value, found = strings.TrimPrefix(a, "--format="), true
		case strings.HasPrefix(a, "-o="):
			value, found = strings.TrimPrefix(a, "-o="), true
		}
	}
	return value, found
}
