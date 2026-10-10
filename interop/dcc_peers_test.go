//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// DeviceCommunicationControl against BACpypes3 0.0.110 and BACnet4J 6.1.1.
// The assertions record what each upstream implementation does, which is not
// what the standard says in every case:
//
//   - BACpypes3 acknowledges every DCC request and checks the password, but
//     never restricts communication: the service stores the decoded
//     enumeration and the stack compares that state with strings, which never
//     match. A "disabled" device keeps answering.
//   - BACnet4J accepts "disable" and withholds acknowledgements afterwards. It
//     still executes the requests it does not acknowledge, still sends Error
//     replies, and still answers Who-Is.
//
// The BACpypes3 tests need the adapter that includes the upstream service
// mixin (bacnet-interop 0.10.0); BACnet4J needs no adapter support except for
// a configured password. Every test starts its own peer container.

const (
	dccAdapterMin   = "0.10.0"
	dccPassword     = "interop-dcc"
	dccPasswordEnv  = "BACNET_DCC_PASSWORD=" + dccPassword
	dccAPDUTimeout  = 500 * time.Millisecond
	dccRetries      = 2
	dccSilentWindow = dccAPDUTimeout * (dccRetries + 1)
)

// dccSession is one started peer plus a client allowed to send device
// management requests, with a short transaction timeout so an unanswered
// read fails quickly.
type dccSession struct {
	t    *testing.T
	ctx  context.Context
	c    *client.Client
	dev  deviceFixture
	peer *peerHandle
}

