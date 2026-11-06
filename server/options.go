// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// Option configures a Server.
type Option func(*config)

type config struct {
	transport      bip.Transport
	port           int
	localAddr      string
	iface          string
	limits         bacnet.DecodeLimits
	logger         *slog.Logger
	diagFn         func(Diagnostic)
	workers        int
	queue          int
	maxAPDU        uint16
	segmentation   bool
	maxSegments    int
	window         uint8
	apduTimeout    time.Duration
	segmentTimeout time.Duration
	retries        int
	password       string
	maxCOV         int
	maxLifetime    time.Duration
	hooks          Hooks
	bbmd           BBMD
	clock          clock.Clock
	errs           []string
}

func defaultConfig() config {
	return config{
		limits:         bacnet.DefaultDecodeLimits(),
		workers:        4,
		queue:          64,
		maxAPDU:        1476,
		segmentation:   true,
		maxSegments:    64,
		window:         4,
		apduTimeout:    3 * time.Second,
		segmentTimeout: 2 * time.Second,
		retries:        3,
		maxCOV:         256,
		maxLifetime:    0,
		clock:          clock.Real{},
	}
}

func (c *config) invalid(format string, a ...any) {
	c.errs = append(c.errs, fmt.Sprintf(format, a...))
}

// Hooks are the application's part in services that act on the device
// rather than on an object. Each runs on a worker with the request's
// deadline; a nil hook keeps the default given per field.
type Hooks struct {
	// BeforeWrite is called before a WriteProperty, each write of a
	// WritePropertyMultiple, or an AddListElement or RemoveListElement (the
	// Write then holds the elements as a constructed value) is applied. A
	// returned error is sent as the answer (see object.Err). Default: allow.
	BeforeWrite func(ctx context.Context, obj bacnet.ObjectIdentifier, w object.Write) error
	// BeforeDelete is called before DeleteObject removes a deletable
	// object. Default: allow.
	BeforeDelete func(ctx context.Context, obj bacnet.ObjectIdentifier) error
	// Reinitialize is called for ReinitializeDevice after the password was
	// checked; the SimpleACK is sent when it returns nil. Default: accept
	// and do nothing.
	Reinitialize func(ctx context.Context, state service.ReinitializedStateOfDevice) error
	// TimeSync is called for TimeSynchronization (utc false) and
	// UTCTimeSynchronization (utc true).
	//
	// The services below act only through their hook: without it they are
	// not listed in Protocol_Services_Supported, a confirmed request is
	// rejected with unrecognized-service and an unconfirmed one ignored.
	TimeSync func(t service.TimeSynchronization, utc bool)
	// PrivateTransfer handles Confirmed/UnconfirmedPrivateTransfer. For a
	// confirmed one the returned elements are the resultBlock.
	PrivateTransfer func(ctx context.Context, p service.PrivateTransfer, confirmed bool) ([]bacnet.Element, error)
	// TextMessage handles Confirmed/UnconfirmedTextMessage.
	TextMessage func(ctx context.Context, m service.TextMessage, confirmed bool) error
	// WriteGroup handles WriteGroup.
	WriteGroup func(ctx context.Context, w service.WriteGroup)
}

// Diagnostic is a non-fatal event of the server (a malformed frame, a
// dropped request, a failed send).
type Diagnostic struct {
	Kind    string
	Message string
	Fields  map[string]any
}

// WithTransport serves on t instead of opening a UDP socket. Close closes t.
func WithTransport(t bip.Transport) Option {
	return func(c *config) {
		if t == nil {
			c.invalid("nil transport")
		}
		c.transport = t
	}
}

// WithPort sets the UDP port (default 47808).
func WithPort(port int) Option {
	return func(c *config) {
		if port < 0 || port > 65535 {
			c.invalid("port %d outside 0..65535", port)
		}
		c.port = port
	}
}

// WithLocalAddr binds the UDP socket to host:port.
func WithLocalAddr(addr string) Option { return func(c *config) { c.localAddr = addr } }

// WithInterface limits broadcasts (I-Am, I-Have) to one interface.
func WithInterface(name string) Option { return func(c *config) { c.iface = name } }

// WithDecodeLimits sets the decode limits for inbound frames.
func WithDecodeLimits(l bacnet.DecodeLimits) Option {
	return func(c *config) { c.limits = l.Normalize() }
}

// WithLogger logs diagnostics. The server is silent without it.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }

// WithDiagnosticFunc receives every diagnostic. It runs on the receive
// loop or a worker and must not block.
func WithDiagnosticFunc(fn func(Diagnostic)) Option { return func(c *config) { c.diagFn = fn } }

// WithWorkers sets the number of request workers (default 4) and the
// number of requests that may wait for one (default 64). A confirmed
// request that finds the queue full is answered with Abort
// (out-of-resources); an unconfirmed one is dropped.
func WithWorkers(workers, queue int) Option {
	return func(c *config) {
		if workers < 1 || queue < 0 {
			c.invalid("workers %d / queue %d", workers, queue)
		}
		c.workers, c.queue = workers, queue
	}
}

// WithMaxAPDU sets Max_APDU_Length_Accepted (50..1476, default 1476).
func WithMaxAPDU(size uint16) Option {
	return func(c *config) {
		if size < 50 || size > 1476 {
			c.invalid("max APDU %d outside 50..1476", size)
		}
		c.maxAPDU = size
	}
}

// WithSegmentation sets segmentation support: maxSegments (2..255; 0
// disables segmentation) and the proposed window size (1..127).
func WithSegmentation(maxSegments int, window uint8) Option {
	return func(c *config) {
		if maxSegments == 0 {
			c.segmentation = false
			return
		}
		if maxSegments < 2 || maxSegments > 255 || window < 1 || window > 127 {
			c.invalid("segmentation %d segments, window %d", maxSegments, window)
		}
		c.segmentation, c.maxSegments, c.window = true, maxSegments, window
	}
}

// WithTransactionOptions sets APDU timeout, retries and segment timeout
// for the server's own confirmed requests (notifications) and segmented
// replies; they are also the values of the Device's APDU properties.
func WithTransactionOptions(apduTimeout time.Duration, retries int, segmentTimeout time.Duration) Option {
	return func(c *config) {
		if apduTimeout <= 0 || retries < 0 || segmentTimeout <= 0 {
			c.invalid("transaction options %v / %d / %v", apduTimeout, retries, segmentTimeout)
		}
		c.apduTimeout, c.retries, c.segmentTimeout = apduTimeout, retries, segmentTimeout
	}
}

// WithPassword sets the password that DeviceCommunicationControl and
// ReinitializeDevice must carry (at most 20 characters). Without it no
// password is checked.
func WithPassword(p string) Option {
	return func(c *config) {
		if len(p) > 20 {
			c.invalid("password longer than 20 octets")
		}
		c.password = p
	}
}

// WithCOVLimits bounds COV subscriptions (default 256) and their lifetime
// (0: no limit, which also admits indefinite subscriptions).
func WithCOVLimits(maxSubscriptions int, maxLifetime time.Duration) Option {
	return func(c *config) {
		if maxSubscriptions < 0 || maxLifetime < 0 {
			c.invalid("COV limits %d / %v", maxSubscriptions, maxLifetime)
		}
		c.maxCOV, c.maxLifetime = maxSubscriptions, maxLifetime
	}
}

// WithHooks installs application hooks.
func WithHooks(h Hooks) Option { return func(c *config) { c.hooks = h } }

// withClock replaces the clock (tests).
func withClock(clk clock.Clock) Option { return func(c *config) { c.clock = clk } }
