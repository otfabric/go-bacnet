// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// FileAccessMethod selects stream or record access for AtomicRead/WriteFile.
type FileAccessMethod uint8

const (
	FileAccessStream FileAccessMethod = 0
	FileAccessRecord FileAccessMethod = 1
)

// AtomicReadFileRequest is an AtomicReadFile confirmed request.
type AtomicReadFileRequest struct {
	File          bacnet.ObjectIdentifier
	Access        FileAccessMethod
	StartPosition int32  // stream: octet start; record: record start
	Count         uint32 // stream: octet count; record: record count
}

// AtomicReadFileACK is an AtomicReadFile ComplexACK.
type AtomicReadFileACK struct {
	EndOfFile     bool
	Access        FileAccessMethod
	StartPosition int32
	Data          []byte   // stream fileData
	Records       [][]byte // record fileRecordData
	RecordCount   uint32   // returnedRecordCount (record access)
}

// AtomicWriteFileRequest is an AtomicWriteFile confirmed request.
type AtomicWriteFileRequest struct {
	File          bacnet.ObjectIdentifier
	Access        FileAccessMethod
	StartPosition int32
	Data          []byte   // stream
	Records       [][]byte // record
}

// AtomicWriteFileACK is an AtomicWriteFile SimpleACK-equivalent ComplexACK with start position.
type AtomicWriteFileACK struct {
	Access        FileAccessMethod
	StartPosition int32
}

// EncodeAtomicReadFile encodes an AtomicReadFile request.
//
// Wire shape (ASHRAE 135 Clause 17 / BACnet4J-compatible):
//
//	application ObjectIdentifier(file)
//	[0] streamAccess  SEQUENCE { Signed start, Unsigned octetCount }
//	or
//	[1] recordAccess  SEQUENCE { Signed start, Unsigned recordCount }
func EncodeAtomicReadFile(req AtomicReadFileRequest) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(req.File))
	if err != nil {
		return nil, err
	}
	tag := uint8(0)
	if req.Access == FileAccessRecord {
		tag = 1
	}
	return bacnet.AppendContextTagged(dst, tag, []bacnet.Element{
		{Value: bacnet.SignedValue(int64(req.StartPosition))},
		{Value: bacnet.UnsignedValue(uint64(req.Count))},
	})
}

// DecodeAtomicReadFileACK decodes an AtomicReadFile ComplexACK.
func DecodeAtomicReadFileACK(payload []byte, limits bacnet.DecodeLimits) (AtomicReadFileACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AtomicReadFileACK{}, err
	}
	if n != len(payload) {
		return AtomicReadFileACK{}, fmt.Errorf("%w: AtomicReadFileACK trailing", bacnet.ErrTrailingData)
	}
	var ack AtomicReadFileACK
	var haveEOF bool
	for _, el := range els {
		switch {
		case !el.Context && el.Value.Kind == bacnet.ValueBoolean:
			ack.EndOfFile = el.Value.Boolean
			haveEOF = true
		case el.TagNumber == 0 && bacnet.IsContextConstructed(el):
			ack.Access = FileAccessStream
			if err := decodeFileStreamResult(el.Value.Elements, &ack); err != nil {
				return AtomicReadFileACK{}, err
			}
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			ack.Access = FileAccessRecord
			if err := decodeFileRecordResult(el.Value.Elements, &ack); err != nil {
				return AtomicReadFileACK{}, err
			}
		default:
			return AtomicReadFileACK{}, fmt.Errorf("%w: AtomicReadFileACK tag", bacnet.ErrMalformed)
		}
	}
	if !haveEOF {
		return AtomicReadFileACK{}, fmt.Errorf("%w: AtomicReadFileACK missing endOfFile", bacnet.ErrMalformed)
	}
	return ack, nil
}

func decodeFileStreamResult(els []bacnet.Element, ack *AtomicReadFileACK) error {
	if len(els) != 2 {
		return fmt.Errorf("%w: stream access fields", bacnet.ErrMalformed)
	}
	if els[0].Context || els[0].Value.Kind != bacnet.ValueSigned {
		return fmt.Errorf("%w: fileStartPosition", bacnet.ErrMalformed)
	}
	ack.StartPosition = int32(els[0].Value.Signed)
	if els[1].Context || els[1].Value.Kind != bacnet.ValueOctetString {
		return fmt.Errorf("%w: fileData", bacnet.ErrMalformed)
	}
	ack.Data = append([]byte(nil), els[1].Value.OctetString...)
	return nil
}

