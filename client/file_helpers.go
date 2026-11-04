// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"
	"math"

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
		count := chunkCount(opts.ChunkSize, opts.MaxTotal-len(out))
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
		pos, err = nextFilePosition(ack.StartPosition, int64(len(ack.Data)))
		if err != nil {
			return out, err
		}
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
		count := chunkCount(opts.ChunkSize, opts.MaxTotal-len(out))
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
		pos, err = nextFilePosition(ack.StartPosition, int64(ack.RecordCount))
		if err != nil {
			return out, err
		}
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
	if startPosition >= 0 && int64(startPosition)+int64(len(data)) > math.MaxInt32 {
		return nil, fmt.Errorf("%w: file write ends past octet %d", bacnet.ErrLimitExceeded, int64(math.MaxInt32))
	}
	var outcomes []FileChunkOutcome
	pos := startPosition
	for len(data) > 0 {
		n := min(int(opts.ChunkSize), len(data))
		chunk := data[:n]
		data = data[n:]
		outcome := FileChunkOutcome{StartPosition: pos, ByteCount: n}
		ack, err := c.AtomicWriteFile(ctx, target, service.AtomicWriteFileRequest{
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
		// A negative start (end of file, clause 15.2) leaves the position to
		// the device; its ACK reports where the chunk went, so the next chunk
		// follows that instead of offset n-1.
		base := pos
		if base < 0 {
			base = ack.StartPosition
		}
		if pos, err = nextFilePosition(base, int64(n)); err != nil {
			return outcomes, err
		}
	}
	return outcomes, nil
}

// chunkCount is the AtomicReadFile count for the next request: the chunk size,
// capped by what MaxTotal still allows.
func chunkCount(chunk uint32, remain int) uint32 {
	return min(uint32(max(min(int64(remain), math.MaxUint32), 0)), chunk)
}

// nextFilePosition returns start+n, or an error when the device reports a
// position or count that runs past the signed 32-bit file position range: the
// next request could not address it and would otherwise wrap negative.
func nextFilePosition(start int32, n int64) (int32, error) {
	next := int64(start) + n
	if next > math.MaxInt32 {
		return 0, fmt.Errorf("%w: file position %d+%d exceeds %d", bacnet.ErrProtocolViolation, start, n, int64(math.MaxInt32))
	}
	if next < 0 {
		return 0, fmt.Errorf("%w: file position %d+%d is negative", bacnet.ErrProtocolViolation, start, n)
	}
	return int32(next), nil
}
