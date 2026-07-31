// SPDX-License-Identifier: MIT

// Package diag defines an optional non-blocking diagnostic sink.
package diag

import "fmt"

// Kind classifies diagnostic events.
type Kind string

const (
	KindMalformed         Kind = "malformed"
	KindUnknownInvokeID   Kind = "unknown_invoke_id"
	KindWrongSource       Kind = "wrong_source"
	KindUnexpectedAPDU    Kind = "unexpected_apdu"
	KindWrongService      Kind = "wrong_service"
	KindLateResponse      Kind = "late_response"
	KindDuplicateResponse Kind = "duplicate_response"
	KindDuplicateInstance Kind = "duplicate_device_instance"
	KindQueueDrop         Kind = "queue_drop"
	KindForeignDevice     Kind = "foreign_device"
	KindRouter            Kind = "router"
	KindCOV               Kind = "cov"
)

// Event is a diagnostic report.
type Event struct {
	Kind    Kind
	Message string
	Fields  map[string]any
}

// Sink receives diagnostics. Implementations must not block.
type Sink interface {
	Report(Event)
}

// Discard ignores all events.
type Discard struct{}

func (Discard) Report(Event) {}

// Func adapts a function as a Sink.
type Func func(Event)

func (f Func) Report(e Event) {
	if f != nil {
		f(e)
	}
}

// Format returns a compact string for logging adapters.
func Format(e Event) string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}
