// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

// syncBuffer is a bytes.Buffer safe for the server's hook and the test.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestServeReadWrite(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	up := make(chan bip.Endpoint, 1)
	out := &syncBuffer{}
	done := make(chan error, 1)
	go func() { done <- serve(ctx, "127.0.0.1:0", 1234, "example", func(ep bip.Endpoint) { up <- ep }, out) }()
	var ep bip.Endpoint
	select {
	case ep = <-up:
	case err := <-done:
		t.Fatalf("serve: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not start")
	}

	c, err := client.New(client.WithLocalAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	target := client.Target{Endpoint: ep}
	av1 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	if err := c.WriteProperty(rctx, target, av1, pv, bacnet.RealValue(19), &prio); err != nil {
		t.Fatal(err)
	}
	v, err := c.ReadProperty(rctx, target, av1, pv)
	if err != nil || v.Real != 19 {
		t.Fatalf("read back: %v %+v", err, v)
	}
	if got := out.String(); !strings.Contains(got, "present-value priority=8") {
		t.Fatalf("write not reported: %q", got)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve after cancel: %v", err)
	}
}

func TestServeRejectsInstance(t *testing.T) {
	err := serve(context.Background(), "127.0.0.1:0", bacnet.MaxObjectInstance, "x", func(bip.Endpoint) {}, &syncBuffer{})
	if err == nil {
		t.Fatal("instance 4194303 accepted")
	}
}
