// SPDX-License-Identifier: MIT

package capture

import (
	"fmt"
	"net/netip"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// Layer names the codec that failed.
type Layer string

// Layers of a BACnet/IP datagram, outermost first.
const (
	LayerBVLC    Layer = "bvlc"
	LayerNPDU    Layer = "npdu"
	LayerAPDU    Layer = "apdu"
	LayerService Layer = "service"
)

// Message is one datagram decoded through go-bacnet's codecs.
type Message struct {
	Datagram
	BVLC bvlc.Message
	NPDU *npdu.NPDU // nil for a BVLL message without an NPDU
	APDU *apdu.PDU  // nil for a network layer message
	// Origin is the B/IP node that originated the NPDU: the
	// Forwarded-NPDU origin, else Src.
	Origin netip.AddrPort
	// Service is the decoded service request or ACK (a service.* value),
	// set for complete messages; for a segmented message only on the
	// segment that completed it. Payload is the service payload it was
	// decoded from (the reassembled payload of a segmented message).
	Service any
	Payload []byte
	// Segment reports a segment of a segmented message; Reassembled that
	// this segment completed one.
	Segment, Reassembled bool
	// Err is the first codec error; ErrLayer the codec that returned it.
	Err      error
	ErrLayer Layer
}

// Decoder decodes datagrams in order, reassembling segmented confirmed
// requests and complex ACKs per sender, receiver and invoke ID.
type Decoder struct {
	Limits bacnet.DecodeLimits
	segs   map[segKey]*segState
}

// NewDecoder returns a decoder with the default limits.
func NewDecoder() *Decoder {
	return &Decoder{Limits: bacnet.DefaultDecodeLimits(), segs: map[segKey]*segState{}}
}

type segKey struct {
	from, to string
	invoke   uint8
	ack      bool
}

type segState struct {
	next    uint8
	payload []byte
}

// Decode decodes one datagram.
func (d *Decoder) Decode(dg Datagram) Message {
	m := Message{Datagram: dg, Origin: dg.Src}
	var err error
	if m.BVLC, err = bvlc.Parse(dg.Data, d.Limits); err != nil {
		m.Err, m.ErrLayer = err, LayerBVLC
		return m
	}
	switch m.BVLC.Function {
	case bvlc.FunctionOriginalUnicastNPDU, bvlc.FunctionOriginalBroadcastNPDU,
		bvlc.FunctionDistributeBroadcastToNetwork:
	case bvlc.FunctionForwardedNPDU:
		m.Origin = netip.AddrPortFrom(netip.AddrFrom4(m.BVLC.OriginIP), m.BVLC.OriginPort)
	default:
		return m
	}
	n, _, err := npdu.Parse(m.BVLC.Payload, d.Limits)
	if err != nil {
		m.Err, m.ErrLayer = err, LayerNPDU
		return m
	}
	m.NPDU = &n
	if n.NetworkMessage {
		return m
	}
	p, err := apdu.Parse(n.APDU, d.Limits)
	if err != nil {
		m.Err, m.ErrLayer = err, LayerAPDU
		return m
	}
	m.APDU = &p
	d.service(&m, n)
	return m
}

func (d *Decoder) service(m *Message, n npdu.NPDU) {
	var (
		fn      func([]byte, bacnet.DecodeLimits) (any, error)
		payload []byte
	)
	from := fmt.Sprintf("%s|%v", m.Origin, n.Source)
	to := fmt.Sprintf("%s|%v", m.Dst, n.Destination)
	p := m.APDU
	switch p.Type {
	case apdu.TypeConfirmedRequest:
		r := p.ConfirmedRequest
		fn, payload = confirmed[r.ServiceChoice], r.Payload
		if r.SegmentedMessage {
			if payload = d.reassemble(m, segKey{from, to, r.InvokeID, false}, r.SequenceNumber, r.MoreFollows, payload); payload == nil {
				return
			}
		}
	case apdu.TypeComplexACK:
		r := p.ComplexACK
		fn, payload = acks[r.ServiceChoice], r.Payload
		if r.SegmentedMessage {
			if payload = d.reassemble(m, segKey{from, to, r.InvokeID, true}, r.SequenceNumber, r.MoreFollows, payload); payload == nil {
				return
			}
		}
	case apdu.TypeUnconfirmedRequest:
		fn, payload = unconfirmed[p.UnconfirmedRequest.ServiceChoice], p.UnconfirmedRequest.Payload
	case apdu.TypeError:
		payload = p.Error.Payload
		fn = func(b []byte, l bacnet.DecodeLimits) (any, error) { return apdu.DecodeError(b, l) }
		if f, ok := errorDecoders[p.Error.ServiceChoice]; ok {
			fn = f
		}
	default:
		return
	}
	m.Payload = payload
	if fn == nil {
		return
	}
	v, err := fn(payload, d.Limits)
	if err != nil {
		m.Err, m.ErrLayer = err, LayerService
		return
	}
	m.Service = v
}

// reassemble collects one segment; it returns the whole payload when this
// segment is the last one of an in-order sequence, else nil.
func (d *Decoder) reassemble(m *Message, k segKey, seq uint8, more bool, payload []byte) []byte {
	m.Segment = true
	st := d.segs[k]
	if seq == 0 {
		st = &segState{}
		d.segs[k] = st
	}
	if st == nil || seq != st.next {
		return nil // duplicate, out of order, or the start was not captured
	}
	st.payload = append(st.payload, payload...)
	st.next++
	if more {
		return nil
	}
	delete(d.segs, k)
	m.Reassembled = true
	return st.payload
}

func wrap[T any](f func([]byte, bacnet.DecodeLimits) (T, error)) func([]byte, bacnet.DecodeLimits) (any, error) {
	return func(b []byte, l bacnet.DecodeLimits) (any, error) { return f(b, l) }
}

var confirmed = map[uint8]func([]byte, bacnet.DecodeLimits) (any, error){
	apdu.ServiceAcknowledgeAlarm:                 wrap(service.DecodeAcknowledgeAlarm),
	apdu.ServiceConfirmedCOVNotification:         wrap(service.DecodeCOVNotification),
	apdu.ServiceConfirmedEventNotification:       wrap(service.DecodeEventNotification),
	apdu.ServiceGetEnrollmentSummary:             wrap(service.DecodeGetEnrollmentSummary),
	apdu.ServiceSubscribeCOV:                     wrap(service.DecodeSubscribeCOV),
	apdu.ServiceAtomicReadFile:                   wrap(service.DecodeAtomicReadFile),
	apdu.ServiceAtomicWriteFile:                  wrap(service.DecodeAtomicWriteFile),
	apdu.ServiceAddListElement:                   wrap(service.DecodeListElementRequest),
	apdu.ServiceRemoveListElement:                wrap(service.DecodeListElementRequest),
	apdu.ServiceCreateObject:                     wrap(service.DecodeCreateObject),
	apdu.ServiceDeleteObject:                     wrap(service.DecodeDeleteObject),
	apdu.ServiceReadProperty:                     wrap(service.DecodeReadProperty),
	apdu.ServiceReadPropertyMultiple:             wrap(service.DecodeReadPropertyMultiple),
	apdu.ServiceWriteProperty:                    wrap(service.DecodeWriteProperty),
	apdu.ServiceWritePropertyMultiple:            wrap(service.DecodeWritePropertyMultiple),
	apdu.ServiceDeviceCommunicationControl:       wrap(service.DecodeDeviceCommunicationControl),
	apdu.ServiceConfirmedPrivateTransfer:         wrap(service.DecodePrivateTransfer),
	apdu.ServiceConfirmedTextMessage:             wrap(service.DecodeTextMessage),
	apdu.ServiceReinitializeDevice:               wrap(service.DecodeReinitializeDevice),
	apdu.ServiceVTOpen:                           wrap(service.DecodeVTOpen),
	apdu.ServiceVTData:                           wrap(service.DecodeVTData),
	apdu.ServiceReadRange:                        wrap(service.DecodeReadRange),
	apdu.ServiceLifeSafetyOperation:              wrap(service.DecodeLifeSafetyOperation),
	apdu.ServiceSubscribeCOVProperty:             wrap(service.DecodeSubscribeCOVProperty),
	apdu.ServiceGetEventInformation:              wrap(service.DecodeGetEventInformation),
	apdu.ServiceSubscribeCOVPropertyMultiple:     wrap(service.DecodeSubscribeCOVPropertyMultiple),
	apdu.ServiceConfirmedCOVNotificationMultiple: wrap(service.DecodeCOVNotificationMultiple),
	apdu.ServiceConfirmedAuditNotification:       wrap(service.DecodeAuditNotificationRequest),
	apdu.ServiceAuditLogQuery:                    wrap(service.DecodeAuditLogQuery),
	apdu.ServiceGetAlarmSummary: func(b []byte, l bacnet.DecodeLimits) (any, error) {
		return nil, service.DecodeGetAlarmSummary(b, l)
	},
}

var acks = map[uint8]func([]byte, bacnet.DecodeLimits) (any, error){
	apdu.ServiceGetAlarmSummary:          wrap(service.DecodeGetAlarmSummaryACK),
	apdu.ServiceGetEnrollmentSummary:     wrap(service.DecodeGetEnrollmentSummaryACK),
	apdu.ServiceAtomicReadFile:           wrap(service.DecodeAtomicReadFileACK),
	apdu.ServiceAtomicWriteFile:          wrap(service.DecodeAtomicWriteFileACK),
	apdu.ServiceCreateObject:             wrap(service.DecodeCreateObjectACK),
	apdu.ServiceReadProperty:             wrap(service.DecodeReadPropertyACK),
	apdu.ServiceReadPropertyMultiple:     wrap(service.DecodeReadPropertyMultipleACK),
	apdu.ServiceConfirmedPrivateTransfer: wrap(service.DecodePrivateTransfer),
	apdu.ServiceVTOpen:                   wrap(service.DecodeVTOpenACK),
	apdu.ServiceVTData:                   wrap(service.DecodeVTDataACK),
	apdu.ServiceReadRange:                wrap(service.DecodeReadRangeACK),
	apdu.ServiceGetEventInformation:      wrap(service.DecodeGetEventInformationACK),
	apdu.ServiceAuditLogQuery:            wrap(service.DecodeAuditLogQueryACK),
}

var unconfirmed = map[uint8]func([]byte, bacnet.DecodeLimits) (any, error){
	apdu.ServiceIAm:                                wrap(service.DecodeIAm),
	apdu.ServiceIHave:                              wrap(service.DecodeIHave),
	apdu.ServiceUnconfirmedCOV:                     wrap(service.DecodeCOVNotification),
	apdu.ServiceUnconfirmedEventNotification:       wrap(service.DecodeEventNotification),
	apdu.ServiceUnconfirmedPrivateTransfer:         wrap(service.DecodePrivateTransfer),
	apdu.ServiceUnconfirmedTextMessage:             wrap(service.DecodeTextMessage),
	apdu.ServiceTimeSynchronization:                wrap(service.DecodeTimeSynchronization),
	apdu.ServiceWhoHas:                             wrap(service.DecodeWhoHas),
	apdu.ServiceWhoIs:                              wrap(service.DecodeWhoIs),
	apdu.ServiceUTCTimeSynchronization:             wrap(service.DecodeTimeSynchronization),
	apdu.ServiceWriteGroup:                         wrap(service.DecodeWriteGroup),
	apdu.ServiceUnconfirmedCOVNotificationMultiple: wrap(service.DecodeCOVNotificationMultiple),
	apdu.ServiceUnconfirmedAuditNotification:       wrap(service.DecodeAuditNotificationRequest),
	apdu.ServiceWhoAmI:                             wrap(service.DecodeWhoAmI),
	apdu.ServiceYouAre:                             wrap(service.DecodeYouAre),
}

var errorDecoders = map[uint8]func([]byte, bacnet.DecodeLimits) (any, error){
	apdu.ServiceWritePropertyMultiple: wrap(service.DecodeWritePropertyMultipleError),
}