func decodeFileRecordResult(els []bacnet.Element, ack *AtomicReadFileACK) error {
	if len(els) < 2 {
		return fmt.Errorf("%w: record access fields", bacnet.ErrMalformed)
	}
	if els[0].Context || els[0].Value.Kind != bacnet.ValueSigned {
		return fmt.Errorf("%w: fileStartRecord", bacnet.ErrMalformed)
	}
	ack.StartPosition = int32(els[0].Value.Signed)
	u, err := bacnet.AsUnsigned(els[1].Value)
	if err != nil {
		return fmt.Errorf("%w: returnedRecordCount", bacnet.ErrMalformed)
	}
	ack.RecordCount = uint32(u)
	for _, el := range els[2:] {
		if el.Context || el.Value.Kind != bacnet.ValueOctetString {
			return fmt.Errorf("%w: fileRecordData", bacnet.ErrMalformed)
		}
		ack.Records = append(ack.Records, append([]byte(nil), el.Value.OctetString...))
	}
	return nil
}

// EncodeAtomicReadFileACK encodes an AtomicReadFile ACK (helpers/tests).
func EncodeAtomicReadFileACK(ack AtomicReadFileACK) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.BoolValue(ack.EndOfFile))
	if err != nil {
		return nil, err
	}
	tag := uint8(0)
	els := []bacnet.Element{{Value: bacnet.SignedValue(int64(ack.StartPosition))}}
	if ack.Access == FileAccessRecord {
		tag = 1
		els = append(els, bacnet.Element{Value: bacnet.UnsignedValue(uint64(ack.RecordCount))})
		for _, rec := range ack.Records {
			els = append(els, bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: rec}})
		}
	} else {
		els = append(els, bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: ack.Data}})
	}
	return bacnet.AppendContextTagged(dst, tag, els)
}

// EncodeAtomicWriteFile encodes an AtomicWriteFile request.
//
// Wire shape (ASHRAE 135 / BACnet4J-compatible):
//
//	application ObjectIdentifier(file)
//	[0] streamAccess  SEQUENCE { Signed start, OctetString data }
//	or
//	[1] recordAccess  SEQUENCE { Signed start, Unsigned recordCount, SEQUENCE OF OctetString }
func EncodeAtomicWriteFile(req AtomicWriteFileRequest) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(req.File))
	if err != nil {
		return nil, err
	}
	tag := uint8(0)
	els := []bacnet.Element{{Value: bacnet.SignedValue(int64(req.StartPosition))}}
	if req.Access == FileAccessRecord {
		tag = 1
		els = append(els, bacnet.Element{Value: bacnet.UnsignedValue(uint64(len(req.Records)))})
		for _, rec := range req.Records {
			els = append(els, bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: rec}})
		}
	} else {
		els = append(els, bacnet.Element{Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: req.Data}})
	}
	return bacnet.AppendContextTagged(dst, tag, els)
}

// DecodeAtomicWriteFileACK decodes an AtomicWriteFile ACK (stream/record start position).
//
// Wire shape (ASHRAE / BACnet4J / bacnet-stack):
//
//	streamAccess [0] Signed  — context primitive, not constructed
//	recordAccess [1] Signed
func DecodeAtomicWriteFileACK(payload []byte, limits bacnet.DecodeLimits) (AtomicWriteFileACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AtomicWriteFileACK{}, err
	}
	if n != len(payload) {
		return AtomicWriteFileACK{}, fmt.Errorf("%w: AtomicWriteFileACK trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 1 || !els[0].Context || bacnet.IsContextConstructed(els[0]) {
		return AtomicWriteFileACK{}, fmt.Errorf("%w: AtomicWriteFileACK CHOICE", bacnet.ErrMalformed)
	}
	ack := AtomicWriteFileACK{Access: FileAccessStream}
	switch els[0].TagNumber {
	case 0:
		ack.Access = FileAccessStream
	case 1:
		ack.Access = FileAccessRecord
	default:
		return AtomicWriteFileACK{}, fmt.Errorf("%w: AtomicWriteFileACK tag", bacnet.ErrMalformed)
	}
	pos, err := bacnet.ContextSigned(els[0])
	if err != nil {
		return AtomicWriteFileACK{}, fmt.Errorf("%w: start position", bacnet.ErrMalformed)
	}
	ack.StartPosition = int32(pos)
	return ack, nil
}

// EncodeAtomicWriteFileACK encodes an AtomicWriteFile ACK.
func EncodeAtomicWriteFileACK(ack AtomicWriteFileACK) ([]byte, error) {
	tag := uint8(0)
	if ack.Access == FileAccessRecord {
		tag = 1
	}
	return bacnet.AppendContextSigned(nil, tag, int64(ack.StartPosition))
}

// FileChunkBounds returns a safe [start, count] for the next stream read of size want
// against a file of known length, clamping to EOF.
func FileChunkBounds(fileLen, start int32, want uint32) (pos int32, count uint32, eof bool) {
	if start < 0 {
		start = 0
	}
	if int64(start) >= int64(fileLen) {
		return start, 0, true
	}
	remain := uint32(fileLen - start)
	if want == 0 || want > remain {
		want = remain
	}
	return start, want, int64(start)+int64(want) >= int64(fileLen)
}
