// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// The decoders below refuse a message in which a field comes twice, sits
// where it does not belong, or in which a CHOICE has no alternative or two:
// each of these has no single reading. What stays accepted is listed with
// each table (accepted: true) and in LIMITATIONS.md.

type fieldCase struct {
	name     string
	payload  string // hex
	accepted bool
}

func runFieldCases(t *testing.T, cases []fieldCase, decode func([]byte) error) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := decode(fromHex(tc.payload))
			switch {
			case tc.accepted && err != nil:
				t.Fatalf("refused: %v", err)
			case !tc.accepted && !errors.Is(err, bacnet.ErrMalformed):
				t.Fatalf("error %v, want one that wraps ErrMalformed", err)
			}
		})
	}
}

const (
	fileStream = "0e" + "3100" + "62aabb" + "0f"          // [0] { start 0, data }
	fileRecord = "1e" + "3100" + "2101" + "62ccdd" + "1f" // [1] { start 0, count 1, one record }
)

func TestAtomicReadFileACKAccessAlternative(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	runFieldCases(t, []fieldCase{
		{name: "stream", payload: "11" + fileStream, accepted: true},
		{name: "record", payload: "11" + fileRecord, accepted: true},
		{name: "no access alternative", payload: "11"},
		{name: "both access alternatives", payload: "11" + fileStream + fileRecord},
		{name: "record then stream", payload: "11" + fileRecord + fileStream},
		{name: "stream twice", payload: "11" + fileStream + fileStream},
		{name: "record twice", payload: "11" + fileRecord + fileRecord},
		{name: "endOfFile twice", payload: "1011" + fileStream},
		{name: "endOfFile after the alternative, twice", payload: "11" + fileStream + "10"},
		// Accepted as before: the count is returned next to the records.
		{name: "returnedRecordCount differs from the records", payload: "11" + "1e" + "3100" + "2105" + "62ccdd" + "1f", accepted: true},
	}, func(p []byte) error {
		_, err := service.DecodeAtomicReadFileACK(p, limits)
		return err
	})

	// The alternative that is present is the one reported.
	ack, err := service.DecodeAtomicReadFileACK(fromHex("11"+fileRecord), limits)
	if err != nil || ack.Access != service.FileAccessRecord || len(ack.Records) != 1 || ack.Data != nil || !ack.EndOfFile {
		t.Fatalf("record ACK %+v, %v", ack, err)
	}
}

const (
	rpmObject = "0c00800001"             // objectIdentifier [0] analog-value 1
	rpmValue  = "4e" + "2105" + "4f"     // propertyValue [4] { Unsigned 5 }
	rpmError  = "5e" + "91029120" + "5f" // propertyAccessError [5] { property, unknown-property }
)

func TestReadPropertyMultipleACKFieldOrder(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	runFieldCases(t, []fieldCase{
		{name: "one value", payload: rpmObject + "1e" + "2955" + rpmValue + "1f", accepted: true},
		{name: "index then value", payload: rpmObject + "1e" + "2955" + "3901" + rpmValue + "1f", accepted: true},
		{name: "index then error", payload: rpmObject + "1e" + "2955" + "3901" + rpmError + "1f", accepted: true},
		{name: "two objects", payload: rpmObject + "1e" + "2955" + rpmValue + "1f" + "0c00800002" + "1e" + "2955" + rpmValue + "1f", accepted: true},
		{name: "second result list for one object", payload: rpmObject + "1e" + "2955" + rpmValue + "1f" + "1e" + "294d" + rpmValue + "1f"},
		{name: "index after the value", payload: rpmObject + "1e" + "2955" + rpmValue + "3901" + "1f"},
		{name: "index after the error", payload: rpmObject + "1e" + "2955" + rpmError + "3901" + "1f"},
		{name: "index twice", payload: rpmObject + "1e" + "2955" + "3901" + "3902" + rpmValue + "1f"},
		// Accepted as before (LIMITATIONS.md): devices send them.
		{name: "object without a result list", payload: rpmObject, accepted: true},
		{name: "empty result list", payload: rpmObject + "1e" + "1f", accepted: true},
		{name: "empty acknowledgement", payload: "", accepted: true},
	}, func(p []byte) error {
		_, err := service.DecodeReadPropertyMultipleACK(p, limits)
		return err
	})
}

const (
	covmHead   = "0901" + "1c02000001" + "2900" // process 1, device 1, time remaining 0
	covmStamp  = "3e" + "a47e0b0f07" + "b40a000000" + "3f"
	covmObject = "0c00800001"
	covmValue  = "2e" + "2105" + "2f" // value [2] { Unsigned 5 }
)

