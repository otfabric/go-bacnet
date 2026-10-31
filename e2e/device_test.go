// SPDX-License-Identifier: MIT

package e2e_test

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

const (
	deviceInstance = 1234
	vendorID       = 999
	errorClassObj  = 1  // error-class object
	errorCodeObj   = 31 // error-code unknown-object
	errorClassProp = 2  // error-class property
	errorCodeProp  = 32 // error-code unknown-property
)

var (
	analogValue = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	// silentValue is an object whose requests the device never answers.
	silentValue = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	missing     = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 99}
	limits      = bacnet.DefaultDecodeLimits()
)

type key struct {
	object   bacnet.ObjectIdentifier
	property bacnet.PropertyIdentifier
}

// device is a BACnet/IP device on a UDP socket: Who-Is, ReadProperty,
// WriteProperty and SubscribeCOV.
type device struct {
	t    testing.TB
	conn *net.UDPConn
	done chan struct{}

	mu         sync.Mutex
	properties map[key]bacnet.ApplicationValue
	subscriber *net.UDPAddr
	process    uint32
	cancelled  bool
}

func startDevice(t testing.TB) *device {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	d := &device{t: t, conn: conn, done: make(chan struct{}), properties: map[key]bacnet.ApplicationValue{
		{analogValue, bacnet.PropertyPresentValue}: bacnet.RealValue(21.5),
		{analogValue, bacnet.PropertyObjectName}:   {Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "room temperature"}},
	}}
	go d.serve()
	t.Cleanup(func() {
		_ = conn.Close()
		<-d.done
	})
	return d
}

func (d *device) endpoint() bip.Endpoint {
	a := d.conn.LocalAddr().(*net.UDPAddr)
	return bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), uint16(a.Port)))
}

func (d *device) serve() {
	defer close(d.done)
	buf := make([]byte, 1500)
	for {
		n, from, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if err := d.handle(buf[:n], from); err != nil {
			d.t.Errorf("device: %v", err)
		}
	}
}

func (d *device) handle(frame []byte, from *net.UDPAddr) error {
	msg, err := bvlc.Parse(frame, limits)
	if err != nil {
		return err
	}
	n, _, err := npdu.Parse(msg.Payload, limits)
	if err != nil {
		return err
	}
	pdu, err := apdu.Parse(n.APDU, limits)
	if err != nil {
		return err
	}
	switch {
	case pdu.UnconfirmedRequest != nil:
		if pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceWhoIs {
			return nil
		}
		iam, err := service.EncodeIAm(service.IAm{
			Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: deviceInstance},
			MaxAPDULength: 1476, VendorID: vendorID,
		})
		if err != nil {
			return err
		}
		return d.send(from, apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIAm, Payload: iam}))
	case pdu.ConfirmedRequest != nil:
		return d.confirmed(pdu.ConfirmedRequest, from)
	}
	return nil // acknowledgements of nothing this device asked
}

func (d *device) confirmed(req *apdu.ConfirmedRequest, from *net.UDPAddr) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch req.ServiceChoice {
	case apdu.ServiceReadProperty:
		r, err := service.DecodeReadProperty(req.Payload, limits)
		if err != nil {
			return err
		}
		if r.Object == silentValue {
			return nil
		}
		v, ok := d.properties[key{r.Object, r.Property.Identifier}]
		if !ok {
			return d.refuse(req, from, r.Object)
		}
		ack, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: r.Object, Property: r.Property, Value: v})
		if err != nil {
			return err
		}
		return d.send(from, apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: ack}))
	case apdu.ServiceWriteProperty:
		w, err := service.DecodeWriteProperty(req.Payload, limits)
		if err != nil {
			return err
		}
		k := key{w.Object, w.Property.Identifier}
		if _, ok := d.properties[k]; !ok {
			return d.refuse(req, from, w.Object)
		}
		d.properties[k] = w.Value
		if err := d.send(from, apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice})); err != nil {
			return err
		}
		return d.notify(w.Object, w.Value)
	case apdu.ServiceSubscribeCOV:
		s, err := service.DecodeSubscribeCOV(req.Payload, limits)
		if err != nil {
			return err
		}
		d.process = s.ProcessIdentifier
		d.subscriber = from
		d.cancelled = s.Cancellation
		return d.send(from, apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice}))
	}
	return errors.New("unexpected confirmed service")
}

// refuse answers with the Error a device gives for the object or property.
func (d *device) refuse(req *apdu.ConfirmedRequest, from *net.UDPAddr, object bacnet.ObjectIdentifier) error {
	class, code := uint32(errorClassProp), uint32(errorCodeProp)
	if object == missing {
		class, code = errorClassObj, errorCodeObj
	}
	payload, err := bacnet.AppendApplicationValue(nil, bacnet.EnumValue(class))
	if err != nil {
		return err
	}
	if payload, err = bacnet.AppendApplicationValue(payload, bacnet.EnumValue(code)); err != nil {
		return err
	}
	return d.send(from, apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload}))
}

// notify sends the subscriber an unconfirmed COV notification.
func (d *device) notify(object bacnet.ObjectIdentifier, v bacnet.ApplicationValue) error {
	if d.subscriber == nil || d.cancelled {
		return nil
	}
	// service has a decoder for this notification and no encoder: a client
	// does not send one.
	note, err := bacnet.AppendContextUnsigned(nil, 0, uint64(d.process))
	if err != nil {
		return err
	}
	if note, err = bacnet.AppendContextObjectID(note, 1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: deviceInstance}); err != nil {
		return err
	}
	if note, err = bacnet.AppendContextObjectID(note, 2, object); err != nil {
		return err
	}
	if note, err = bacnet.AppendContextUnsigned(note, 3, 60); err != nil {
		return err
	}
	note = append(note, 0x4E) // opening [4], list of values
	if note, err = bacnet.AppendContextUnsigned(note, 0, uint64(bacnet.PropertyPresentValue)); err != nil {
		return err
	}
	note = append(note, 0x2E) // opening [2], value
	if note, err = bacnet.AppendApplicationValue(note, v); err != nil {
		return err
	}
	note = append(note, 0x2F, 0x4F) // closing [2], closing [4]
	return d.send(d.subscriber, apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceUnconfirmedCOV, Payload: note}))
}

func (d *device) send(to *net.UDPAddr, pdu []byte) error {
	n, err := npdu.Append(nil, npdu.NPDU{Version: 1, APDU: pdu})
	if err != nil {
		return err
	}
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: n})
	if err != nil {
		return err
	}
	if binary.BigEndian.Uint16(frame[2:4]) != uint16(len(frame)) {
		return errors.New("BVLC length does not match the frame")
	}
	_, err = d.conn.WriteToUDP(frame, to)
	return err
}
