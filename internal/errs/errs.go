// SPDX-License-Identifier: MIT

// Package errs holds the typed errors of profinetctl: a stable kind, which
// selects the exit code, and a stable code for scripts and JSON output.
package errs

import (
	"errors"
	"fmt"
)

// Kind is a coarse failure category for exit-code mapping and JSON error.kind.
type Kind string

const (
	KindUsage        Kind = "usage"
	KindInvalidInput Kind = "invalid_input"
	// KindTransport is a failure of the host: the interface cannot be opened, a socket
	// cannot be bound.
	KindTransport Kind = "transport"
	KindTimeout   Kind = "timeout"
	// KindProtocol is a peer that answered with something malformed.
	KindProtocol Kind = "protocol"
	// KindRefused is a request the device answered and refused: a PNIO error status,
	// a DCP block error, a rejected Connect.
	KindRefused Kind = "refused"
	// KindNotFound is a device that did not answer DCP: no device has the station
	// name or MAC address.
	KindNotFound Kind = "not_found"
	// KindUnsupported is an operation the device does not support.
	KindUnsupported Kind = "unsupported"
	// KindPermission is a host that denies raw Ethernet access.
	KindPermission  Kind = "permission"
	KindInterrupted Kind = "interrupted"
	KindOutput      Kind = "output"
	KindInternal    Kind = "internal"
)

// Process exit codes (single mapping from Kind for fatals).
const (
	ExitOK           = 0
	ExitUsage        = 2
	ExitInvalidInput = 3
	ExitTransport    = 4
	ExitTimeout      = 5
	ExitProtocol     = 6
	ExitPartial      = 7
	ExitUnsupported  = 8
	ExitPermission   = 9
	ExitInternal     = 10
	ExitNotFound     = 11
	ExitInterrupted  = 130
)

// ExitCodeForKind maps Kind to exit code for fatal errors.
func ExitCodeForKind(k Kind) int {
	switch k {
	case KindUsage:
		return ExitUsage
	case KindInvalidInput:
		return ExitInvalidInput
	case KindTransport:
		return ExitTransport
	case KindTimeout:
		return ExitTimeout
	case KindProtocol, KindRefused:
		return ExitProtocol
	case KindNotFound:
		return ExitNotFound
	case KindUnsupported:
		return ExitUnsupported
	case KindPermission:
		return ExitPermission
	case KindInterrupted:
		return ExitInterrupted
	case KindOutput, KindInternal:
		return ExitInternal
	default:
		return ExitInternal
	}
}

// Stable machine-oriented codes for JSON error.code and scripts.
const (
	CodeInvalidFormat          = "invalid_format"
	CodeInvalidInput           = "invalid_input"
	CodeInvalidFlagCombination = "invalid_flag_combination"
	CodeConfirmationRequired   = "confirmation_required"
	CodeInputFileOpenFailed    = "input_file_open_failed"
	CodeInvalidGSDML           = "invalid_gsdml"
	CodeInvalidModel           = "invalid_model"

	CodeInterfaceOpenFailed = "interface_open_failed"
	CodePermissionDenied    = "permission_denied"
	CodeSocketFailed        = "socket_failed"

	CodeRequestTimeout = "request_timeout"
	CodeDeviceNotFound = "device_not_found"
	// CodeDuplicateName is a station name that several devices answer to.
	CodeDuplicateName = "duplicate_station_name"

	CodeProtocolViolation = "protocol_violation"
	// CodePNIOError is a PNIO error status (ErrorCode, ErrorDecode, ErrorCode1,
	// ErrorCode2) in a response.
	CodePNIOError = "pnio_error"
	// CodeDCPBlockError is a DCP Set the device refused with a block error.
	CodeDCPBlockError = "dcp_block_error"
	CodeUnsupported   = "unsupported"
	// CodeConnectFailed is an AR that could not be established.
	CodeConnectFailed = "connect_failed"

	CodeContextCanceled = "context_canceled"

	CodeJSONEncodeFailed       = "json_encode_failed"
	CodeTableRenderFailed      = "table_render_failed"
	CodeOutputWriteFailed      = "output_write_failed"
	CodeOutputFileWriteFailed  = "output_file_write_failed"
	CodeOutputFileCreateFailed = "output_file_create_failed"

	CodeInternalUnexpected = "internal_unexpected"
)

// Error is a typed CLI failure with stable Kind and Code.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return string(e.Kind)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New constructs a typed error with a user-facing message.
func New(kind Kind, code, message string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Cause: cause}
}

// InvalidInput wraps validation or bad user input.
func InvalidInput(code, message string, cause error) *Error {
	return New(KindInvalidInput, code, message, cause)
}

// Output wraps encode/render/write failures.
func Output(code string, cause error) *Error {
	msg := "output error"
	if cause != nil {
		msg = cause.Error()
	}
	return New(KindOutput, code, msg, cause)
}

// Internal wraps unexpected failures.
func Internal(cause error) *Error {
	return New(KindInternal, CodeInternalUnexpected, "internal error", cause)
}

// As returns (*Error, true) if err unwraps to *Error.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// FormatInvalid wraps format.Parse failures.
func FormatInvalid(cause error) *Error {
	return InvalidInput(CodeInvalidFormat, fmt.Sprintf("invalid output format: %v", cause), cause)
}

// WrapValidation wraps validation failures as invalid input with a stable message
// prefix. A typed *Error is returned as it is.
func WrapValidation(cause error) *Error {
	if cause == nil {
		return nil
	}
	if e, ok := As(cause); ok {
		return e
	}
	return InvalidInput(CodeInvalidInput, fmt.Sprintf("invalid input: %v", cause), cause)
}
