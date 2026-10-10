//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// VT-Open, VT-Data and VT-Close against BACpypes3's own APDU classes. The
// adapter has no virtual terminal: its handlers report what
// bacpypes3.apdu.VTOpenRequest / VTDataRequest / VTCloseRequest decoded from
// the request and answer with VTOpenACK, VTDataACK and a SimpleACK built by
// the upstream encoders. This is decode evidence for the three requests and
// for the VT-Open ACK, not evidence that a VT session works.
func TestBACpypes3VTDecodeEvidence(t *testing.T) {
	image := getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image)
	requireAdapterVersion(t, image, "0.10.0")
	peer := startPeer(t, image, "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()

	mark := evidenceMark(peer)
	ack, err := c.VTOpen(ctx, peer.target, service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 3})
	if err != nil {
		t.Fatalf("VTOpen: %v", err)
	}
	// The adapter answers remote = local + 100: the ACK belongs to this request.
	if ack.RemoteVTSessionIdentifier != 103 {
		t.Fatalf("VTOpen-ACK remote-vt-session-identifier = %d, want 103", ack.RemoteVTSessionIdentifier)
	}
	ev := awaitOperationLine(t, peer, "vt-open", mark, 5*time.Second)
	if ev["decoder"] != "bacpypes3.apdu.VTOpenRequest" {
		t.Errorf("decoder = %v", ev["decoder"])
	}
	f := operationFields(t, ev)
	wantField(t, f, "vt_class", 1)
	wantField(t, f, "local_vt_session_identifier", 3)

	mark = evidenceMark(peer)
	dataACK, err := c.VTData(ctx, peer.target, service.VTDataRequest{
		VTSessionIdentifier: ack.RemoteVTSessionIdentifier, VTNewData: []byte("ls\r"), VTDataFlag: 1,
	})
	if err != nil {
		t.Fatalf("VTData: %v", err)
	}
	// The adapter answers with BACpypes3's VTDataACK: everything accepted,
	// and the octet count although the standard only wants it otherwise.
	if !dataACK.AllNewDataAccepted || dataACK.AcceptedOctetCount == nil || *dataACK.AcceptedOctetCount != 3 {
		t.Fatalf("VT-Data ACK %+v, want all-new-data-accepted and accepted-octet-count 3", dataACK)
	}
	f = operationFields(t, awaitOperationLine(t, peer, "vt-data", mark, 5*time.Second))
	wantField(t, f, "vt_session_identifier", 103)
	wantField(t, f, "vt_new_data_hex", "6c730d")
	wantField(t, f, "vt_data_flag", 1)

	mark = evidenceMark(peer)
	if err := c.VTClose(ctx, peer.target, service.VTCloseRequest{RemoteVTSessionIdentifiers: []uint8{103, 7}}); err != nil {
		t.Fatalf("VTClose: %v", err)
	}
	f = operationFields(t, awaitOperationLine(t, peer, "vt-close", mark, 5*time.Second))
	wantField(t, f, "remote_vt_session_identifiers", []int{103, 7})
}

// bacnet-stack has no VT codec and BACnet4J 6.1.1 leaves the VT services out
// of its services-supported set: both reject VT-Open as an unrecognized
// service. Pinned so that a peer that starts to serve VT is noticed.
func TestVTOpenUnrecognizedService(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer(), bacnet4jPeer()}, func(t *testing.T, _ deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		_, err := c.VTOpen(ctx, peer.target, service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 3})
		var rej *bacnet.RejectError
		if !errors.As(err, &rej) || rej.Reason != bacnet.RejectReasonUnrecognizedService {
			t.Fatalf("VTOpen: got %v, want Reject unrecognized-service", err)
		}
	})
}
