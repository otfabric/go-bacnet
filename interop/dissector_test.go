//go:build dissector

// SPDX-License-Identifier: MIT

package interop

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Dissector oracle: every request the client can emit is captured from a
// recording transport, written to a pcap file and dissected by Wireshark's
// tshark. A frame tshark calls malformed, or flags with an expert warning or
// error, fails the test, and so does a request in which Wireshark does not
// find the fields listed for it. This is the only independent check available
// for requests no pinned peer decodes (Who-Am-I, AuthRequest, …).
//
//	make dissector        # needs tshark on PATH
//
// It checks requests as Wireshark understands them. It does not prove a
// device accepts the request, and it is only as strict as the cases make it:
// without listed fields a case asserts tag-level well-formedness and the
// service name, nothing about the layout.

// recordingTransport keeps every datagram the client sends and never
// delivers one.
type recordingTransport struct {
	local  bip.Endpoint
	mu     sync.Mutex
	frames [][]byte
	closed chan struct{}
	once   sync.Once
}

func (r *recordingTransport) Local() bip.Endpoint { return r.local }

func (r *recordingTransport) Send(_ context.Context, pkt client.OutboundPacket) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, append([]byte(nil), pkt.Data...))
	return nil
}

func (r *recordingTransport) Recv(ctx context.Context) (client.InboundPacket, error) {
	select {
	case <-ctx.Done():
		return client.InboundPacket{}, ctx.Err()
	case <-r.closed:
		return client.InboundPacket{}, errors.New("closed")
	}
}

func (r *recordingTransport) Close() error {
	r.once.Do(func() { close(r.closed) })
	return nil
}

func (r *recordingTransport) take() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.frames
	r.frames = nil
	return out
}

