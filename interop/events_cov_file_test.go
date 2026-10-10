//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// forPeersWithFixture is forPeers with a specific device fixture version.
func forPeersWithFixture(t *testing.T, peers []interopPeer, fixturePath func(*testing.T) string, fixture string,
	fn func(t *testing.T, peer *peerHandle)) {
	t.Helper()
	for _, p := range peers {
		t.Run(p.name, func(t *testing.T) {
			t.Setenv("BACNET_DEVICE_FIXTURE", fixturePath(t))
			peer := startPeer(t, p.image, p.name,
				"DEVICE_FIXTURE_FILE=/fixtures/device/"+fixture+".json",
				"FIXTURE="+fixture,
			)
			if peer.assertedByReexec {
				return
			}
			fn(t, peer)
		})
	}
}

// eventTimeStamp converts entry i of a GetEventInformation eventTimeStamps
// list (SEQUENCE OF BACnetTimeStamp) to the form AcknowledgeAlarm sends back.
func eventTimeStamp(t *testing.T, list bacnet.ApplicationValue, i int) service.TimeStamp {
	t.Helper()
	if i >= len(list.Elements) {
		t.Fatalf("eventTimeStamps has %d entries, need index %d: %+v", len(list.Elements), i, list)
	}
	el := list.Elements[i]
	switch el.TagNumber {
	case 0:
		tm, err := bacnet.ContextTime(el)
		if err != nil {
			t.Fatalf("timestamp time: %v", err)
		}
		return service.TimeStamp{Choice: service.TimeStampTime, Time: tm}
	case 1:
		seq, err := bacnet.ContextUnsigned(el)
		if err != nil {
			t.Fatalf("timestamp sequence: %v", err)
		}
		return service.TimeStamp{Choice: service.TimeStampSequence, Sequence: uint32(seq)}
	case 2:
		in := el.Value.Elements
		if len(in) != 2 || in[0].Value.Kind != bacnet.ValueDate || in[1].Value.Kind != bacnet.ValueTime {
			t.Fatalf("timestamp datetime: %+v", el)
		}
		return service.TimeStamp{Choice: service.TimeStampDateTime,
			DateTime: service.DateTime{Date: in[0].Value.Date, Time: in[1].Value.Time}}
	}
	t.Fatalf("unexpected BACnetTimeStamp choice %d", el.TagNumber)
	return service.TimeStamp{}
}

