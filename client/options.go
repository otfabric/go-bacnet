// SPDX-License-Identifier: MIT

package client

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// Option configures a Client.
type Option func(*config)

type config struct {
	iface                    string
	localAddr                string // host:port bind
	port                     int
	limits                   bacnet.DecodeLimits
	advertisedMaxAPDU        uint16 // 0 = use limits.MaxAPDUSize
	clock                    clock.Clock
	diag                     diag.Sink
	transport                Transport
	apduTimeout              time.Duration
	retryCount               int
	segmentTimeout           time.Duration
	segmentSendWindow        uint8
	segmentReceiveWindow     uint8
	maxTransactions          int
	fd                       *ForeignDeviceConfig
	hopCount                 uint8
	registry                 RegistryOptions
	deviceManagementEnabled  bool
	networkManagementEnabled bool
	eventHandler             EventNotificationHandler
	eventDispatcher          *EventDispatcherConfig
	logger                   *slog.Logger
	diagFn                   func(Diagnostic)
	// errs collects option values rejected while applying options; New
	// returns them joined instead of running with a silently altered setup.
	errs []error
}

func (c *config) invalid(format string, args ...any) {
	c.errs = append(c.errs, fmt.Errorf("%w: "+format, append([]any{bacnet.ErrInvalidConfig}, args...)...))
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
// window. Production defaults keep receive at 1 for peer compatibility while
// proposing 16 on send; use this option when both directions should share a
// larger window. A window outside 1..127 makes New fail.
func WithSegmentWindow(window uint8) Option {
	return func(c *config) {
		if window < 1 || window > 127 {
			c.invalid("segment window %d outside 1..127", window)
			return
		}
		c.segmentSendWindow = window
		c.segmentReceiveWindow = window
	}
}

// WithInterface restricts subnet-directed broadcasts to one network interface
// (UDP transport only).
//
// A broadcast is sent as one datagram to the directed broadcast address of
// each local subnet and one to the limited broadcast 255.255.255.255 on each
// interface (on Linux and macOS; elsewhere the system sends the limited
// broadcast once, on the interface of the default route). The
// subnets are those of the interface named here, or without this option of
// every interface that is up, broadcast-capable and not a loopback (only the
// subnet of the bound address when WithLocalAddr binds a specific one).
// Both forms are sent because devices differ in which one their socket
// receives; a device that receives both answers twice.
func WithInterface(name string) Option {
	return func(c *config) { c.iface = name }
}

// WithLocalAddr sets the UDP bind address (e.g. "0.0.0.0:47808"). Binding a
// specific unicast address stops most operating systems from delivering
// broadcast I-Am replies to the socket; prefer the wildcard address and
// WithInterface. Port 0 binds an ephemeral port, which works for unicast
// requests but not for peers that answer Who-Is by broadcast to 47808.
func WithLocalAddr(addr string) Option {
	return func(c *config) { c.localAddr = addr }
}

// WithPort sets the BACnet/IP UDP port the client binds and broadcasts to
// (default 47808). Zero selects the default; a port outside 0..65535 makes
// New fail. WithLocalAddr overrides the bind address but not the broadcast
// port.
func WithPort(port int) Option {
	return func(c *config) {
		if port < 0 || port > 65535 {
			c.invalid("port %d outside 0..65535", port)
			return
		}
		c.port = port
	}
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

// WithDiagnosticFunc registers a callback for client diagnostics: malformed
// or unexpected frames, responses that match no transaction, route and
// foreign-device changes, dropped queue entries. See OBSERVABILITY.md for the
// kinds.
//
// The callback is invoked synchronously on the receive and timeout paths,
// possibly from several goroutines. It must return promptly, must not panic
// and must not call back into the Client. Nil removes the callback. It may be
// combined with WithLogger.
func WithDiagnosticFunc(fn func(Diagnostic)) Option {
	return func(c *config) { c.diagFn = fn }
}

// WithLogger logs client diagnostics through l. The client is silent without
// it: nil (the default) disables logging, and slog.Default is never used.
//
// Each entry has the message of the diagnostic, a "kind" attribute and the
// diagnostic's fields. Frames the client drops (malformed, wrong source, no
// matching transaction) are logged at Debug; lost functionality (receive
// failures, foreign-device registration, dropped notifications, evictions) at
// Warn; route changes at Info. No payloads are logged.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) { c.logger = l }
}

// WithRegistryOptions configures device-observation retention bounds.
// Zero fields select package defaults (see RegistryOptions).
func WithRegistryOptions(opts RegistryOptions) Option {
	return func(c *config) { c.registry = opts }
}

// WithTransport runs the client on t instead of opening a UDP socket. The
// client takes ownership: Close closes t, and New closes it when construction
// fails.
func WithTransport(t Transport) Option {
	return func(c *config) { c.transport = t }
}

// WithTransactionOptions sets the APDU timeout (default 3s), the number of
// retransmissions after the first attempt (default 3) and the segment timeout
// (default 2s). A zero timeout keeps the default; retryCount 0 disables
// retransmission. A negative value makes New fail.
//
// Retransmission only applies to services that are safe to repeat (reads);
// see ConfirmedServicePolicy. Without a context deadline a read that gets no
// answer fails with bacnet.ErrTimeout after apduTimeout × (retryCount + 1).
func WithTransactionOptions(apduTimeout time.Duration, retryCount int, segmentTimeout time.Duration) Option {
	return func(c *config) {
		if apduTimeout < 0 || segmentTimeout < 0 || retryCount < 0 {
			c.invalid("transaction options apduTimeout=%v retryCount=%d segmentTimeout=%v must not be negative",
				apduTimeout, retryCount, segmentTimeout)
			return
		}
		if apduTimeout > 0 {
			c.apduTimeout = apduTimeout
		}
		c.retryCount = retryCount
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
// ReinitializeDevice (OperationDeviceManagement). confirm must equal
// DeviceManagementConfirm; any other value makes New fail.
//
// These services can disable communication or reboot a peer. Exact-APDU
// retransmission is disabled and post-send timeout/cancel returns
// *bacnet.OutcomeUnknownError.
func WithDeviceManagementEnabled(confirm string) Option {
	return func(c *config) {
		if confirm != DeviceManagementConfirm {
			c.invalid("WithDeviceManagementEnabled needs client.DeviceManagementConfirm")
			return
		}
		c.deviceManagementEnabled = true
	}
}

// NetworkManagementConfirm is required by WithNetworkManagementEnabled.
const NetworkManagementConfirm = "I_UNDERSTAND_NETWORK_MANAGEMENT"

// WithNetworkManagementEnabled opts into BVLC Write-BDT and similar
// OperationNetworkManagement actions. confirm must equal
// NetworkManagementConfirm; any other value makes New fail.
func WithNetworkManagementEnabled(confirm string) Option {
	return func(c *config) {
		if confirm != NetworkManagementConfirm {
			c.invalid("WithNetworkManagementEnabled needs client.NetworkManagementConfirm")
			return
		}
		c.networkManagementEnabled = true
	}
}

// WithEventNotificationHandler registers a handler for inbound
// Confirmed/Unconfirmed EventNotification PDUs. Confirmed notifications are
// SimpleACK'd before the handler runs. Nil disables the handler.
//
// The handler is invoked synchronously on the receive path and must return
// promptly without panicking. Prefer WithEventDispatcher for asynchronous work.
func WithEventNotificationHandler(h EventNotificationHandler) Option {
	return func(c *config) { c.eventHandler = h }
}

// WithEventDispatcher enables a bounded asynchronous EventNotification worker
// pool. Confirmed notifications are still SimpleACK'd on the receive path
// before enqueue. Handler must be non-nil. When set, the synchronous
// WithEventNotificationHandler path is unused for deliveries (dispatcher owns
// the handler). OpenEventStream remains independent.
func WithEventDispatcher(cfg EventDispatcherConfig) Option {
	return func(c *config) {
		cp := cfg
		c.eventDispatcher = &cp
	}
}