// writePcap wraps each BACnet/IP datagram in Ethernet/IPv4/UDP.
func writePcap(t *testing.T, path string, src, dst netip.AddrPort, frames [][]byte) {
	t.Helper()
	var buf bytes.Buffer
	le := binary.LittleEndian
	hdr := make([]byte, 24)
	le.PutUint32(hdr[0:], 0xa1b2c3d4)
	le.PutUint16(hdr[4:], 2)
	le.PutUint16(hdr[6:], 4)
	le.PutUint32(hdr[16:], 65535)
	le.PutUint32(hdr[20:], 1) // LINKTYPE_ETHERNET
	buf.Write(hdr)
	s4, d4 := src.Addr().As4(), dst.Addr().As4()
	for i, payload := range frames {
		udpLen := 8 + len(payload)
		pkt := make([]byte, 0, 14+20+udpLen)
		pkt = append(pkt, 0x02, 0, 0, 0, 0, 2, 0x02, 0, 0, 0, 0, 1, 0x08, 0x00)
		ip := []byte{0x45, 0, 0, 0, 0, 0, 0, 0, 64, 17, 0, 0}
		binary.BigEndian.PutUint16(ip[2:], uint16(20+udpLen))
		ip = append(ip, s4[:]...)
		ip = append(ip, d4[:]...)
		var sum uint32
		for j := 0; j < 20; j += 2 {
			sum += uint32(binary.BigEndian.Uint16(ip[j:]))
		}
		for sum>>16 != 0 {
			sum = sum&0xffff + sum>>16
		}
		binary.BigEndian.PutUint16(ip[10:], ^uint16(sum))
		pkt = append(pkt, ip...)
		udp := make([]byte, 8)
		binary.BigEndian.PutUint16(udp[0:], src.Port())
		binary.BigEndian.PutUint16(udp[2:], dst.Port())
		binary.BigEndian.PutUint16(udp[4:], uint16(udpLen)) // checksum 0 = none
		pkt = append(pkt, udp...)
		pkt = append(pkt, payload...)
		rec := make([]byte, 16)
		le.PutUint32(rec[0:], uint32(1700000000+i))
		le.PutUint32(rec[8:], uint32(len(pkt)))
		le.PutUint32(rec[12:], uint32(len(pkt)))
		buf.Write(rec)
		buf.Write(pkt)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

type dissectorCase struct {
	name string
	// info is what Wireshark's Info column must contain for the frame; empty
	// when Wireshark has no name for the service.
	info string
	call func(ctx context.Context, c *client.Client, target client.Target) error
	// fields are labelled values Wireshark must show in the decoded tree
	// (whitespace-insensitive). The dissector walks tags leniently and does
	// not flag a wrong structure as malformed, so for requests whose layout
	// is easy to get wrong these labels are the actual check: Wireshark only
	// assigns them when the tags sit where the standard puts them. Object
	// identifiers are left out: Wireshark 4.2 and 4.6 render them differently.
	fields []string
}

func u32(v uint32) *uint32 { return &v }

func dissectorCases() []dissectorCase {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1234}
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	tl := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	idx := uint32(3)
	prio := uint8(8)
	inc := float32(0.5)
	avType := bacnet.ObjectTypeAnalogValue
	bcast := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.255:47808"))
	text := func(s string) bacnet.CharacterString { return bacnet.CharacterString{Value: s} }
	seqStamp := service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 7}

	return []dissectorCase{
		{"ReadProperty", "readProperty", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.ReadProperty(ctx, t, dev, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &idx})
			return err
		}, nil},
		{"ReadPropertyMultiple", "readPropertyMultiple", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.ReadPropertyMultiple(ctx, t, []service.ReadAccessSpecification{
				{Object: dev, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyObjectList, ArrayIndex: &idx}}},
				{Object: av, Properties: []bacnet.PropertyReference{pv}},
			})
			return err
		}, nil},
		{"WriteProperty", "writeProperty", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.WriteProperty(ctx, t, av, pv, bacnet.RealValue(21.5), &prio)
		}, nil},
		{"WriteProperty NULL (relinquish)", "writeProperty", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.RelinquishPriority(ctx, t, av, 8)
		}, nil},
		{"WritePropertyMultiple", "writePropertyMultiple", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.WritePropertyMultiple(ctx, t, []service.WriteAccessSpecification{{Object: av, Properties: []service.WritePropertyValue{
				{Property: pv, Value: bacnet.RealValue(1), Priority: &prio},
				{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyDescription}, Value: bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: text("x")}},
			}}})
		}, nil},
		{"ReadRange byPosition", "readRange", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.ReadRange(ctx, t, service.ReadRangeRequest{Object: tl, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
				By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 10})
			return err
		}, nil},
		{"ReadRange bySequenceNumber", "readRange", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.ReadRange(ctx, t, service.ReadRangeRequest{Object: tl, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
				By: service.ReadRangeBySequenceNumber, ReferenceIndex: 100, Count: -5})
			return err
		}, nil},
		{"SubscribeCOV", "subscribeCOV", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.SubscribeCOV(ctx, t, av, client.COVOptions{Lifetime: 120, IssueConfirmed: true})
			return err
		}, nil},
		{"SubscribeCOVProperty", "subscribeCOVProperty", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.SubscribeCOVProperty(ctx, t, av, pv, client.COVOptions{Lifetime: 120}, &inc)
			return err
		}, nil},
		{"SubscribeCOVPropertyMultiple", "subscribeCOVPropertyMultiple", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.SubscribeCOVPropertyMultiple(ctx, t, service.SubscribeCOVPropertyMultipleRequest{
				SubscriberProcessIdentifier: 5,
				IssueConfirmedNotifications: true,
				LifetimeRemaining:           u32(300),
				MaxNotificationDelay:        u32(10),
				Subscriptions: []service.COVMultipleSubscription{{Object: av, Properties: []service.COVPropertyReference{
					{Property: pv, COVIncrement: &inc, Timestamped: true},
				}}},
			})
		}, []string{"subscriber Process Id: (Unsigned) 5", "life time (hh.mm.ss): 0.05.00", "notification delay (hh.mm.ss): 0.00.10", "list-of-cov-subscription-specifications", "list-of-cov-references"}},
		{"SubscribeCOVPropertyMultiple cancellation", "subscribeCOVPropertyMultiple", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.SubscribeCOVPropertyMultiple(ctx, t, service.SubscribeCOVPropertyMultipleRequest{
				SubscriberProcessIdentifier: 5,
				Cancellation:                true,
				Subscriptions: []service.COVMultipleSubscription{{Object: av, Properties: []service.COVPropertyReference{
					{Property: pv},
				}}},
			})
		}, []string{"subscriber Process Id: (Unsigned) 5", "list-of-cov-subscription-specifications", "list-of-cov-references"}},
		{"AtomicReadFile stream", "atomicReadFile", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.AtomicReadFile(ctx, t, service.AtomicReadFileRequest{File: file, Access: service.FileAccessStream, StartPosition: 0, Count: 64})
			return err
		}, nil},
		{"AtomicReadFile record", "atomicReadFile", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.AtomicReadFile(ctx, t, service.AtomicReadFileRequest{File: file, Access: service.FileAccessRecord, StartPosition: 2, Count: 3})
			return err
		}, nil},
		{"AtomicWriteFile stream", "atomicWriteFile", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.AtomicWriteFile(ctx, t, service.AtomicWriteFileRequest{File: file, Access: service.FileAccessStream, StartPosition: 0, Data: []byte("data")})
			return err
		}, nil},
		{"AtomicWriteFile record", "atomicWriteFile", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.AtomicWriteFile(ctx, t, service.AtomicWriteFileRequest{File: file, Access: service.FileAccessRecord, StartPosition: 0, Records: [][]byte{[]byte("r1"), []byte("r2")}})
			return err
		}, nil},
		{"CreateObject by type", "createObject", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.CreateObject(ctx, t, service.CreateObjectRequest{ObjectType: &avType,
				InitialValues: []service.PropertyValue{{Property: pv, Value: bacnet.RealValue(2)}}})
			return err
		}, []string{"Object Type: analog-value (2)", "Property Identifier: present-value (85)", "Present Value (real): 2"}},
		{"CreateObject by identifier", "createObject", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.CreateObject(ctx, t, service.CreateObjectRequest{ObjectIdentifier: &av})
			return err
		}, nil},
		{"DeleteObject", "deleteObject", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.DeleteObject(ctx, t, service.DeleteObjectRequest{Object: av})
		}, nil},
		{"AddListElement", "addListElement", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.AddListElement(ctx, t, service.ListElementRequest{Object: dev, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList},
				Elements: []bacnet.Element{{Value: bacnet.ObjectIDValue(av)}}})
		}, nil},
		{"RemoveListElement", "removeListElement", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.RemoveListElement(ctx, t, service.ListElementRequest{Object: dev, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList},
				Elements: []bacnet.Element{{Value: bacnet.ObjectIDValue(av)}}})
		}, nil},
		{"AcknowledgeAlarm", "acknowledgeAlarm", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.AcknowledgeAlarm(ctx, t, service.AcknowledgeAlarmRequest{ProcessIdentifier: 1, EventObject: av, EventStateAcked: 2,
				TimeStamp: seqStamp, AcknowledgmentSource: text("operator"),
				TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampDateTime, DateTime: service.DateTime{
					Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12}}}})
		}, []string{"acknowledging Process Id: (Unsigned) 1", "event State Acknowledged: offnormal (2)", "sequence number: (Unsigned) 7", "acknowledgement Source: UTF-8 'operator'", "acknowledgement timestamp"}},
		{"GetEventInformation", "getEventInformation", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.GetEventInformation(ctx, t, service.GetEventInformationRequest{LastReceived: &av})
			return err
		}, nil},
		{"GetAlarmSummary", "getAlarmSummary", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.GetAlarmSummary(ctx, t)
			return err
		}, nil},
		{"GetEnrollmentSummary with filters", "getEnrollmentSummary", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.GetEnrollmentSummary(ctx, t, service.GetEnrollmentSummaryRequest{
				EventStateFilter: u32(2), EventTypeFilter: u32(5), PriorityFilter: &[2]uint32{1, 200}, NotificationClassFilter: u32(1)})
			return err
		}, []string{"acknowledgment Filter: all (0)", "event State Filter:", "event Type Filter:", "min Priority: (Unsigned) 1", "max Priority: (Unsigned) 200", "notification Class Filter: (Unsigned) 1"}},
		{"DeviceCommunicationControl", "deviceCommunicationControl", func(ctx context.Context, c *client.Client, t client.Target) error {
			minutes := uint16(5)
			pw := text("secret")
			return c.DeviceCommunicationControl(ctx, t, service.DeviceCommunicationControlRequest{
				TimeDuration: &minutes, EnableDisable: service.EnableDisableDisableInitiation, Password: &pw})
		}, []string{"time Duration: (Unsigned) 5", "enable-disable: disable-initiation (2)", "Password: UTF-8 'secret'"}},
		{"ReinitializeDevice", "reinitializeDevice", func(ctx context.Context, c *client.Client, t client.Target) error {
			pw := text("secret")
			return c.ReinitializeDevice(ctx, t, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart, Password: &pw})
		}, nil},
		{"ConfirmedPrivateTransfer", "confirmedPrivateTransfer", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.ConfirmedPrivateTransfer(ctx, t, service.PrivateTransfer{VendorID: 260, ServiceNumber: 1,
				ServiceParameters: []bacnet.Element{{Value: bacnet.UnsignedValue(42)}}})
		}, nil},
		{"ConfirmedTextMessage", "confirmedTextMessage", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.ConfirmedTextMessage(ctx, t, service.TextMessage{TextMessageSourceDevice: dev, MessageClass: u32(1), MessagePriority: 1, Message: "hello"})
		}, nil},
		{"LifeSafetyOperation", "lifeSafetyOperation", func(ctx context.Context, c *client.Client, t client.Target) error {
			lsp := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeLifeSafetyPoint, Instance: 1}
			return c.LifeSafetyOperation(ctx, t, service.LifeSafetyOperationRequest{RequestingProcessIdentifier: 1, RequestingSource: "panel", Request: 4, Object: &lsp})
		}, []string{"requesting Process Id: (Unsigned) 1", "requesting Source: UTF-8 'panel'", "request: reset (4)"}},
		{"VT-Open", "vtOpen", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.VTOpen(ctx, t, service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 3})
			return err
		}, nil},
		{"VT-Close", "vtClose", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.VTClose(ctx, t, service.VTCloseRequest{RemoteVTSessionIdentifiers: []uint8{3, 4}})
		}, nil},
		{"VT-Data", "vtData", func(ctx context.Context, c *client.Client, t client.Target) error {
			_, err := c.VTData(ctx, t, service.VTDataRequest{VTSessionIdentifier: 3, VTNewData: []byte("ls\r"), VTDataFlag: 0})
			return err
		}, nil},
		{"AuditLogQuery", "auditLogQuery", func(ctx context.Context, c *client.Client, t client.Target) error {
			prop, start := bacnet.PropertyPresentValue, uint64(100)
			_, err := c.AuditLogQuery(ctx, t, service.AuditLogQueryRequest{
				AuditLog: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAuditLog, Instance: 1},
				ByTarget: &service.AuditQueryByTarget{TargetDevice: dev, TargetObject: &av, TargetProperty: &prop,
					Operations: &bacnet.BitString{UnusedBits: 6, Bytes: []byte{0x40}}, SuccessFilter: service.AuditSuccessSuccessesOnly},
				StartAtSequenceNumber: &start, RequestedCount: 10})
			return err
		}, []string{"query-parameters", "target-parameters", "Property Identifier: present-value (85)", "write = TRUE",
			"target-successful-action: successes-only (1)", "start-at-sequence-number: (Unsigned) 100", "requested-count: (Unsigned) 10"}},

		{"Who-Is with limits", "who-Is", func(ctx context.Context, c *client.Client, _ client.Target) error {
			return c.SendWhoIs(ctx, bcast, true, client.DiscoveryOptions{LowLimit: u32(1000), HighLimit: u32(2000)})
		}, nil},
		{"Who-Is to a remote network", "who-Is", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.SendWhoIs(ctx, t.Endpoint, false, client.DiscoveryOptions{Address: bacnet.RemoteBroadcast(7)})
		}, nil},
		{"Who-Has by name", "who-Has", func(ctx context.Context, c *client.Client, _ client.Target) error {
			name := text("AV-1")
			return c.SendWhoHas(ctx, bcast, true, client.WhoHasOptions{Name: &name})
		}, nil},
		{"Who-Has by object with limits", "who-Has", func(ctx context.Context, c *client.Client, _ client.Target) error {
			return c.SendWhoHas(ctx, bcast, true, client.WhoHasOptions{Object: &av, LowLimit: u32(1), HighLimit: u32(4194302)})
		}, nil},
		{"TimeSynchronization", "timeSynchronization", func(ctx context.Context, c *client.Client, _ client.Target) error {
			return c.TimeSynchronization(ctx, bcast, true, service.TimeSynchronization{
				Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 12, Minute: 30}})
		}, nil},
		{"UTCTimeSynchronization", "utcTimeSynchronization", func(ctx context.Context, c *client.Client, _ client.Target) error {
			return c.UTCTimeSynchronization(ctx, bcast, true, service.TimeSynchronization{
				Date: bacnet.Date{Year: 126, Month: 10, Day: 10, Weekday: 6}, Time: bacnet.Time{Hour: 10, Minute: 30}})
		}, nil},
		{"UnconfirmedPrivateTransfer", "unconfirmedPrivateTransfer", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.UnconfirmedPrivateTransfer(ctx, t.Endpoint, false, service.PrivateTransfer{VendorID: 260, ServiceNumber: 1})
		}, nil},
		{"UnconfirmedTextMessage", "unconfirmedTextMessage", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.UnconfirmedTextMessage(ctx, t.Endpoint, false, service.TextMessage{TextMessageSourceDevice: dev, Message: "hello"})
		}, nil},
		{"WriteGroup", "writeGroup", func(ctx context.Context, c *client.Client, t client.Target) error {
			inhibit := true
			return c.WriteGroup(ctx, t.Endpoint, false, service.WriteGroup{GroupNumber: 1, WritePriority: 8, InhibitDelay: &inhibit,
				ChangeList: []bacnet.Element{
					{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)},
					{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(1)}}}},
				}})
		}, nil},
		{"Who-Am-I", "", func(ctx context.Context, c *client.Client, _ client.Target) error {
			return c.WhoAmI(ctx, bcast, true, service.WhoAmI{VendorID: 260, ModelName: "model", SerialNumber: "sn-1"})
		}, []string{"Vendor ID: (Unsigned) 260", "Model name: UTF-8 'model'", "Serial number: UTF-8 'sn-1'"}},
		{"You-Are", "", func(ctx context.Context, c *client.Client, t client.Target) error {
			return c.YouAre(ctx, t.Endpoint, false, service.YouAre{VendorID: 260, ModelName: "model", SerialNumber: "sn-1", Device: dev})
		}, []string{"Vendor ID: (Unsigned) 260", "Serial number: UTF-8 'sn-1'"}},
		{"Who-Is-Router-To-Network", "Who-Is-Router-To-Network", func(ctx context.Context, c *client.Client, _ client.Target) error {
			net := uint16(7)
			return c.WhoIsRouterToNetwork(ctx, &net)
		}, nil},
		{"ReadProperty to a routed device", "readProperty", func(ctx context.Context, c *client.Client, t client.Target) error {
			routed := client.Target{Address: bacnet.RemoteStation(7, bacnet.MustMAC([]byte{0x0a})), Endpoint: t.Endpoint, MaxAPDU: 480}
			_, err := c.ReadProperty(ctx, routed, dev, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
			return err
		}, nil},
	}
}