// Drive AV-1 out of range, find the alarm with GetEventInformation and
// acknowledge the to-offnormal transition with the time stamp the device
// reported for it.
func TestGetEventInformationAndAcknowledgeAlarm(t *testing.T) {
	forPeersWithFixture(t, []interopPeer{bacnet4jPeer(), stackPeer()}, deviceBaselineV3Path, "device-baseline-v3",
		func(t *testing.T, peer *peerHandle) {
			c := newClient(t)
			ctx, cancel := withTimeout(t, 30*time.Second)
			defer cancel()
			av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
			pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
			prio := uint8(8)

			before, err := c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
			if err != nil {
				t.Fatalf("GetEventInformation before the alarm: %v", err)
			}
			for _, s := range before.Summaries {
				if s.Object == av {
					t.Fatalf("AV-1 already has an active event: %+v", s)
				}
			}

			if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(90), &prio); err != nil {
				t.Fatalf("WriteProperty AV-1=90: %v", err)
			}
			t.Cleanup(func() {
				rctx, rcancel := withTimeout(t, 5*time.Second)
				defer rcancel()
				_ = c.WriteProperty(rctx, peer.target, av, pv, bacnet.RealValue(20), &prio)
			})

			var summary service.EventSummary
			deadline := time.Now().Add(10 * time.Second)
			for found := false; !found; {
				ack, err := c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
				if err != nil {
					t.Fatalf("GetEventInformation: %v", err)
				}
				for _, s := range ack.Summaries {
					if s.Object == av {
						summary, found = s, true
					}
				}
				if !found {
					if time.Now().After(deadline) {
						t.Fatalf("AV-1 not reported by GetEventInformation; summaries=%+v", ack.Summaries)
					}
					time.Sleep(200 * time.Millisecond)
				}
			}
			// Out_Of_Range is reported as offnormal(2) or high-limit(3).
			if summary.EventState != 2 && summary.EventState != 3 {
				t.Fatalf("event state %d, want offnormal(2) or high-limit(3): %+v", summary.EventState, summary)
			}
			if n := len(summary.EventTimeStamps.Elements); n != 3 {
				t.Fatalf("eventTimeStamps has %d entries, want 3 (to-offnormal, to-fault, to-normal)", n)
			}

			err = c.AcknowledgeAlarm(ctx, peer.target, service.AcknowledgeAlarmRequest{
				ProcessIdentifier:    1,
				EventObject:          av,
				EventStateAcked:      summary.EventState,
				TimeStamp:            eventTimeStamp(t, summary.EventTimeStamps, 0),
				AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
				TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1},
			})
			if err != nil {
				t.Fatalf("AcknowledgeAlarm with the reported time stamp: %v", err)
			}

			// A stale time stamp is refused with a definite Error.
			err = c.AcknowledgeAlarm(ctx, peer.target, service.AcknowledgeAlarmRequest{
				ProcessIdentifier:    1,
				EventObject:          av,
				EventStateAcked:      summary.EventState,
				TimeStamp:            service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 4000000},
				AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
				TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
			})
			// The refusal is definite on both peers, in different PDUs:
			// BACnet4J answers Error services/invalid-time-stamp,
			// bacnet-stack 1.6.1 an Abort with reason other.
			var er *bacnet.ErrorResponse
			var abort *bacnet.AbortError
			switch {
			case errors.As(err, &er):
				t.Logf("%s refuses a stale acknowledgement with Error %s/%s", t.Name(),
					bacnet.ErrorClassName(er.Class), bacnet.ErrorCodeName(er.Code))
			case errors.As(err, &abort) && abort.Server:
				t.Logf("%s refuses a stale acknowledgement with Abort %s", t.Name(), bacnet.AbortReasonName(abort.Reason))
			default:
				t.Fatalf("AcknowledgeAlarm with a wrong time stamp: %v, want a remote Error or Abort", err)
			}
		})
}

// SubscribeCOVProperty: BACnet4J is the only pinned peer with a server path.
func TestBACnet4JSubscribeCOVProperty(t *testing.T) {
	forPeers(t, []interopPeer{bacnet4jPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 20*time.Second)
		defer cancel()
		av, baseline := analogValueObject(dev)
		pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
		increment := float32(1)
		sub, err := c.SubscribeCOVProperty(ctx, peer.target, av, pv, client.COVOptions{Lifetime: 60, BufferSize: 8}, &increment)
		if err != nil {
			t.Fatalf("SubscribeCOVProperty: %v", err)
		}
		defer func() { _ = sub.Close() }()

		prio := uint8(8)
		written := baseline + 5
		if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(written), &prio); err != nil {
			t.Fatalf("WriteProperty: %v", err)
		}
		t.Cleanup(func() {
			rctx, rcancel := withTimeout(t, 5*time.Second)
			defer rcancel()
			_ = c.WriteProperty(rctx, peer.target, av, pv, bacnet.RealValue(baseline), &prio)
		})
		deadline := time.After(8 * time.Second)
		for {
			var note *service.COVNotification
			select {
			case <-deadline:
				t.Fatalf("no notification carrying present-value=%v", written)
			case ev, ok := <-sub.Events():
				if !ok {
					t.Fatal("subscription closed")
				}
				note = ev.Notification
			}
			if note == nil {
				continue
			}
			if note.MonitoredObject != av {
				t.Fatalf("notification for %v, want %v", note.MonitoredObject, av)
			}
			for _, v := range note.Values {
				if v.Property.Identifier == bacnet.PropertyPresentValue && v.Value.Kind == bacnet.ValueReal && v.Value.Real == written {
					if err := sub.Close(); err != nil {
						t.Fatalf("cancel subscription: %v", err)
					}
					return
				}
			}
		}
	})
}

