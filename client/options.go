// SPDX-License-Identifier: MIT

package client

import (
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// Option configures a Client.
type Option func(*config)

type config struct {
	iface                   string
	localAddr               string // host:port bind
	port                    int
	limits                  bacnet.DecodeLimits
	advertisedMaxAPDU       uint16 // 0 = use limits.MaxAPDUSize
	clock                   clock.Clock
	diag                    diag.Sink
	transport               Transport
	apduTimeout             time.Duration
	retryCount              int
	segmentTimeout          time.Duration
	segmentSendWindow       uint8
	segmentReceiveWindow    uint8
	maxTransactions         int
	fd                      *ForeignDeviceConfig
	hopCount                uint8
	registry                RegistryOptions
	deviceManagementEnabled bool
	eventHandler            EventNotificationHandler
}

// ForeignDeviceConfig registers with a single BBMD.
//
// TTL must be a whole number of seconds and at least 2s (wire encoding is
// uint16 seconds). Zero selects the default of 60s. Wire TTL and renew
// scheduling both use the normalized whole-second value.
//
// BVLC-Result correlation is by pending attempt and BBMD peer only: the wire
// has no generation field, so a delayed Result for an earlier attempt at the
// same BBMD cannot be distinguished from a Result for the current attempt.
type ForeignDeviceConfig struct {
	BBMD bip.Endpoint
	TTL  time.Duration
}

// Diagnostic is a public observability event.
type Diagnostic struct {
	Kind    string
	Message string
	Fields  map[string]any
}

func defaultConfig() config {
	return config{
		port:                 bip.DefaultPort,
		limits:               bacnet.DefaultDecodeLimits(),
		clock:                clock.Real{},
		diag:                 diag.Discard{},
		apduTimeout:          3 * time.Second,
		retryCount:           3,
		segmentTimeout:       2 * time.Second,
		segmentSendWindow:    defaultSegmentSendWindow,
		segmentReceiveWindow: defaultSegmentReceiveWindow,
		maxTransactions:      255,
		hopCount:             255,
	}
}

// WithSegmentWindow sets both the send proposed window and the receive actual
// window (1..127). Prefer this for tests. Production defaults keep receive at 1
// for peer compatibility while proposing 16 on send; use this option when both
// directions should share a larger window.
func WithSegmentWindow(window uint8) Option {
	return func(c *config) {
		if window >= 1 && window <= 127 {
			c.segmentSendWindow = window
			c.segmentReceiveWindow = window
		}
	}
}

// WithInterface selects a network interface name for broadcast (UDP mode).
func WithInterface(name string) Option {
	return func(c *config) { c.iface = name }
}

// WithLocalAddr sets the UDP bind address (e.g. "0.0.0.0:47808").
func WithLocalAddr(addr string) Option {
	return func(c *config) { c.localAddr = addr }
}

// WithPort sets the BACnet/IP UDP port (default 47808).
func WithPort(port int) Option {
	return func(c *config) { c.port = port }
}

// WithDecodeLimits sets parser/reassembly limits.
func WithDecodeLimits(l bacnet.DecodeLimits) Option {
	return func(c *config) { c.limits = l }
}

// WithAdvertisedMaxAPDU sets the Max APDU length accepted field in confirmed
// requests. When 0 (default), the client advertises DecodeLimits.MaxAPDUSize.
//
// Validated at New: 0 or ≥ 50, encodable as a BACnet MaxAPDU code, and not
// greater than DecodeLimits.MaxAPDUSize. Non-discrete values floor to the next
// lower defined size when encoded (50/128/206/480/1024/1476).
//
// Use a value smaller than DecodeLimits.MaxAPDUSize to coax peers into
// segmenting ComplexACK responses without tightening the local parser bound.
// Some peers size segment payloads to the advertised maximum without reserving
// APDU header space; keeping the decode limit higher avoids dropping those
// segments.
func WithAdvertisedMaxAPDU(size uint16) Option {
	return func(c *config) { c.advertisedMaxAPDU = size }
}

// withClock injects a clock for deterministic in-package tests.
func withClock(clk clock.Clock) Option {
	return func(c *config) { c.clock = clk }
}

// WithDiagnosticFunc registers a diagnostic callback.
//
// Callbacks are invoked synchronously on the receive and timeout paths.
// They must return promptly and must not panic. Nil disables diagnostics
// (silent by default).
func WithDiagnosticFunc(fn func(Diagnostic)) Option {
	return func(c *config) {
		if fn == nil {
			c.diag = diag.Discard{}
			return
		}
		c.diag = diag.Func(func(e diag.Event) {
			fn(Diagnostic{Kind: string(e.Kind), Message: e.Message, Fields: e.Fields})
		})
	}
}

// WithRegistryOptions configures device-observation retention bounds.
// Zero fields select package defaults (see RegistryOptions).
func WithRegistryOptions(opts RegistryOptions) Option {
	return func(c *config) { c.registry = opts }
}

// WithTransport injects a Transport (virtual or custom). Skips UDP dial.
func WithTransport(t Transport) Option {
	return func(c *config) { c.transport = t }
}

// WithTransactionOptions sets APDU timeout/retry/segment timeout.
func WithTransactionOptions(apduTimeout time.Duration, retryCount int, segmentTimeout time.Duration) Option {
	return func(c *config) {
		if apduTimeout > 0 {
			c.apduTimeout = apduTimeout
		}
		if retryCount >= 0 {
			c.retryCount = retryCount
		}
		if segmentTimeout > 0 {
			c.segmentTimeout = segmentTimeout
		}
	}
}

// WithForeignDevice enables foreign-device registration with one BBMD.
func WithForeignDevice(fd ForeignDeviceConfig) Option {
	return func(c *config) { c.fd = &fd }
}

// DeviceManagementConfirm is the exact confirmation string required by
// WithDeviceManagementEnabled.
const DeviceManagementConfirm = "I_UNDERSTAND_DEVICE_MANAGEMENT"

// WithDeviceManagementEnabled opts into DeviceCommunicationControl and
// ReinitializeDevice. confirm must equal DeviceManagementConfirm.
//
// These services can disable communication or reboot a peer. Exact-APDU
// retransmission is disabled and post-send timeout/cancel returns
// *bacnet.OutcomeUnknownError.
func WithDeviceManagementEnabled(confirm string) Option {
	return func(c *config) {
		c.deviceManagementEnabled = confirm == DeviceManagementConfirm
	}
}

// WithEventNotificationHandler registers a handler for inbound
// Confirmed/Unconfirmed EventNotification PDUs. Confirmed notifications are
// SimpleACK'd before the handler runs. Nil disables the handler.
//
// The handler is invoked synchronously on the receive path and must return
// promptly without panicking.
func WithEventNotificationHandler(h EventNotificationHandler) Option {
	return func(c *config) { c.eventHandler = h }
}
