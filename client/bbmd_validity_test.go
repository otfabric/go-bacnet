// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bvlc"
)

// registered brings the rig to an accepted first registration at t = 0: the
// expiry is due at 60 s and the renewal at 30 s.
func (r *fdRig) registered(t *testing.T) {
	t.Helper()
	waitRegisterFD(t, r.tr, 1)
	r.created(t, fdTestAPDUTimeout, "Result timeout")
	r.result(t, 0)
	r.created(t, fdTestTTL, "expiry")
	r.created(t, fdTestTTL/2, "renewal")
	if !r.c.ForeignDeviceRegistered() {
		t.Fatal("not registered after the accepted registration")
	}
}

func (r *fdRig) broadcast() error {
	return r.c.SendWhoIs(context.Background(), r.c.broadcastEndpoint(), true, DiscoveryOptions{})
}

// waitDiag waits until a diagnostic with msg has been reported n times.
func (r *fdRig) waitDiag(t *testing.T, msg string, n int) []Diagnostic {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := r.diag.with(msg); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("no diagnostic %q number %d", msg, n)
		}
		time.Sleep(time.Millisecond)
	}
}

const (
	fdRenewalLostMsg = "foreign-device renewal failed; registration still valid"
	fdExpiredMsg     = "foreign-device registration expired; broadcasts unavailable"
)