func TestDissectorAcceptsClientRequests(t *testing.T) {
	tshark, err := exec.LookPath("tshark")
	if err != nil {
		if os.Getenv("BACNET_DISSECTOR_REQUIRED") != "" {
			t.Fatal("tshark not found on PATH")
		}
		t.Skip("tshark not found on PATH")
	}
	local := netip.MustParseAddrPort("10.0.0.1:47808")
	peer := netip.MustParseAddrPort("10.0.0.2:47808")
	rec := &recordingTransport{local: bip.NewEndpoint(local), closed: make(chan struct{})}
	c, err := client.New(
		client.WithTransport(rec),
		// Nothing answers: each confirmed request is sent once and given up on.
		client.WithTransactionOptions(30*time.Millisecond, 0, 30*time.Millisecond),
		client.WithDeviceManagementEnabled(client.DeviceManagementConfirm),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	target := client.Target{Endpoint: bip.NewEndpoint(peer), MaxAPDU: 1476}

	cases := dissectorCases()
	var frames [][]byte
	var owner []int // case index of each frame
	for i, tc := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := tc.call(ctx, c, target)
		cancel()
		sent := rec.take()
		if len(sent) == 0 {
			t.Errorf("%s: nothing was sent (%v)", tc.name, err)
			continue
		}
		// Only the request itself: cancellations and aborts that follow a
		// timeout are separate frames of the same case.
		frames = append(frames, sent[0])
		owner = append(owner, i)
	}

	pcap := filepath.Join(t.TempDir(), "client-requests.pcap")
	writePcap(t, pcap, local, peer, frames)
	if keep := os.Getenv("BACNET_DISSECTOR_PCAP"); keep != "" {
		writePcap(t, keep, local, peer, frames)
		t.Logf("pcap kept at %s", keep)
	}
	out, err := exec.Command(tshark, "-r", pcap, "-T", "fields", "-E", "separator=|",
		"-e", "frame.number", "-e", "_ws.col.protocol", "-e", "_ws.col.info",
		"-e", "_ws.malformed", "-e", "_ws.expert.severity", "-e", "_ws.expert.message").Output()
	if err != nil {
		t.Fatalf("tshark: %v\n%s", err, out)
	}
	verbose, err := exec.Command(tshark, "-r", pcap, "-V").Output()
	if err != nil {
		t.Fatalf("tshark -V: %v", err)
	}
	// One block per frame, whitespace collapsed for label matching.
	var trees []string
	for _, block := range strings.Split("\n"+string(verbose), "\nFrame ")[1:] {
		trees = append(trees, strings.Join(strings.Fields(block), " "))
	}
	if len(trees) != len(frames) {
		t.Fatalf("tshark -V reported %d frames for %d written", len(trees), len(frames))
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(frames) {
		t.Fatalf("tshark reported %d frames for %d written:\n%s", len(lines), len(frames), out)
	}
	for n, line := range lines {
		tc := cases[owner[n]]
		f := strings.SplitN(line, "|", 6)
		if len(f) != 6 {
			t.Errorf("%s: unexpected tshark line %q", tc.name, line)
			continue
		}
		proto, info, malformed, severity, expert := f[1], f[2], f[3], f[4], f[5]
		t.Logf("%-34s %-8s %s", tc.name, proto, info)
		if !strings.HasPrefix(proto, "BACnet") && proto != "BVLC" {
			t.Errorf("%s: dissected as %q, not BACnet", tc.name, proto)
		}
		if malformed != "" || strings.Contains(strings.ToLower(info), "malformed") {
			t.Errorf("%s: malformed packet: %s | %s\n  frame % x", tc.name, info, expert, frames[n])
		}
		// Expert severities: chat 0x200000, note 0x400000, warn 0x600000, error 0x800000.
		for _, sev := range strings.Split(severity, ",") {
			if sev == "6291456" || sev == "8388608" {
				t.Errorf("%s: expert warning/error: %s\n  frame % x", tc.name, expert, frames[n])
				break
			}
		}
		if tc.info != "" && !strings.Contains(strings.ToLower(info), strings.ToLower(tc.info)) {
			t.Errorf("%s: Info %q does not name %q", tc.name, info, tc.info)
		}
		for _, want := range tc.fields {
			if !strings.Contains(trees[n], strings.Join(strings.Fields(want), " ")) {
				t.Errorf("%s: Wireshark does not decode %q from the request\n  frame % x", tc.name, want, frames[n])
			}
		}
	}
}
