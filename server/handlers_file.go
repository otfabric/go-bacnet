// SPDX-License-Identifier: MIT

package server

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// fileFor finds the File object of an AtomicReadFile or AtomicWriteFile
// and checks the access method, with bacnet-stack's error choices
// (h_arf.c, h_awf.c): an object that is not a File is
// inconsistent-object-type, a method the file does not use is
// invalid-file-access-method.
func (s *Server) fileFor(id bacnet.ObjectIdentifier, access service.FileAccessMethod) (object.FileAccessor, error) {
	o, ok := s.dev.Object(id)
	if !ok {
		return nil, object.ErrUnknownObject
	}
	f, ok := o.(object.FileAccessor)
	if !ok || id.Type != bacnet.ObjectTypeFile {
		return nil, object.ErrInconsistentObjectType
	}
	if f.StreamAccess() != (access == service.FileAccessStream) {
		return nil, object.ErrInvalidFileAccessMethod
	}
	return f, nil
}

// atomicReadFile serves AtomicReadFile (clause 14.1). A reply larger than
// the requester accepts is segmented or aborted by the responder.
func (s *Server) atomicReadFile(_ context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeAtomicReadFile(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	f, err := s.fileFor(r.File, r.Access)
	if err != nil {
		return fail(err)
	}
	ack := service.AtomicReadFileACK{Access: r.Access, StartPosition: r.StartPosition}
	if r.Access == service.FileAccessStream {
		ack.Data, ack.EndOfFile, err = f.ReadStream(r.StartPosition, r.Count)
	} else {
		ack.Records, ack.EndOfFile, err = f.ReadRecords(r.StartPosition, r.Count)
		ack.RecordCount = uint32(len(ack.Records))
	}
	if err != nil {
		return fail(err)
	}
	payload, err := service.EncodeAtomicReadFileACK(ack)
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}

// atomicWriteFile serves AtomicWriteFile (clause 14.2); the ACK carries
// the position the data was written at (the end of the file for -1).
func (s *Server) atomicWriteFile(_ context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeAtomicWriteFile(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	f, err := s.fileFor(r.File, r.Access)
	if err != nil {
		return fail(err)
	}
	var at int32
	if r.Access == service.FileAccessStream {
		at, err = f.WriteStream(r.StartPosition, r.Data)
	} else {
		at, err = f.WriteRecords(r.StartPosition, r.Records)
	}
	if err != nil {
		return fail(err)
	}
	payload, err := service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{Access: r.Access, StartPosition: at})
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}
