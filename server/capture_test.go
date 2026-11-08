// SPDX-License-Identifier: MIT

package server

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/capture"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// outcome is the kind of an answer: "simple-ack", "complex-ack <service>",
// "error <class>/<code>", "reject <reason>", "abort <reason>" or "none".
func outcome(p *apdu.PDU) string {
	if p == nil {
		return "none"
	}
	switch p.Type {
	case apdu.TypeSimpleACK:
		return "simple-ack"
	case apdu.TypeComplexACK:
		return fmt.Sprintf("complex-ack %d", p.ComplexACK.ServiceChoice)
	case apdu.TypeError:
		c, k, err := apdu.DecodeErrorClassCode(p.Error.Payload, bacnet.DefaultDecodeLimits())
		if err != nil {
			return "error ?"
		}
		return fmt.Sprintf("error %s/%s", bacnet.ErrorClassName(c), bacnet.ErrorCodeName(k))
	case apdu.TypeReject:
		return "reject " + bacnet.RejectReasonName(p.Reject.Reason)
	case apdu.TypeAbort:
		return "abort " + bacnet.AbortReasonName(p.Abort.Reason)
	default:
		return fmt.Sprintf("type %#x", uint8(p.Type))
	}
}

// parityServer is a go-bacnet server standing in for one recorded device:
// its Device object has the recorded device's instance, and a password is
// configured as the recorded answers to ReinitializeDevice and
// DeviceCommunicationControl require.
type parityServer struct {
	s   *Server
	tr  *virtual.Transport
	out int
}

func newParityServer(t *testing.T, instance uint32, password string) *parityServer {
	t.Helper()
	dev, err := object.NewDevice(object.DeviceConfig{Instance: instance, Name: "capture-parity", VendorID: 999})
	if err != nil {
		t.Fatal(err)
	}
	ps := &parityServer{tr: virtual.New(serverEP, nil, 256)}
	opts := []Option{WithTransport(ps.tr), WithTransactionOptions(200*time.Millisecond, 0, 200*time.Millisecond)}
	if password != "" {
		opts = append(opts, WithPassword(password))
	}
	if ps.s, err = New(dev, opts...); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ps.s.Close() })
	return ps
}

// ask sends one recorded request APDU from clientEP and returns the answer
// (nil if none came within wait).
func (ps *parityServer) ask(t *testing.T, a []byte, wait time.Duration) *apdu.PDU {
	t.Helper()
	raw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, ExpectingReply: true, APDU: a})
	if err != nil {
		t.Fatal(err)
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	ps.tr.Inject(bip.InboundPacket{Data: frame, ImmediatePeer: clientEP})
	for deadline := time.Now().Add(wait); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		out := ps.tr.Outbox()
		if ps.out < len(out) {
			p := decodeSent(t, out[ps.out])
			ps.out++
			return &p.pdu
		}
	}
	return nil
}

// recordedTarget returns the Device instance a request addresses, if any.
func recordedTarget(v any) (uint32, bool) {
	var id bacnet.ObjectIdentifier
	switch r := v.(type) {
	case service.ReadPropertyRequest:
		id = r.Object
	case service.WritePropertyRequest:
		id = r.Object
	case []service.ReadAccessSpecification:
		if len(r) == 0 {
			return 0, false
		}
		for _, s := range r {
			if s.Object.Type != bacnet.ObjectTypeDevice || s.Object != r[0].Object {
				return 0, false
			}
		}
		id = r[0].Object
	default:
		return 0, false
	}
	return id.Instance, id.Type == bacnet.ObjectTypeDevice
}

func password(v any) *bacnet.CharacterString {
	switch r := v.(type) {
	case service.ReinitializeDeviceRequest:
		return r.Password
	case service.DeviceCommunicationControlRequest:
		return r.Password
	}
	return nil
}