// startDCCSession returns nil on the host side of an in-network re-execution.
func startDCCSession(t *testing.T, p interopPeer, env ...string) *dccSession {
	t.Helper()
	dev := loadDeviceFixture(t)
	peer := startPeer(t, p.image, p.name, env...)
	if peer.assertedByReexec {
		return nil
	}
	c := newClientOpts(t,
		client.WithDeviceManagementEnabled(client.DeviceManagementConfirm),
		client.WithTransactionOptions(dccAPDUTimeout, dccRetries, time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	s := &dccSession{t: t, ctx: ctx, c: c, dev: dev, peer: peer}
	// A peer can report ready just before it serves its first request.
	if _, err := readPropertyRetry(t, ctx, c, peer.target, deviceObject(dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}); err != nil {
		t.Fatalf("read before DeviceCommunicationControl: %v", err)
	}
	return s
}

// control sends DeviceCommunicationControl. minutes 0 omits time-duration and
// an empty password omits the password.
func (s *dccSession) control(state service.EnableDisable, minutes uint16, password string) error {
	s.t.Helper()
	req := service.DeviceCommunicationControlRequest{EnableDisable: state}
	if minutes != 0 {
		req.TimeDuration = &minutes
	}
	if password != "" {
		req.Password = &bacnet.CharacterString{Value: password}
	}
	return s.c.DeviceCommunicationControl(s.ctx, s.peer.target, req)
}

func (s *dccSession) mustControl(what string, state service.EnableDisable, minutes uint16, password string) {
	s.t.Helper()
	if err := s.control(state, minutes, password); err != nil {
		s.t.Fatalf("DeviceCommunicationControl %s: %v, want SimpleACK", what, err)
	}
}

func (s *dccSession) readName() error {
	s.t.Helper()
	val, err := s.c.ReadProperty(s.ctx, s.peer.target, deviceObject(s.dev),
		bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		return err
	}
	if got := characterString(s.t, val); got != s.dev.DeviceName {
		s.t.Fatalf("object-name = %q, want %q", got, s.dev.DeviceName)
	}
	return nil
}

func (s *dccSession) mustRead(when string) {
	s.t.Helper()
	if err := s.readName(); err != nil {
		s.t.Fatalf("ReadProperty %s: %v, want an answer", when, err)
	}
}

// mustStaySilent asserts a read gets no answer at all: ErrTimeout after
// timeout × (retries + 1), and not an outcome-unknown.
func (s *dccSession) mustStaySilent(when string) {
	s.t.Helper()
	start := time.Now()
	err := s.readName()
	elapsed := time.Since(start)
	if !errors.Is(err, bacnet.ErrTimeout) {
		s.t.Fatalf("ReadProperty %s: %v, want ErrTimeout", when, err)
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		s.t.Fatalf("ReadProperty %s: a read timeout must not be outcome-unknown: %v", when, err)
	}
	if elapsed < dccSilentWindow-100*time.Millisecond || elapsed > dccSilentWindow+2*time.Second {
		s.t.Fatalf("ReadProperty %s gave up after %v, want about %v (timeout × (retries+1))", when, elapsed, dccSilentWindow)
	}
}

func (s *dccSession) presentValue() float32 {
	s.t.Helper()
	av, _ := analogValueObject(s.dev)
	val, err := s.c.ReadProperty(s.ctx, s.peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	if err != nil {
		s.t.Fatalf("read present-value: %v", err)
	}
	f, err := bacnet.AsReal(val)
	if err != nil {
		s.t.Fatal(err)
	}
	return f
}

func (s *dccSession) writePresentValue(v float32) error {
	s.t.Helper()
	av, _ := analogValueObject(s.dev)
	prio := uint8(8)
	return s.c.WriteProperty(s.ctx, s.peer.target, av,
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(v), &prio)
}

// iAmReceived sends a directed Who-Is from the BACnet/IP port and reports
// whether the device's I-Am arrives within wait.
func (s *dccSession) iAmReceived(wait time.Duration) bool {
	s.t.Helper()
	c, err := client.New(
		client.WithLocalAddr(fmt.Sprintf("0.0.0.0:%d", bip.DefaultPort)),
		client.WithPort(bip.DefaultPort))
	if err != nil {
		s.t.Fatalf("client.New discovery: %v", err)
	}
	defer func() { _ = c.Close() }()
	low, high := s.dev.DeviceInstance, s.dev.DeviceInstance
	if err := c.SendWhoIs(s.ctx, s.peer.endpoint, false, client.DiscoveryOptions{LowLimit: &low, HighLimit: &high}); err != nil {
		s.t.Fatalf("SendWhoIs: %v", err)
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		for _, obs := range c.Devices() {
			if obs.Instance == s.dev.DeviceInstance {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (s *dccSession) mustAnswerWhoIs(when string) {
	s.t.Helper()
	if !s.iAmReceived(3 * time.Second) {
		s.t.Fatalf("no I-Am for a Who-Is %s", when)
	}
}

// requirePasswordFailure asserts a definite Error security/password-failure.
func requirePasswordFailure(t *testing.T, what string, err error) {
	t.Helper()
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) {
		t.Fatalf("%s: got %T %v, want *bacnet.ErrorResponse security/password-failure", what, err, err)
	}
	if er.Class != bacnet.ErrorClassSecurity || er.Code != bacnet.ErrorCodePasswordFailure {
		t.Fatalf("%s: Error %s/%s, want security/password-failure", what,
			bacnet.ErrorClassName(er.Class), bacnet.ErrorCodeName(er.Code))
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("%s: definite Error reported as outcome-unknown: %v", what, err)
	}
}

// ---------------------------------------------------------------------------
// BACpypes3
// ---------------------------------------------------------------------------

func startBACpypes3DCC(t *testing.T, env ...string) *dccSession {
	t.Helper()
	p := bacpypes3Peer()
	requireAdapterVersion(t, p.image, dccAdapterMin)
	return startDCCSession(t, p, env...)
}

// Without a configured password BACpypes3 does not look at the password
// parameter: a request that carries one is acknowledged too.
func TestBACpypes3DeviceCommunicationControlEnable(t *testing.T) {
	s := startBACpypes3DCC(t)
	if s == nil {
		return
	}
	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
	s.mustControl("enable with an unexpected password", service.EnableDisableEnable, 0, "not-configured")
	s.mustRead("after enable with a password")
}

func TestBACpypes3DeviceCommunicationControlDisableInitiation(t *testing.T) {
	s := startBACpypes3DCC(t)
	if s == nil {
		return
	}
	s.mustControl("disable-initiation for 1 minute", service.EnableDisableDisableInitiation, 1, "")
	s.mustRead("under disable-initiation")
	s.mustAnswerWhoIs("under disable-initiation")
	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
}

// BACpypes3 0.0.110 acknowledges "disable" and then keeps communicating: it
// answers reads, executes and acknowledges writes, and answers Who-Is. The
// upstream service stores the decoded enumeration in the stack's
// dccEnableDisable, the stack compares that with the strings "enable",
// "disable" and "disableInitiation", and an enumeration never equals a
// string. When upstream fixes that, this test fails and the muted behaviour
// must be asserted instead.
func TestBACpypes3DeviceCommunicationControlDisableNotEnforced(t *testing.T) {
	s := startBACpypes3DCC(t)
	if s == nil {
		return
	}
	s.mustControl("disable for 1 minute", service.EnableDisableDisable, 1, "")
	s.mustRead("after an acknowledged disable (BACpypes3 does not mute)")
	if err := s.writePresentValue(66.5); err != nil {
		t.Fatalf("WriteProperty after an acknowledged disable: %v, want SimpleACK", err)
	}
	if got := s.presentValue(); math.Abs(float64(got-66.5)) > 0.01 {
		t.Fatalf("present-value = %v, want 66.5", got)
	}
	s.mustAnswerWhoIs("after an acknowledged disable")
	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
}

// With a configured password a request without it, or with another one, is
// refused with Error security/password-failure whatever it asks for.
func TestBACpypes3DeviceCommunicationControlPassword(t *testing.T) {
	s := startBACpypes3DCC(t, dccPasswordEnv)
	if s == nil {
		return
	}
	requirePasswordFailure(t, "disable without a password", s.control(service.EnableDisableDisable, 1, ""))
	requirePasswordFailure(t, "disable with a wrong password", s.control(service.EnableDisableDisable, 1, "wrong"))
	requirePasswordFailure(t, "enable with a wrong password", s.control(service.EnableDisableEnable, 0, "wrong"))
	s.mustRead("after refused requests")
	s.mustControl("disable-initiation with the password", service.EnableDisableDisableInitiation, 1, dccPassword)
	s.mustRead("under disable-initiation")
	s.mustControl("enable with the password", service.EnableDisableEnable, 0, dccPassword)
	s.mustRead("after enable")
}

// ---------------------------------------------------------------------------
// BACnet4J
// ---------------------------------------------------------------------------

func TestBACnet4JDeviceCommunicationControlEnable(t *testing.T) {
	s := startDCCSession(t, bacnet4jPeer())
	if s == nil {
		return
	}
	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
}

func TestBACnet4JDeviceCommunicationControlDisableInitiation(t *testing.T) {
	s := startDCCSession(t, bacnet4jPeer())
	if s == nil {
		return
	}
	s.mustControl("disable-initiation for 1 minute", service.EnableDisableDisableInitiation, 1, "")
	s.mustRead("under disable-initiation")
	s.mustAnswerWhoIs("under disable-initiation")
	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
}

// BACnet4J 6.1.1 accepts "disable". From then on a read gets no answer, until
// "enable" (the 1 minute time-duration is not waited for). What stays:
//
//   - Who-Is is answered with I-Am;
//   - a request that fails is answered with its Error: only acknowledgements
//     are withheld;
//   - a write is executed without an acknowledgement, so the client reports
//     outcome-unknown and the value is there after "enable";
//   - ReinitializeDevice and DeviceCommunicationControl are acknowledged.
func TestBACnet4JDeviceCommunicationControlDisable(t *testing.T) {
	s := startDCCSession(t, bacnet4jPeer())
	if s == nil {
		return
	}
	before := s.presentValue()
	const written = float32(66.5)
	if math.Abs(float64(before-written)) < 0.01 {
		t.Fatalf("fixture present-value is already %v", written)
	}

	s.mustControl("disable for 1 minute", service.EnableDisableDisable, 1, "")
	s.mustStaySilent("under disable")
	s.mustAnswerWhoIs("under disable")

	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 4000000}
	_, err := s.c.ReadProperty(s.ctx, s.peer.target, missing, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) || er.Class != bacnet.ErrorClassObject || er.Code != bacnet.ErrorCodeUnknownObject {
		t.Fatalf("ReadProperty of an unknown object under disable: %v, want Error object/unknown-object", err)
	}

	err = s.writePresentValue(written)
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("WriteProperty under disable: %v, want OutcomeUnknownError wrapping ErrTimeout", err)
	}

	if err := s.c.ReinitializeDevice(s.ctx, s.peer.target, service.ReinitializeDeviceRequest{
		State: service.ReinitializedWarmstart,
	}); err != nil {
		t.Fatalf("ReinitializeDevice under disable: %v, want SimpleACK", err)
	}
	s.mustControl("disable again", service.EnableDisableDisable, 1, "")
	s.mustStaySilent("after a second disable")

	s.mustControl("enable", service.EnableDisableEnable, 0, "")
	s.mustRead("after enable")
	if got := s.presentValue(); math.Abs(float64(got-written)) > 0.01 {
		t.Fatalf("present-value after enable = %v, want %v: the write sent under disable was executed", got, written)
	}
}

// BACnet4J compares the request password with the LocalDevice password for
// equality. None is configured here, so a request that carries a password is
// refused, and the device stays enabled.
func TestBACnet4JDeviceCommunicationControlUnexpectedPassword(t *testing.T) {
	s := startDCCSession(t, bacnet4jPeer())
	if s == nil {
		return
	}
	requirePasswordFailure(t, "disable with a password the device does not have",
		s.control(service.EnableDisableDisable, 1, "not-configured"))
	requirePasswordFailure(t, "enable with a password the device does not have",
		s.control(service.EnableDisableEnable, 0, "not-configured"))
	s.mustRead("after refused requests")
}

// With a configured password. The refusal is an Error, which BACnet4J sends
// while disabled as well.
func TestBACnet4JDeviceCommunicationControlPassword(t *testing.T) {
	p := bacnet4jPeer()
	requireAdapterVersion(t, p.image, dccAdapterMin)
	s := startDCCSession(t, p, dccPasswordEnv)
	if s == nil {
		return
	}
	requirePasswordFailure(t, "disable without a password", s.control(service.EnableDisableDisable, 1, ""))
	requirePasswordFailure(t, "disable with a wrong password", s.control(service.EnableDisableDisable, 1, "wrong"))
	s.mustRead("after refused requests")

	s.mustControl("disable with the password", service.EnableDisableDisable, 1, dccPassword)
	s.mustStaySilent("under disable")
	requirePasswordFailure(t, "enable with a wrong password under disable", s.control(service.EnableDisableEnable, 0, "wrong"))
	requirePasswordFailure(t, "enable without a password under disable", s.control(service.EnableDisableEnable, 0, ""))
	s.mustStaySilent("after refused enables")
	s.mustControl("enable with the password", service.EnableDisableEnable, 0, dccPassword)
	s.mustRead("after enable")
}