func TestCOVNotificationMultipleFieldOrder(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	list := func(values string) string { return "4e" + covmObject + "1e" + values + "1f" + "4f" }
	one := list("0955" + covmValue)
	runFieldCases(t, []fieldCase{
		{name: "with timestamp", payload: covmHead + covmStamp + one, accepted: true},
		{name: "without timestamp", payload: covmHead + one, accepted: true},
		{name: "index, value and time of change", payload: covmHead + list("0955"+"1901"+covmValue+"3c0a000000"), accepted: true},
		{name: "process identifier twice", payload: "0902" + covmHead + one},
		{name: "initiating device twice", payload: covmHead + "1c02000002" + one},
		{name: "time remaining twice", payload: covmHead + "2901" + one},
		{name: "timestamp twice", payload: covmHead + covmStamp + covmStamp + one},
		{name: "notification list twice", payload: covmHead + one + one},
		{name: "timestamp of two Unsigned", payload: covmHead + "3e" + "2101" + "2102" + "3f" + one},
		{name: "timestamp with the date only", payload: covmHead + "3e" + "a47e0b0f07" + "3f" + one},
		{name: "timestamp with three values", payload: covmHead + "3e" + "a47e0b0f07" + "b40a000000" + "b40a000000" + "3f" + one},
		{name: "timestamp time before date", payload: covmHead + "3e" + "b40a000000" + "a47e0b0f07" + "3f" + one},
		{name: "second value list for one object", payload: covmHead + "4e" + covmObject + "1e" + "0955" + covmValue + "1f" + "1e" + "094d" + covmValue + "1f" + "4f"},
		{name: "index after the value", payload: covmHead + list("0955"+covmValue+"1901")},
		{name: "index twice", payload: covmHead + list("0955"+"1901"+"1902"+covmValue)},
		{name: "time of change twice", payload: covmHead + list("0955"+covmValue+"3c0a000000"+"3c0b000000")},
		{name: "time of change of two Unsigned", payload: covmHead + list("0955"+covmValue+"3e"+"2101"+"2102"+"3f")},
		// Accepted as before: a notification without its list has no values.
		{name: "no notification list", payload: covmHead, accepted: true},
	}, func(p []byte) error {
		_, err := service.DecodeCOVNotificationMultiple(p, limits)
		return err
	})
}

func TestNotificationParametersFieldTags(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	runFieldCases(t, []fieldCase{
		// change-of-life-safety [8]: new-state [0], new-mode [1], status-flags [2], operation-expected [3].
		{name: "life safety", payload: "8e" + "0901" + "1902" + "2a0400" + "3903" + "8f", accepted: true},
		{name: "life safety, mode and state exchanged", payload: "8e" + "1902" + "0901" + "2a0400" + "3903" + "8f"},
		{name: "life safety, operation tagged [4]", payload: "8e" + "0901" + "1902" + "2a0400" + "4903" + "8f"},
		// command-failure [3]: command-value [0], status-flags [1], feedback-value [2].
		{name: "command failure", payload: "3e" + "0e9101" + "0f" + "1a0400" + "2e9100" + "2f" + "3f", accepted: true},
		{name: "command failure, values exchanged", payload: "3e" + "2e9100" + "2f" + "1a0400" + "0e9101" + "0f" + "3f"},
		// unsigned-range [11]: exceeding-value [0], status-flags [1], exceeded-limit [2].
		{name: "unsigned range", payload: "be" + "0905" + "1a0400" + "2904" + "bf", accepted: true},
		{name: "unsigned range, limit and value exchanged", payload: "be" + "2904" + "1a0400" + "0905" + "bf"},
		// extended [9]: vendor-id [0], extended-event-type [1], parameters [2].
		{name: "extended", payload: "9e" + "0907" + "1901" + "2e" + "2f" + "9f", accepted: true},
		{name: "extended, vendor twice", payload: "9e" + "0907" + "0908" + "1901" + "2e" + "2f" + "9f"},
		{name: "extended, event type twice", payload: "9e" + "0907" + "1901" + "1902" + "2e" + "2f" + "9f"},
		{name: "extended, parameters twice", payload: "9e" + "0907" + "1901" + "2e" + "2f" + "2e" + "2f" + "9f"},
		// buffer-ready [10]: buffer-property [0], previous-notification [1], current-notification [2].
		{name: "buffer ready", payload: "ae" + "0e" + "0c05000001" + "1983" + "0f" + "1901" + "2902" + "af", accepted: true},
		{name: "buffer ready, previous count twice", payload: "ae" + "0e" + "0c05000001" + "1983" + "0f" + "1901" + "1905" + "2902" + "af"},
		{name: "buffer ready, current count twice", payload: "ae" + "0e" + "0c05000001" + "1983" + "0f" + "1901" + "2902" + "2903" + "af"},
		// Accepted as before: absent fields of these two are zero.
		{name: "extended without parameters", payload: "9e" + "0907" + "1901" + "9f", accepted: true},
		{name: "buffer ready without counts", payload: "ae" + "0e" + "0c05000001" + "1983" + "0f" + "af", accepted: true},
	}, func(p []byte) error {
		els, _, err := bacnet.ParseSequence(p, limits, -1)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.DecodeNotificationParameters(els)
		return err
	})
}

