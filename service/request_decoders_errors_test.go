// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func join(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestServerRequestDecodersMalformedFields covers field-level errors of
// the request decoders the server uses. Each payload is well-formed ASN.1
// with one field of the wrong shape.
func TestServerRequestDecodersMalformedFields(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	file := []byte{0xC4, 0x02, 0x80, 0x00, 0x01} // File 1
	readStream := []byte{0x0E, 0x31, 0x00, 0x21, 0x01, 0x0F}
	sub := []byte{0x0C, 0x00, 0x80, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x1F} // AV 1, Present_Value
	auditLog := []byte{0x0C, 0x0F, 0x40, 0x00, 0x01}
	count := []byte{0x39, 0x0A}
	byTarget := func(fields ...byte) []byte {
		return join(auditLog, []byte{0x1E, 0x0E}, fields, []byte{0x0F, 0x1F}, count)
	}
	device := []byte{0x0C, 0x02, 0x00, 0x00, 0x01}
	cases := []struct {
		name    string
		payload []byte
		decode  func([]byte) error
	}{
		{"read: second access method", join(file, readStream, []byte{0x1E, 0x31, 0x00, 0x21, 0x01, 0x1F}), func(p []byte) error {
			_, err := service.DecodeAtomicReadFile(p, limits)
			return err
		}},
		{"read: count not unsigned", join(file, []byte{0x0E, 0x31, 0x00, 0x11, 0x0F}), func(p []byte) error {
			_, err := service.DecodeAtomicReadFile(p, limits)
			return err
		}},
		{"write: second access method", join(file, []byte{0x0E, 0x31, 0x00, 0x61, 0xAA, 0x0F, 0x1E, 0x31, 0x00, 0x21, 0x00, 0x1F}), decodeWrite(limits)},
		{"write stream: start not signed", join(file, []byte{0x0E, 0x21, 0x00, 0x61, 0xAA, 0x0F}), decodeWrite(limits)},
		{"write stream: data not octets", join(file, []byte{0x0E, 0x31, 0x00, 0x21, 0x01, 0x0F}), decodeWrite(limits)},
		{"write record: one parameter", join(file, []byte{0x1E, 0x31, 0x00, 0x1F}), decodeWrite(limits)},
		{"write record: start not signed", join(file, []byte{0x1E, 0x21, 0x00, 0x21, 0x00, 0x1F}), decodeWrite(limits)},
		{"write record: data not octets", join(file, []byte{0x1E, 0x31, 0x00, 0x21, 0x01, 0x21, 0x05, 0x1F}), decodeWrite(limits)},
		{"enrollment: max priority 256", []byte{0x09, 0x00, 0x4E, 0x09, 0x00, 0x1A, 0x01, 0x00, 0x4F}, func(p []byte) error {
			_, err := service.DecodeGetEnrollmentSummary(p, limits)
			return err
		}},
		{"cov multiple: two subscription lists", join([]byte{0x09, 0x01, 0x4E}, sub, []byte{0x4F, 0x4E}, sub, []byte{0x4F}), decodeCOVMultiple(limits)},
		{"cov multiple: constructed object", []byte{0x09, 0x01, 0x4E, 0x0E, 0x0F, 0x4F}, decodeCOVMultiple(limits)},
		{"cov multiple: short object", []byte{0x09, 0x01, 0x4E, 0x0B, 0x00, 0x80, 0x00, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x1F, 0x4F}, decodeCOVMultiple(limits)},
		{"cov multiple: no references", []byte{0x09, 0x01, 0x4E, 0x0C, 0x00, 0x80, 0x00, 0x01, 0x4F}, decodeCOVMultiple(limits)},
		{"cov multiple: timestamped of two octets", []byte{0x09, 0x01, 0x4E, 0x0C, 0x00, 0x80, 0x00, 0x01, 0x1E, 0x0E, 0x09, 0x55, 0x0F, 0x2A, 0x01, 0x00, 0x1F, 0x4F}, decodeCOVMultiple(limits)},
		{"audit: application field", byTarget(0x21, 0x01), decodeAudit(limits)},
		{"audit: constructed device", byTarget(0x0E, 0x0F), decodeAudit(limits)},
		{"audit: short device", byTarget(0x0B, 0x02, 0x00, 0x00), decodeAudit(limits)},
		{"audit: priority 256", byTarget(append(append([]byte{}, device...), 0x5A, 0x01, 0x00, 0x79, 0x00)...), decodeAudit(limits)},
	}
	for _, c := range cases {
		if err := c.decode(c.payload); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: % x: %v", c.name, c.payload, err)
		}
	}
	// localVTSessionIdentifier must be an Unsigned.
	if _, err := service.DecodeVTOpen([]byte{0x91, 0x00, 0x11}, limits); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Errorf("vt-open session BOOLEAN: %v", err)
	}
	// The by-target shape used above decodes with a valid priority.
	if req, err := service.DecodeAuditLogQuery(byTarget(append(append([]byte{}, device...), 0x59, 0x08, 0x79, 0x00)...), limits); err != nil ||
		req.ByTarget == nil || req.ByTarget.TargetPriority == nil || *req.ByTarget.TargetPriority != 8 || req.RequestedCount != 10 {
		t.Fatalf("valid audit query %+v %v", req, err)
	}
}

func decodeWrite(limits bacnet.DecodeLimits) func([]byte) error {
	return func(p []byte) error {
		_, err := service.DecodeAtomicWriteFile(p, limits)
		return err
	}
}

func decodeCOVMultiple(limits bacnet.DecodeLimits) func([]byte) error {
	return func(p []byte) error {
		_, err := service.DecodeSubscribeCOVPropertyMultiple(p, limits)
		return err
	}
}

func decodeAudit(limits bacnet.DecodeLimits) func([]byte) error {
	return func(p []byte) error {
		_, err := service.DecodeAuditLogQuery(p, limits)
		return err
	}
}
