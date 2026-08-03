// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadFileStreamMultiChunk(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	var n atomic.Int64
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		if choice != apdu.ServiceAtomicReadFile {
			return nil, context.Canceled
		}
		i := n.Add(1)
		if i == 1 {
			return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
				EndOfFile: false, Access: service.FileAccessStream, StartPosition: 0, Data: []byte("ab"),
			})
		}
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: true, Access: service.FileAccessStream, StartPosition: 2, Data: []byte("c"),
		})
	})
	data, err := env.Client.ReadFileStream(ctx, env.Target, file, FileReadOptions{ChunkSize: 2, MaxTotal: 100})
	if err != nil || string(data) != "abc" {
		t.Fatalf("%v %q", err, data)
	}
}
