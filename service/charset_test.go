// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// A text field keeps the character set it was sent in (clause 20.2.9): the
// decoder returns the octets and the set, bacnet.CharacterString.Text turns
// them into the Go string, and the encoder writes the same octets and set
// again.
func TestTextFieldCharacterSets(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	sets := []struct {
		name     string
		encoding uint8
		octets   string
		text     string // what Text returns; "" with unsupported
		unsupp   bool
	}{
		{name: "UTF-8", encoding: bacnet.CharacterEncodingUTF8, octets: "Hi é", text: "Hi é"},
		{name: "DBCS", encoding: bacnet.CharacterEncodingDBCS, octets: "\x00\x01Hi", unsupp: true},
		{name: "UCS-4", encoding: bacnet.CharacterEncodingUCS4, octets: "\x00\x00\x00H\x00\x00\x00i", text: "Hi"},
		{name: "UCS-2", encoding: bacnet.CharacterEncodingUCS2, octets: "\x00H\x00i", text: "Hi"},
		{name: "ISO 8859-1", encoding: bacnet.CharacterEncodingISO88591, octets: "caf\xe9", text: "café"},
	}
	// Each field: its wire form for a string, and what the decoder returns.
	type field struct {
		name   string
		wire   func(cs bacnet.CharacterString) []byte
		decode func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error)
	}
	ctxCS := func(tag uint8, cs bacnet.CharacterString) []byte {
		return must(bacnet.AppendContextCharacterString(nil, tag, cs))
	}
	appCS := func(cs bacnet.CharacterString) []byte {
		return must(bacnet.AppendApplicationValue(nil, bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: cs}))
	}
	vendor := must(bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(7)))
	plain := bacnet.CharacterString{Value: "x"}
	audit := func(source, target bacnet.CharacterString) []byte {
		var b []byte
		b = append(b, 0x2e) // source-device [2] { device [0] }
		b = append(b, must(bacnet.AppendContextObjectID(nil, 0, dev))...)
		b = append(b, 0x2f)
		b = append(b, must(bacnet.AppendContextUnsigned(nil, 4, 1))...) // operation [4]
		b = append(b, ctxCS(5, source)...)
		b = append(b, ctxCS(6, target)...)
		b = append(b, 0xae) // target-device [10] { device [0] }
		b = append(b, must(bacnet.AppendContextObjectID(nil, 0, dev))...)
		b = append(b, 0xaf)
		return append(append([]byte{0x0e}, b...), 0x0f) // one notification [0]
	}
	fields := []field{
		{
			name: "TextMessage message",
			wire: func(cs bacnet.CharacterString) []byte {
				b := must(bacnet.AppendContextObjectID(nil, 0, dev))
				b = append(b, must(bacnet.AppendContextUnsigned(nil, 2, 0))...)
				return append(b, ctxCS(3, cs)...)
			},
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				m, err := service.DecodeTextMessage(b, limits)
				return bacnet.CharacterString{Encoding: m.MessageEncoding, Value: m.Message}, func() ([]byte, error) { return service.EncodeTextMessage(m) }, err
			},
		},
		{
			name: "TextMessage message, constructed form",
			wire: func(cs bacnet.CharacterString) []byte {
				b := must(bacnet.AppendContextObjectID(nil, 0, dev))
				b = append(b, must(bacnet.AppendContextUnsigned(nil, 2, 0))...)
				return append(b, must(bacnet.AppendContextTagged(nil, 3, []bacnet.Element{{Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: cs}}}))...)
			},
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				m, err := service.DecodeTextMessage(b, limits)
				return bacnet.CharacterString{Encoding: m.MessageEncoding, Value: m.Message}, nil, err
			},
		},
		{
			name: "LifeSafetyOperation requesting source",
			wire: func(cs bacnet.CharacterString) []byte {
				b := must(bacnet.AppendContextUnsigned(nil, 0, 1))
				b = append(b, ctxCS(1, cs)...)
				return append(b, must(bacnet.AppendContextUnsigned(nil, 2, 3))...)
			},
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				r, err := service.DecodeLifeSafetyOperation(b, limits)
				return bacnet.CharacterString{Encoding: r.RequestingSourceEncoding, Value: r.RequestingSource}, func() ([]byte, error) { return service.EncodeLifeSafetyOperation(r) }, err
			},
		},
		{
			name: "AuditNotification source comment",
			wire: func(cs bacnet.CharacterString) []byte { return audit(cs, plain) },
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				n, err := service.DecodeAuditNotificationRequest(b, limits)
				if err != nil || len(n) != 1 || n[0].SourceComment == nil {
					return bacnet.CharacterString{}, nil, errors.Join(err, errors.New("no source comment"))
				}
				return bacnet.CharacterString{Encoding: n[0].SourceCommentEncoding, Value: *n[0].SourceComment}, func() ([]byte, error) { return service.EncodeAuditNotificationRequest(n) }, nil
			},
		},
		{
			name: "AuditNotification target comment",
			wire: func(cs bacnet.CharacterString) []byte { return audit(plain, cs) },
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				n, err := service.DecodeAuditNotificationRequest(b, limits)
				if err != nil || len(n) != 1 || n[0].TargetComment == nil {
					return bacnet.CharacterString{}, nil, errors.Join(err, errors.New("no target comment"))
				}
				return bacnet.CharacterString{Encoding: n[0].TargetCommentEncoding, Value: *n[0].TargetComment}, func() ([]byte, error) { return service.EncodeAuditNotificationRequest(n) }, nil
			},
		},
		{
			name: "Who-Am-I model name",
			wire: func(cs bacnet.CharacterString) []byte {
				return append(append(append([]byte(nil), vendor...), appCS(cs)...), appCS(plain)...)
			},
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				w, err := service.DecodeWhoAmI(b, limits)
				return bacnet.CharacterString{Encoding: w.ModelNameEncoding, Value: w.ModelName}, func() ([]byte, error) { return service.EncodeWhoAmI(w) }, err
			},
		},
		{
			name: "Who-Am-I serial number",
			wire: func(cs bacnet.CharacterString) []byte {
				return append(append(append([]byte(nil), vendor...), appCS(plain)...), appCS(cs)...)
			},
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				w, err := service.DecodeWhoAmI(b, limits)
				return bacnet.CharacterString{Encoding: w.SerialNumberEncoding, Value: w.SerialNumber}, func() ([]byte, error) { return service.EncodeWhoAmI(w) }, err
			},
		},
		{
			name: "You-Are model name",
			wire: func(cs bacnet.CharacterString) []byte {
				return append(append(append([]byte(nil), vendor...), appCS(cs)...), appCS(plain)...)
			},
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				y, err := service.DecodeYouAre(b, limits)
				return bacnet.CharacterString{Encoding: y.ModelNameEncoding, Value: y.ModelName}, func() ([]byte, error) { return service.EncodeYouAre(y) }, err
			},
		},
		{
			name: "You-Are serial number",
			wire: func(cs bacnet.CharacterString) []byte {
				return append(append(append([]byte(nil), vendor...), appCS(plain)...), appCS(cs)...)
			},
			decode: func(b []byte) (bacnet.CharacterString, func() ([]byte, error), error) {
				y, err := service.DecodeYouAre(b, limits)
				return bacnet.CharacterString{Encoding: y.SerialNumberEncoding, Value: y.SerialNumber}, func() ([]byte, error) { return service.EncodeYouAre(y) }, err
			},
		},
	}
	for _, f := range fields {
		for _, set := range sets {
			t.Run(f.name+"/"+set.name, func(t *testing.T) {
				wire := f.wire(bacnet.CharacterString{Encoding: set.encoding, Value: set.octets})
				got, encode, err := f.decode(wire)
				if err != nil {
					t.Fatal(err)
				}
				if got.Encoding != set.encoding || got.Value != set.octets {
					t.Fatalf("decoded character set %d, octets %q; want %d, %q", got.Encoding, got.Value, set.encoding, set.octets)
				}
				text, err := got.Text()
				switch {
				case set.unsupp && !errors.Is(err, bacnet.ErrUnsupported):
					t.Fatalf("Text: %q, %v; want ErrUnsupported", text, err)
				case !set.unsupp && (err != nil || text != set.text):
					t.Fatalf("Text: %q, %v; want %q", text, err, set.text)
				}
				if encode == nil {
					return // a form the encoder does not write
				}
				again, err := encode()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(again, wire) {
					t.Fatalf("re-encoded\n% x\nwant\n% x", again, wire)
				}
			})
		}
	}
}

// The zero value of a character set field is UTF-8, so a message built
// without it encodes as before.
func TestTextFieldCharacterSetZeroIsUTF8(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	got, err := service.EncodeTextMessage(service.TextMessage{TextMessageSourceDevice: dev, Message: "Hi"})
	if err != nil {
		t.Fatal(err)
	}
	want := must(bacnet.AppendContextObjectID(nil, 0, dev))
	want = append(want, must(bacnet.AppendContextUnsigned(nil, 2, 0))...)
	want = append(want, must(bacnet.AppendContextCharacterString(nil, 3, bacnet.CharacterString{Encoding: bacnet.CharacterEncodingUTF8, Value: "Hi"}))...)
	if !bytes.Equal(got, want) {
		t.Fatalf("% x, want % x", got, want)
	}
}