// A renewal whose Result is lost keeps the registration until TTL after the
// accepted Register-Foreign-Device was sent; broadcasts keep working until
// then, and the expiry falls into a pending retry here.
func TestFDLostRenewalValidUntilExpiry(t *testing.T) {
	r := newFDRig(t, nil)
	r.registered(t)
	r.clk.Advance(30 * time.Second) // renewal sent at 30 s
	r.expectTimer(t, fdTestAPDUTimeout, "renewal Result timeout")
	r.created(t, 2*time.Second, "retry 1")
	got := r.waitDiag(t, fdRenewalLostMsg, 1)
	if got[0].Fields["valid_for"] != "25s" || got[0].Fields["failure"] != "timeout" {
		t.Fatalf("renewal diagnostic %v", got[0].Fields)
	}
	if !r.c.ForeignDeviceRegistered() {
		t.Fatal("a lost renewal Result ended the registration")
	}
	if err := r.broadcast(); err != nil {
		t.Fatalf("broadcast while the registration is valid: %v", err)
	}
	if n := count(functions(r.tr), bvlc.FunctionDistributeBroadcastToNetwork); n != 1 {
		t.Fatalf("%d Distribute-Broadcast-To-Network sent, want 1", n)
	}
	// Retries at 37 s, 46 s and 59 s lose their Results too.
	r.clk.Advance(2 * time.Second)
	r.expectTimer(t, fdTestAPDUTimeout, "retry 1 Result timeout")
	r.expectTimer(t, 4*time.Second, "retry 2")
	r.expectTimer(t, fdTestAPDUTimeout, "retry 2 Result timeout")
	r.expectTimer(t, 8*time.Second, "retry 3")
	r.created(t, fdTestAPDUTimeout, "retry 3 Result timeout")
	if !r.c.ForeignDeviceRegistered() {
		t.Fatal("unregistered before the registration expired")
	}
	r.clk.Advance(time.Second) // 60 s: TTL after the accepted request was sent
	r.waitDiag(t, fdExpiredMsg, 1)
	if r.c.ForeignDeviceRegistered() {
		t.Fatal("registered after the registration expired")
	}
	if err := r.broadcast(); !errors.Is(err, bacnet.ErrNotRegistered) {
		t.Fatalf("broadcast after the expiry: %v, want ErrNotRegistered", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := r.c.WaitForeignDeviceRegistered(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("WaitForeignDeviceRegistered after the expiry: %v", err)
	}
	// The pending retry is accepted: registered again, valid for one TTL.
	r.result(t, 0)
	r.created(t, fdTestTTL-time.Second, "expiry of retry 3, sent at 59 s")
	r.created(t, fdTestTTL/2, "renewal")
	if !r.c.ForeignDeviceRegistered() {
		t.Fatal("not registered after the retry was accepted")
	}
}

// The expiry also ends the registration while the client waits to retry.
func TestFDLostRenewalExpiresDuringRetryWait(t *testing.T) {
	r := newFDRig(t, func(time.Duration) time.Duration { return 30 * time.Second })
	r.registered(t)
	r.clk.Advance(30 * time.Second)
	r.expectTimer(t, fdTestAPDUTimeout, "renewal Result timeout")
	r.created(t, 30*time.Second, "retry")
	r.clk.Advance(25 * time.Second) // 60 s
	r.waitDiag(t, fdExpiredMsg, 1)
	if r.c.ForeignDeviceRegistered() {
		t.Fatal("registered after the registration expired")
	}
	r.clk.Advance(5 * time.Second) // 65 s: the retry
	waitRegisterFD(t, r.tr, 3)
}

// A renewal accepted after a lost Result keeps the client registered the
// whole time: a waiter is never blocked and the ready channel stays closed.
func TestFDLostRenewalRecoveredWithoutGap(t *testing.T) {
	r := newFDRig(t, nil)
	r.registered(t)
	ready := r.c.fd.readyCh()
	r.clk.Advance(30 * time.Second)
	r.expectTimer(t, fdTestAPDUTimeout, "renewal Result timeout")
	r.expectTimer(t, 2*time.Second, "retry")
	waitRegisterFD(t, r.tr, 3)
	r.created(t, fdTestAPDUTimeout, "retry Result timeout")
	r.result(t, 0)
	r.created(t, fdTestTTL, "expiry of the retry, sent at 37 s")
	r.created(t, fdTestTTL/2, "renewal")
	if r.c.fd.readyCh() != ready {
		t.Fatal("the registration lapsed between the lost renewal and the retry")
	}
	if err := r.c.WaitForeignDeviceRegistered(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.diag.with(fdExpiredMsg); len(got) != 0 {
		t.Fatalf("expired: %v", got)
	}
}

// A NAK on a renewal ends the registration at once.
func TestFDRenewalNAKUnregisters(t *testing.T) {
	r := newFDRig(t, nil)
	r.registered(t)
	r.clk.Advance(30 * time.Second)
	r.created(t, fdTestAPDUTimeout, "renewal Result timeout")
	r.result(t, bvlc.ResultRegisterForeignDeviceNAK)
	r.created(t, fdRetryNAK, "retry after NAK")
	if r.c.ForeignDeviceRegistered() {
		t.Fatal("registered after the BBMD refused the renewal")
	}
	if err := r.broadcast(); !errors.Is(err, bacnet.ErrNotRegistered) {
		t.Fatalf("broadcast after the NAK: %v, want ErrNotRegistered", err)
	}
	r.waitDiag(t, fdFailedMsg, 1)
}

// A Distribute-Broadcast-To-Network-NAK from the BBMD makes the client
// register again at once (no sooner than fdRetryMin after the last attempt),
// without ending the registration; one that arrives during an attempt is
// answered by that attempt.
func TestFDDistributeBroadcastNAKRegistersAgain(t *testing.T) {
	r := newFDRig(t, nil)
	r.registered(t)
	r.clk.Advance(3 * time.Second)
	r.result(t, bvlc.ResultDistributeBroadcastToNetworkNAK)
	r.created(t, 0, "attempt at once: 3 s since the last one")
	waitRegisterFD(t, r.tr, 2)
	if !r.c.ForeignDeviceRegistered() {
		t.Fatal("a Distribute-Broadcast-To-Network-NAK ended the registration")
	}
	r.waitDiag(t, "Distribute-Broadcast-To-Network-NAK from the BBMD; registering again", 1)
	r.created(t, fdTestAPDUTimeout, "Result timeout")
	r.result(t, bvlc.ResultDistributeBroadcastToNetworkNAK) // during the attempt
	r.waitDiag(t, "Distribute-Broadcast-To-Network-NAK from the BBMD; registering again", 2)
	r.result(t, 0)
	r.created(t, fdTestTTL, "expiry")
	r.created(t, fdTestTTL/2, "renewal, not a second attempt")
	// Right after an attempt, the next one waits fdRetryMin.
	r.result(t, bvlc.ResultDistributeBroadcastToNetworkNAK)
	r.created(t, fdRetryMin, "attempt after fdRetryMin")
	if n := r.registers(); n != 2 {
		t.Fatalf("%d Register-Foreign-Device sent before fdRetryMin, want 2", n)
	}
	r.clk.Advance(fdRetryMin)
	waitRegisterFD(t, r.tr, 3)
}