// TestCaptureServerParity sends recorded requests to a go-bacnet server
// standing in for the recorded device and compares its answer with the
// device's, wherever the answer does not depend on the device's own objects:
// requests to its Device object (same instance), and requests to no object
// (ReinitializeDevice, DeviceCommunicationControl, GetEventInformation,
// GetAlarmSummary, PrivateTransfer). Every difference must be one the
// table below classifies; the rest must match the device.
func TestCaptureServerParity(t *testing.T) {
	gs, err := capture.LoadGolden()
	if err != nil {
		t.Fatal(err)
	}
	same, diff := map[string]int{}, map[string]int{}
	classes := map[string]string{}
	for _, g := range gs {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		parity(t, g.ID, msgs, same, diff, classes)
	}
	n := 0
	for _, v := range same {
		n += v
	}
	if n < 50 {
		t.Fatalf("only %d recorded answers reproduced", n)
	}
	if err := capture.Report("server", parityReport(same, diff, classes)); err != nil {
		t.Error(err)
	}
}

// TestCorpusServerParity runs the parity comparison over every committed
// capture (with BACNET_CAPTURE_EXTENDED=1 also the cached ones);
// report/server-corpus.json holds the counts.
func TestCorpusServerParity(t *testing.T) {
	man, err := capture.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	same, diff := map[string]int{}, map[string]int{}
	classes := map[string]string{}
	for _, mf := range man.Files {
		path := capture.CorpusPath(mf)
		if path == "" || mf.Stats.Transactions == 0 {
			continue
		}
		f, err := capture.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, msgs := capture.Analyze(f)
		parity(t, mf.File, msgs, same, diff, classes)
	}
	if os.Getenv("BACNET_CAPTURE_EXTENDED") != "1" {
		if err := capture.Report("server-corpus", parityReport(same, diff, classes)); err != nil {
			t.Error(err)
		}
	}
}

func parityReport(same, diff map[string]int, classes map[string]string) any {
	type difference struct {
		Count          int    `json:"count"`
		Classification string `json:"classification"`
	}
	ds := map[string]difference{}
	for k, v := range diff {
		ds[k] = difference{v, classes[k]}
	}
	return map[string]any{"same_as_device": same, "differs_from_device": ds}
}

// parity sends the comparable recorded requests of msgs to a stand-in
// server and counts the answers that equal the device's (same) and those
// that differ (diff, with their classification); an unclassified
// difference fails the test.
func parity(t *testing.T, label string, msgs []capture.Message, same, diff map[string]int, classes map[string]string) {
	t.Helper()
	txs, _ := capture.Transactions(msgs)
	ids := capture.Identities(msgs)
	for _, tx := range txs {
		req, resp := tx.Final()
		if !tx.Answered() || len(tx.Request) > 1 || len(tx.Response) > 1 || req.Err != nil {
			continue
		}
		instance, onDevice := recordedTarget(req.Service)
		objectless := false
		switch tx.Service {
		case apdu.ServiceReinitializeDevice, apdu.ServiceDeviceCommunicationControl, apdu.ServiceGetEventInformation,
			apdu.ServiceGetAlarmSummary, apdu.ServiceConfirmedPrivateTransfer:
			objectless = true
		}
		if !onDevice && !objectless {
			continue
		}
		// The stand-in has the recorded responder's instance when the
		// capture identifies it, so that a request naming another
		// Device instance finds none, as at the recorded device.
		own := uint32(100)
		if id := ids[capture.Endpoint(resp, true)]; id != nil && id.Instance != 0 && id.Instance < 4194303 {
			own = id.Instance
		} else if onDevice && instance < 4194303 {
			own = instance // an I-Am with the wildcard instance (the ALC LGE) or none
		}
		want := outcome(resp.APDU)
		pw := ""
		if p := password(req.Service); p != nil {
			pw = p.Value // the device accepted it
		}
		if strings.Contains(want, "password") {
			pw = "capture-parity" // one the request does not carry
		}
		ps := newParityServer(t, own, pw)
		got := outcome(ps.ask(t, req.NPDU.APDU, 300*time.Millisecond))
		_ = ps.s.Close()
		key := fmt.Sprintf("service %d: device %s, go-bacnet %s", tx.Service, want, got)
		if got == want {
			same[fmt.Sprintf("service %d: %s", tx.Service, got)]++
			continue
		}
		why, ok := explained(tx.Service, want, got)
		if !ok {
			t.Errorf("%s frame %d: %s", label, req.Frame, key)
		}
		diff[key]++
		classes[key] = why
	}
}

