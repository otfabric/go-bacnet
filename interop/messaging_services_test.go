//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func deviceBaselineV6Path(t *testing.T) string {
	t.Helper()
	for _, p := range []string{
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v6.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v6.json"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	t.Fatal("device-baseline-v6.json not found")
	return ""
}

type messagingPeer struct {
	name  string
	image string
	// Operations expected to produce peer {"event":"operation"} diagnostics.
	ops []string
	// Confirmed services exercised where the peer ACKs natively.
	confirmedPT   bool
	confirmedText bool
}

func messagingPeers() []messagingPeer {
	return []messagingPeer{
		{
			name:  "bacnet4j",
			image: bacnet4jImage(),
			// WriteGroupRequest.handle → NotImplementedException (unsupported-upstream).
			ops:           []string{"time-synchronization", "utc-time-synchronization", "unconfirmed-text-message", "confirmed-text-message", "unconfirmed-private-transfer", "confirmed-private-transfer"},
			confirmedPT:   true,
			confirmedText: true,
		},
		{
			name:          "bacpypes3",
			image:         getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image),
			ops:           []string{"time-synchronization", "utc-time-synchronization", "unconfirmed-text-message", "confirmed-text-message", "unconfirmed-private-transfer", "confirmed-private-transfer", "write-group"},
			confirmedPT:   true,
			confirmedText: true,
		},
		{
			name:  "bacnet-stack",
			image: getEnv("BACNET_STACK_IMAGE", defaultStackImage),
			// TextMessage + ConfirmedPrivateTransfer: unsupported-upstream (EVIDENCE.md).
			ops: []string{"time-synchronization", "utc-time-synchronization", "unconfirmed-private-transfer", "write-group"},
		},
	}
}

func runMessagingSemanticReceipt(t *testing.T, p messagingPeer) {
	t.Helper()
	t.Setenv("BACNET_DEVICE_FIXTURE", deviceBaselineV6Path(t))
	peer := startPeer(t, p.image, p.name,
		"DEVICE_FIXTURE_FILE=/fixtures/device/device-baseline-v6.json",
		"FIXTURE=device-baseline-v6",
	)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ts := service.TimeSynchronization{
		Date: bacnet.Date{Year: 126, Month: 8, Day: 2, Weekday: 7},
		Time: bacnet.Time{Hour: 12, Minute: 0, Second: 0, Hundredths: 0},
	}
	msgClass := uint32(0)
	text := service.TextMessage{
		TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MessageClass:            &msgClass,
		Message:                 "interop-hello",
	}
	pt := service.PrivateTransfer{VendorID: 1, ServiceNumber: 1}
	// WriteGroup changeList: one GroupChannelValue { channel[0]=1, value=Real(1.0) }.
	wgBody, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatalf("WriteGroup channel: %v", err)
	}
	wgBody, err = bacnet.AppendApplicationValue(wgBody, bacnet.RealValue(1.0))
	if err != nil {
		t.Fatalf("WriteGroup value: %v", err)
	}
	wgEls, n, err := bacnet.ParseSequence(wgBody, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(wgBody) {
		t.Fatalf("WriteGroup changeList parse: n=%d err=%v", n, err)
	}
	wg := service.WriteGroup{
		GroupNumber:   1,
		WritePriority: 8,
		ChangeList:    wgEls,
	}

	sendAndAwait := func(op string, send func() error) {
		t.Helper()
		peer.clearOperations()
		if err := send(); err != nil {
			t.Fatalf("%s send: %v", op, err)
		}
		awaitOperation(t, peer, op, 5*time.Second)
	}

	for _, op := range p.ops {
		switch op {
		case "time-synchronization":
			sendAndAwait(op, func() error {
				return c.TimeSynchronization(ctx, peer.target.Endpoint, false, ts)
			})
		case "utc-time-synchronization":
			sendAndAwait(op, func() error {
				return c.UTCTimeSynchronization(ctx, peer.target.Endpoint, false, ts)
			})
		case "unconfirmed-text-message":
			sendAndAwait(op, func() error {
				return c.UnconfirmedTextMessage(ctx, peer.target.Endpoint, false, text)
			})
		case "confirmed-text-message":
			if !p.confirmedText {
				continue
			}
			sendAndAwait(op, func() error {
				return c.ConfirmedTextMessage(ctx, peer.target, text)
			})
		case "unconfirmed-private-transfer":
			sendAndAwait(op, func() error {
				return c.UnconfirmedPrivateTransfer(ctx, peer.target.Endpoint, false, pt)
			})
		case "confirmed-private-transfer":
			if !p.confirmedPT {
				continue
			}
			sendAndAwait(op, func() error {
				return c.ConfirmedPrivateTransfer(ctx, peer.target, pt)
			})
		case "write-group":
			sendAndAwait(op, func() error {
				return c.WriteGroup(ctx, peer.target.Endpoint, false, wg)
			})
		default:
			t.Fatalf("unknown messaging op %q", op)
		}
	}
}

func TestMessagingSemanticReceipt(t *testing.T) {
	for _, p := range messagingPeers() {
		t.Run(p.name, func(t *testing.T) {
			runMessagingSemanticReceipt(t, p)
		})
	}
}

func TestWorldietyMessagingUnsupported(t *testing.T) {
	t.Skip("Worldiety messaging family unsupported-upstream; see bacnet-interop/EVIDENCE.md (B7d)")
}
