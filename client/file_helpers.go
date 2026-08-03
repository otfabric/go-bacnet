// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// FileReadOptions bounds AtomicReadFile streaming helpers.
type FileReadOptions struct {
	ChunkSize uint32 // default 256 octets / records
	MaxTotal  int    // default 1 MiB / 4096 records; 0 uses default
}

// FileWriteOptions bounds AtomicWriteFile streaming helpers.
type FileWriteOptions struct {
	ChunkSize uint32 // default 256 octets
}

// FileChunkOutcome is one AtomicWriteFile chunk result.
type FileChunkOutcome struct {
	StartPosition int32
	ByteCount     int
	Err           error // nil, definitive error, or *bacnet.OutcomeUnknownError
}

// ReadFileStream reads a stream-access File object until EOF or MaxTotal.
func (c *Client) ReadFileStream(ctx context.Context, target Target, file bacnet.ObjectIdentifier, opts FileReadOptions) ([]byte, error) {
	if opts.ChunkSize == 0 {
		opts.ChunkSize = 256
	}
	if opts.MaxTotal <= 0 {
		opts.MaxTotal = 1 << 20
	}
	var out []byte
	pos := int32(0)
	for {
		if len(out) >= opts.MaxTotal {
			return out, fmt.Errorf("%w: file read MaxTotal %d exceeded", bacnet.ErrLimitExceeded, opts.MaxTotal)
		}
		remain := opts.MaxTotal - len(out)
		count := opts.ChunkSize
		if int(count) > remain {
			count = uint32(remain)
		}
		ack, err := c.AtomicReadFile(ctx, target, service.AtomicReadFileRequest{
			File:          file,
			Access:        service.FileAccessStream,
			StartPosition: pos,
			Count:         count,
		})
		if err != nil {
			return out, err
		}
		if ack.Access != service.FileAccessStream {
			return out, bacnet.ErrProtocolViolation
		}
		out = append(out, ack.Data...)
		pos = ack.StartPosition + int32(len(ack.Data))
		if ack.EndOfFile || len(ack.Data) == 0 {
			return out, nil
		}
	}
}

// ReadFileRecords reads record-access File data until EOF, MaxTotal records, or empty ACK.
func (c *Client) ReadFileRecords(ctx context.Context, target Target, file bacnet.ObjectIdentifier, startRecord int32, opts FileReadOptions) ([][]byte, error) {
	if opts.ChunkSize == 0 {
		opts.ChunkSize = 16
	}
	if opts.MaxTotal <= 0 {
		opts.MaxTotal = 4096
	}
	var out [][]byte
	pos := startRecord
	for len(out) < opts.MaxTotal {
		remain := opts.MaxTotal - len(out)
		count := opts.ChunkSize
		if int(count) > remain {
			count = uint32(remain)
		}
		ack, err := c.AtomicReadFile(ctx, target, service.AtomicReadFileRequest{
			File:          file,
			Access:        service.FileAccessRecord,
			StartPosition: pos,
			Count:         count,
		})
		if err != nil {
			return out, err
		}
		if ack.Access != service.FileAccessRecord {
			return out, bacnet.ErrProtocolViolation
		}
		out = append(out, ack.Records...)
		pos = ack.StartPosition + int32(ack.RecordCount)
		if ack.EndOfFile || ack.RecordCount == 0 || len(ack.Records) == 0 {
			return out, nil
		}
	}
	return out, fmt.Errorf("%w: file record MaxTotal %d exceeded", bacnet.ErrLimitExceeded, opts.MaxTotal)
}

// WriteFileStream writes data as stream-access AtomicWriteFile chunks.
//
// Each chunk is side-effecting: after a chunk is sent, timeout/cancel returns
// *bacnet.OutcomeUnknownError for that chunk. Prior successful chunks remain
// applied. startPosition is the octet offset of the first chunk.
func (c *Client) WriteFileStream(ctx context.Context, target Target, file bacnet.ObjectIdentifier, startPosition int32, data []byte, opts FileWriteOptions) ([]FileChunkOutcome, error) {
	if opts.ChunkSize == 0 {
		opts.ChunkSize = 256
	}
	var outcomes []FileChunkOutcome
	pos := startPosition
	for len(data) > 0 {
		n := int(opts.ChunkSize)
		if n > len(data) {
			n = len(data)
		}
		chunk := data[:n]
		data = data[n:]
		outcome := FileChunkOutcome{StartPosition: pos, ByteCount: n}
		_, err := c.AtomicWriteFile(ctx, target, service.AtomicWriteFileRequest{
			File:          file,
			Access:        service.FileAccessStream,
			StartPosition: pos,
			Data:          chunk,
		})
		outcome.Err = err
		outcomes = append(outcomes, outcome)
		if err != nil {
			return outcomes, err
		}
		pos += int32(n)
	}
	return outcomes, nil
}