// A subscription outlives its lifetime: the client renews at half of it and
// notifications still arrive after the original lifetime has passed.
func TestCOVRenewKeepsSubscriptionAlive(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer(), bacpypes3Peer(), bacnet4jPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 40*time.Second)
		defer cancel()
		av, baseline := analogValueObject(dev)
		pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
		const lifetime = 4 * time.Second
		sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{Lifetime: uint32(lifetime / time.Second), BufferSize: 32})
		if err != nil {
			t.Fatalf("SubscribeCOV: %v", err)
		}
		defer func() { _ = sub.Close() }()
		start := time.Now()

		// Two renewals take the subscription past its original lifetime.
		renewals := 0
		deadline := time.After(3 * lifetime)
		for renewals < 2 {
			select {
			case <-deadline:
				t.Fatalf("saw %d renewals in %v", renewals, 3*lifetime)
			case ev, ok := <-sub.Events():
				if !ok {
					t.Fatal("subscription closed")
				}
				switch ev.State {
				case client.SubscriptionRenewing:
					renewals++
				case client.SubscriptionDegraded, client.SubscriptionExpired, client.SubscriptionClosed:
					t.Fatalf("subscription went %v: %v", ev.State, ev.Err)
				}
			}
		}
		if wait := lifetime + time.Second - time.Since(start); wait > 0 {
			time.Sleep(wait)
		}

		prio := uint8(8)
		written := baseline + 7
		if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(written), &prio); err != nil {
			t.Fatalf("WriteProperty: %v", err)
		}
		t.Cleanup(func() {
			rctx, rcancel := withTimeout(t, 5*time.Second)
			defer rcancel()
			_ = c.WriteProperty(rctx, peer.target, av, pv, bacnet.RealValue(baseline), &prio)
		})
		got := time.After(8 * time.Second)
		for {
			select {
			case <-got:
				t.Fatalf("no notification %v after subscribing with lifetime %v: renewal did not keep it alive",
					time.Since(start).Round(time.Second), lifetime)
			case ev, ok := <-sub.Events():
				if !ok {
					t.Fatal("subscription closed")
				}
				if ev.Notification == nil {
					continue
				}
				for _, v := range ev.Notification.Values {
					if v.Property.Identifier == bacnet.PropertyPresentValue && v.Value.Kind == bacnet.ValueReal && v.Value.Real == written {
						return
					}
				}
			}
		}
	})
}

// ReadFileStream and WriteFileStream move a file in chunks far smaller than
// the file, so every chunk boundary and the end-of-file flag are exercised.
func TestFileStreamHelpers(t *testing.T) {
	forPeersWithFixture(t, []interopPeer{bacnet4jPeer(), stackPeer()}, deviceBaselineV4Path, "device-baseline-v4",
		func(t *testing.T, peer *peerHandle) {
			c := newClient(t)
			ctx, cancel := withTimeout(t, 30*time.Second)
			defer cancel()
			file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}

			got, err := c.ReadFileStream(ctx, peer.target, file, client.FileReadOptions{ChunkSize: 7})
			if err != nil {
				t.Fatalf("ReadFileStream: %v", err)
			}
			if want := []byte("hello-world-stream-file-data"); !bytes.Equal(got, want) {
				t.Fatalf("ReadFileStream = %q, want %q", got, want)
			}

			payload := []byte("the quick brown fox jumps over the lazy dog 0123456789")
			outcomes, err := c.WriteFileStream(ctx, peer.target, file, 0, payload, client.FileWriteOptions{ChunkSize: 10})
			if err != nil {
				t.Fatalf("WriteFileStream: %v (outcomes %+v)", err, outcomes)
			}
			if len(outcomes) != 6 { // 54 octets in chunks of 10
				t.Fatalf("%d chunks written, want 6: %+v", len(outcomes), outcomes)
			}
			for i, o := range outcomes {
				if o.Err != nil || o.StartPosition != int32(i*10) {
					t.Fatalf("chunk %d: %+v", i, o)
				}
			}
			back, err := c.ReadFileStream(ctx, peer.target, file, client.FileReadOptions{ChunkSize: 16})
			if err != nil {
				t.Fatalf("ReadFileStream after write: %v", err)
			}
			if !bytes.HasPrefix(back, payload) {
				t.Fatalf("readback %q does not start with what was written %q", back, payload)
			}
		})
}
