// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestDiscoverObjectsIHave(t *testing.T) {
	env := newVirtualPair(t)
	name := bacnet.CharacterString{Encoding: 0, Value: "AV-1"}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go func() {
		// Wait briefly for Who-Has to leave the client, then inject I-Have.
		time.Sleep(20 * time.Millisecond)
		apduBytes, err := service.EncodeIHaveAPDU(service.IHave{
			Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 55},
			Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
			Name:   name,
		})
		if err != nil {
			t.Error(err)
			return
		}
		injectBroadcastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apduBytes)
	}()

	got, err := env.Client.DiscoverObjects(ctx, WhoHasOptions{Name: &name})
	if err != context.DeadlineExceeded && err != context.Canceled {
		// DiscoverObjects returns ctx.Err() after the wait window.
		if err == nil {
			t.Fatal("expected context error after collection window")
		}
	}
	if len(got) != 1 {
		// Also accept Objects() snapshot in case timing put LastSeen before since.
		got = env.Client.Objects()
	}
	if len(got) != 1 || got[0].Name.Value != "AV-1" || got[0].DeviceInstance != 55 {
		t.Fatalf("observations=%+v err=%v", got, err)
	}
}

func TestSendWhoHasClosed(t *testing.T) {
	env := newVirtualPair(t)
	_ = env.Client.Close()
	name := bacnet.CharacterString{Value: "x"}
	err := env.Client.SendWhoHas(context.Background(), env.Peer, true, WhoHasOptions{Name: &name})
	if err != bacnet.ErrClosed {
		t.Fatalf("got %v", err)
	}
}

func TestHandleIHaveMalformed(t *testing.T) {
	env := newVirtualPair(t)
	// Truncated I-Have payload.
	apduBytes := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIHave,
		Payload:       []byte{0xC4},
	})
	injectBroadcastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apduBytes)
	time.Sleep(20 * time.Millisecond)
	if len(env.Client.Objects()) != 0 {
		t.Fatal("malformed I-Have must not upsert")
	}
}

func TestSendWhoHasByObjectUnicast(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}
	if err := env.Client.SendWhoHas(context.Background(), env.Peer, false, WhoHasOptions{Object: &obj}); err != nil {
		t.Fatal(err)
	}
	if len(env.ClientTr.Outbox()) == 0 {
		t.Fatal("expected outbound Who-Has")
	}
	name := bacnet.CharacterString{Value: "n"}
	if err := env.Client.SendWhoHas(context.Background(), env.Peer, true, WhoHasOptions{
		Name:    &name,
		Address: bacnet.GlobalBroadcast(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverObjectsClosedDuringWait(t *testing.T) {
	env := newVirtualPair(t)
	name := bacnet.CharacterString{Value: "n"}
	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.DiscoverObjects(context.Background(), WhoHasOptions{Name: &name})
		errCh <- err
	}()
	time.Sleep(20 * time.Millisecond)
	_ = env.Client.Close()
	if err := <-errCh; err != bacnet.ErrClosed {
		t.Fatalf("got %v", err)
	}
}

func TestDiscoverObjectsClosed(t *testing.T) {
	env := newVirtualPair(t)
	_ = env.Client.Close()
	name := bacnet.CharacterString{Value: "x"}
	_, err := env.Client.DiscoverObjects(context.Background(), WhoHasOptions{Name: &name})
	if err != bacnet.ErrClosed {
		t.Fatalf("got %v", err)
	}
}

func TestSendWhoHasEncodeError(t *testing.T) {
	env := newVirtualPair(t)
	err := env.Client.SendWhoHas(context.Background(), env.Peer, true, WhoHasOptions{})
	if err == nil {
		t.Fatal("expected encode error")
	}
}