func TestGetEnrollmentSummaryFields(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	runFieldCases(t, []fieldCase{
		{name: "filter only", payload: "0900", accepted: true},
		{name: "all filters", payload: "0901" + "2900" + "3905" + "4e" + "0900" + "19ff" + "4f" + "5907", accepted: true},
		{name: "acknowledgment filter twice", payload: "0900" + "0901"},
		{name: "event state filter twice", payload: "0900" + "2900" + "2901"},
		{name: "event type filter twice", payload: "0900" + "3905" + "3906"},
		{name: "priority filter twice", payload: "0900" + "4e" + "0900" + "19ff" + "4f" + "4e" + "0901" + "1902" + "4f"},
		{name: "notification class filter twice", payload: "0900" + "5907" + "5908"},
		{name: "tag [6]", payload: "0900" + "6901"},
		{name: "enrollment filter as a primitive", payload: "0900" + "1901"},
		{name: "event state filter as a constructed value", payload: "0900" + "2e" + "2100" + "2f"},
		// Accepted as before: values above not-acked (2) are served as "all".
		{name: "acknowledgment filter 3", payload: "0903", accepted: true},
	}, func(p []byte) error {
		_, err := service.DecodeGetEnrollmentSummary(p, limits)
		return err
	})
}

// A request field that comes twice is refused; the same request without the
// repetition is accepted.
func TestRequestFieldsRepeated(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	stamp := service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 4}
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	inc := float32(1)
	life := uint32(60)

	ackAlarm := must(service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{
		ProcessIdentifier: 1, EventObject: av, EventStateAcked: 3, TimeStamp: stamp,
		AcknowledgmentSource: bacnet.CharacterString{Value: "op"}, TimeOfAcknowledgment: stamp,
	}))
	subProp := must(service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
		SubscribeCOVRequest: service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: av, IssueConfirmed: true, Lifetime: 60},
		Property:            pv, COVIncrement: &inc,
	}))
	subMulti := must(service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		SubscriberProcessIdentifier: 1, IssueConfirmedNotifications: true, LifetimeRemaining: &life, MaxNotificationDelay: &life,
		Subscriptions: []service.COVMultipleSubscription{{Object: av, Properties: []service.COVPropertyReference{{Property: pv}}}},
	}))
	listEl := must(service.EncodeListElementRequest(service.ListElementRequest{
		Object: av, Property: pv, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}},
	}))
	create := fromHex("0e" + "0902" + "0f" + "1e" + "094d" + "2e" + "7100" + "2f" + "1f")

	for _, tc := range []struct {
		name    string
		valid   []byte
		repeats map[string]string // name -> hex appended to the valid request
		decode  func([]byte) error
	}{
		{"AcknowledgeAlarm", ackAlarm, map[string]string{
			"process identifier": "0902", "event object": "1c00800002", "event state": "2900",
			"time stamp": "3e" + "1905" + "3f", "source": "4b006f70", "time of acknowledgment": "5e" + "1905" + "5f",
		}, func(p []byte) error { _, err := service.DecodeAcknowledgeAlarm(p, limits); return err }},
		{"SubscribeCOVProperty", subProp, map[string]string{
			"process identifier": "0902", "COV increment": "5c40000000",
		}, func(p []byte) error { _, err := service.DecodeSubscribeCOVProperty(p, limits); return err }},
		{"SubscribeCOVPropertyMultiple", subMulti, map[string]string{
			"process identifier": "0902", "issue confirmed": "1900", "lifetime": "2901", "notification delay": "3901",
		}, func(p []byte) error { _, err := service.DecodeSubscribeCOVPropertyMultiple(p, limits); return err }},
		{"ListElement", listEl, map[string]string{
			"object": "0c00800002", "property": "194d", "array index": "2901" + "2902", "list": "3e" + "2102" + "3f",
		}, func(p []byte) error { _, err := service.DecodeListElementRequest(p, limits); return err }},
		{"CreateObject", create, map[string]string{
			"initial values": "1e" + "094d" + "2e" + "7100" + "2f" + "1f",
		}, func(p []byte) error { _, err := service.DecodeCreateObject(p, limits); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.decode(tc.valid); err != nil {
				t.Fatalf("valid request refused: %v", err)
			}
			for name, extra := range tc.repeats {
				p := append(append([]byte(nil), tc.valid...), fromHex(extra)...)
				if err := tc.decode(p); !errors.Is(err, bacnet.ErrMalformed) {
					t.Errorf("%s twice: %v, want an error that wraps ErrMalformed", name, err)
				}
			}
		})
	}
}