// explained classifies a difference between a recorded device's answer and
// go-bacnet's (see testdata/captures/README.md).
func explained(svc uint8, device, ours string) (string, bool) {
	switch {
	case device == "reject unrecognized-service":
		return "unsupported by the recorded device: go-bacnet's server implements the service", true
	case svc == apdu.ServiceConfirmedPrivateTransfer && ours == "reject unrecognized-service":
		return "vendor-specific: go-bacnet serves ConfirmedPrivateTransfer only with a hook", true
	case device == "error services/password-failure" && ours == "error security/password-failure":
		return "vendor difference: the device answers class services; go-bacnet class security", true
	case strings.HasPrefix(device, "complex-ack") && ours == "error property/unknown-property",
		device == "error property/unknown-property" && strings.HasPrefix(ours, "complex-ack"):
		return "object model: the recorded Device object and go-bacnet's implement different optional properties", true
	case device == "abort segmentation-not-supported" && strings.HasPrefix(ours, "complex-ack"):
		return "answer size: the recorded device's answer did not fit the requester's max-APDU; go-bacnet's Device object holds less", true
	case svc == apdu.ServiceReadProperty && ours == "error object/unknown-object":
		return "proxy: the device answers for a Device instance that is not its own (a router or gateway for its subordinates)", true
	case device == "error object/unknown-property":
		return "vendor difference: the WattStopper LI_NB answers unknown-property with class object; go-bacnet with class property", true
	case device == "reject buffer-overflow" || device == "error services/no-space-for-object":
		return "device resource limit: the device could not hold the answer", true
	case svc == apdu.ServiceWriteProperty && device == "simple-ack" && strings.HasPrefix(ours, "error"):
		return "object model: the property written is writable only at the recorded device", true
	case svc == apdu.ServiceReinitializeDevice && device == "simple-ack" && ours == "error services/service-request-denied":
		return "capability: the device (a Delta DAC-T305) supports backup and acknowledges START_BACKUP; go-bacnet has no backup procedure without a hook", true
	case svc == apdu.ServiceReadProperty && strings.HasPrefix(device, "complex-ack") && ours == "error property/invalid-array-index":
		return "object model: the recorded Device's arrays (object-list) hold more elements than the stand-in's", true
	case svc == apdu.ServiceReadPropertyMultiple && strings.HasPrefix(device, "error") && strings.HasPrefix(ours, "complex-ack"):
		return "vendor difference: the device answers a failed ReadPropertyMultiple access with an Error PDU; go-bacnet reports it inside the ComplexACK (clause 15.7.1.3)", true
	case strings.HasPrefix(device, "complex-ack") && ours == "abort segmentation-not-supported":
		return "answer size: go-bacnet's answer does not fit the requester's max-APDU without segmentation; the device's did", true
	case svc == apdu.ServiceReadProperty && device == "reject other" && strings.HasPrefix(ours, "complex-ack"):
		return "insufficient evidence: the device refuses the read with Reject other (a routed SPAK MSTP, max-APDU 50, does so for its object-list)", true
	case svc == apdu.ServiceReadPropertyMultiple && device == "error services/other" && ours == "complex-ack 14":
		return "vendor difference: asked for ALL of a Device instance not its own, the SYSC MLX answers Error services/other; go-bacnet a ComplexACK with object/unknown-object for the object", true
	case svc == apdu.ServiceWriteProperty && (ours == "error property/unknown-property" || device == "error property/unknown-property"):
		return "object model: the property written exists in only one of the two Device objects", true
	case svc == apdu.ServiceDeviceCommunicationControl && device == "simple-ack" && ours == "error services/service-request-denied":
		return "protocol revision: go-bacnet's Device (revision 20 or later) refuses the deprecated DISABLE; the recorded device predates it", true
	case svc == apdu.ServiceReinitializeDevice && strings.HasPrefix(device, "reject") && ours == "error services/service-request-denied":
		return "vendor difference: refusing START_BACKUP, the device rejects (undefined-enumeration from a device older than backup and restore, parameter-out-of-range from the PolarSoft server); go-bacnet answers services/service-request-denied, as the LMBC-300 does", true
	}
	return "", false
}

// TestCaptureBVLLResultParity sends the BVLL management requests a node
// that is not a BBMD answered in the recordings (Write-BDT, Read-BDT,
// Read-FDT, Register-Foreign-Device) to a server without a BBMD and
// requires the same BVLC-Result code.
func TestCaptureBVLLResultParity(t *testing.T) {
	gs, err := capture.LoadGolden()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range gs {
		if g.ID != "bvll-management-results" {
			continue
		}
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i+1 < len(msgs); i++ {
			req, res := msgs[i], msgs[i+1]
			if req.BVLC.Function == bvlc.FunctionResult || res.BVLC.Function != bvlc.FunctionResult || res.Src != req.Dst {
				continue
			}
			ps := newParityServer(t, 100, "")
			ps.tr.Inject(bip.InboundPacket{Data: req.Data, ImmediatePeer: clientEP})
			var got *bvlc.Message
			for deadline := time.Now().Add(time.Second); got == nil && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if out := ps.tr.Outbox(); len(out) > 0 {
					m, err := bvlc.Parse(out[0].Data, bacnet.DefaultDecodeLimits())
					if err != nil {
						t.Fatal(err)
					}
					got = &m
				}
			}
			if got == nil || got.Function != bvlc.FunctionResult || got.ResultCode != res.BVLC.ResultCode {
				t.Errorf("frame %d (function %#x): recorded result %#04x, go-bacnet %+v", req.Frame, uint8(req.BVLC.Function), res.BVLC.ResultCode, got)
			}
			n++
		}
	}
	if n != 4 {
		t.Fatalf("%d BVLL requests compared, want 4", n)
	}
}

// TestCaptureMutatedRequests sends malformed variants of every recorded
// request (capture.Mutations) to one server: every answer it sends must be
// a well-formed BVLL message, and afterwards it still answers a
// ReadProperty of its Device object.
func TestCaptureMutatedRequests(t *testing.T) {
	gs, err := capture.LoadGolden()
	if err != nil {
		t.Fatal(err)
	}
	ps := newParityServer(t, 100, "")
	n := 0
	for _, g := range gs {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		for i, m := range msgs {
			if m.APDU == nil || m.APDU.ConfirmedRequest == nil {
				continue
			}
			for _, v := range capture.Mutations(m.Data, uint64(i)) {
				ps.tr.Inject(bip.InboundPacket{Data: v, ImmediatePeer: clientEP})
				if n++; n%32 == 0 {
					time.Sleep(time.Millisecond) // the virtual inbox drops what it cannot hold
				}
			}
		}
	}
	out := ps.tr.Outbox()
	for _, p := range out {
		if _, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits()); err != nil {
			t.Fatalf("server sent a malformed datagram % x: %v", p.Data, err)
		}
	}
	rp, err := service.EncodeReadProperty(service.ReadPropertyRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
	})
	if err != nil {
		t.Fatal(err)
	}
	var got *apdu.PDU
	for attempt := uint8(0); got == nil || got.ComplexACK == nil; attempt++ {
		if attempt == 20 {
			t.Fatalf("server no longer answers after %d mutated requests: %+v", n, got)
		}
		ps.out = len(ps.tr.Outbox())
		got = ps.ask(t, apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{MaxAPDU: 5, InvokeID: 200 + attempt, ServiceChoice: apdu.ServiceReadProperty, Payload: rp}), 500*time.Millisecond)
	}
	for _, p := range ps.tr.Outbox()[len(out):] {
		if _, err := bvlc.Parse(p.Data, bacnet.DefaultDecodeLimits()); err != nil {
			t.Fatalf("server sent a malformed datagram % x: %v", p.Data, err)
		}
	}
	t.Logf("%d mutated requests, %d answers", n, len(ps.tr.Outbox()))
	if n < 5000 {
		t.Fatalf("only %d mutated requests", n)
	}
}
