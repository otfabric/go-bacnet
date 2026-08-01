// SPDX-License-Identifier: MIT

package fixtures_test

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/fixtures"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestInteropCorpusDecodes(t *testing.T) {
	all, err := fixtures.LoadAll()
	if err != nil {
		if os.Getenv("BACNET_INTEROP_REQUIRED") != "" {
			t.Fatal(err)
		}
		t.Skip(err.Error())
	}
	if len(all) == 0 {
		t.Fatal("expected fixtures in bacnet-interop manifest")
	}
	limits := bacnet.DefaultDecodeLimits()
	for _, meta := range all {
		meta := meta
		t.Run(meta.ID, func(t *testing.T) {
			raw, err := meta.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			if meta.Operation == "" {
				t.Fatalf("fixture %s missing operation (legacy tag dispatch removed)", meta.ID)
			}
			assertOperation(t, meta, raw, limits)
		})
	}
}

func assertOperation(t *testing.T, meta fixtures.Meta, raw []byte, limits bacnet.DecodeLimits) {
	t.Helper()
	switch meta.Operation {
	case "decode_bvlc":
		msg, err := bvlc.Parse(raw, limits)
		assertExpectedError(t, meta, err, "bvlc")
		if err != nil {
			return
		}
		assertExpectedBVLC(t, meta, msg)
		assertReencode(t, meta, raw, func() ([]byte, error) { return bvlc.Append(nil, msg) })

	case "decode_tag":
		_, _, err := bacnet.ParseTag(raw, limits)
		assertExpectedError(t, meta, err, "tag")

	case "decode_npdu":
		n, consumed, err := npdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "npdu")
		if err != nil {
			return
		}
		if consumed != len(raw) {
			t.Fatalf("consumed %d of %d", consumed, len(raw))
		}
		assertExpectedNPDU(t, meta, n)
		assertReencode(t, meta, raw, func() ([]byte, error) { return npdu.Append(nil, n) })

	case "decode_reject":
		pdu, err := apdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "apdu")
		if err != nil {
			return
		}
		if pdu.Reject == nil {
			t.Fatal("want Reject PDU")
		}
		want := meta.Expected
		if int(pdu.Reject.InvokeID) != intNum(want["invoke_id"]) || int(pdu.Reject.Reason) != intNum(want["reason"]) {
			t.Fatalf("reject=%+v", pdu.Reject)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return apdu.AppendReject(nil, *pdu.Reject), nil
		})

	case "decode_abort":
		pdu, err := apdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "apdu")
		if err != nil {
			return
		}
		if pdu.Abort == nil {
			t.Fatal("want Abort PDU")
		}
		want := meta.Expected
		if int(pdu.Abort.InvokeID) != intNum(want["invoke_id"]) || int(pdu.Abort.Reason) != intNum(want["reason"]) {
			t.Fatalf("abort=%+v", pdu.Abort)
		}
		if boolVal(want["server"]) != pdu.Abort.Server {
			t.Fatalf("server=%v", pdu.Abort.Server)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return apdu.AppendAbort(nil, *pdu.Abort), nil
		})

	case "decode_segment_ack":
		pdu, err := apdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "apdu")
		if err != nil {
			return
		}
		if pdu.SegmentACK == nil {
			t.Fatal("want SegmentACK")
		}
		want := meta.Expected
		ack := pdu.SegmentACK
		if int(ack.InvokeID) != intNum(want["invoke_id"]) ||
			int(ack.SequenceNumber) != intNum(want["sequence_number"]) ||
			int(ack.ActualWindowSize) != intNum(want["window_size"]) {
			t.Fatalf("segment-ack=%+v", ack)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return apdu.AppendSegmentACK(nil, *ack), nil
		})

	case "decode_complex_ack":
		pdu, err := apdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "apdu")
		if err != nil {
			return
		}
		if pdu.ComplexACK == nil {
			t.Fatal("want ComplexACK")
		}
		want := meta.Expected
		ack := pdu.ComplexACK
		if int(ack.InvokeID) != intNum(want["invoke_id"]) || int(ack.ServiceChoice) != intNum(want["service"]) {
			t.Fatalf("complex-ack=%+v", ack)
		}
		if boolVal(want["segmented"]) != ack.SegmentedMessage {
			t.Fatalf("segmented=%v", ack.SegmentedMessage)
		}
		if seq, ok := want["sequence_number"]; ok && int(ack.SequenceNumber) != intNum(seq) {
			t.Fatalf("sequence=%d", ack.SequenceNumber)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return apdu.AppendComplexACK(nil, *ack), nil
		})

	case "decode_read_property_ack":
		pdu, err := apdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "apdu")
		if err != nil {
			return
		}
		if pdu.Type != apdu.TypeComplexACK || pdu.ComplexACK == nil {
			t.Fatalf("want ComplexACK, got %v", pdu.Type)
		}
		ack, err := service.DecodeReadPropertyACK(pdu.ComplexACK.Payload, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(pdu.ComplexACK.InvokeID) != intNum(want["invoke_id"]) {
			t.Fatalf("invoke_id=%d", pdu.ComplexACK.InvokeID)
		}
		obj := want["object"].(map[string]any)
		if int(ack.Object.Type) != intNum(obj["type"]) || int(ack.Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", ack.Object)
		}
		if int(ack.Property.Identifier) != intNum(want["property"]) {
			t.Fatalf("property=%d", ack.Property.Identifier)
		}
		val := want["value"].(map[string]any)
		if val["kind"] == "real" {
			f, err := bacnet.AsReal(ack.Value)
			if err != nil {
				t.Fatal(err)
			}
			if math.Abs(float64(f)-floatNum(val["value"])) > 0.01 {
				t.Fatalf("real=%v want %v", f, val["value"])
			}
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return apdu.AppendComplexACK(nil, *pdu.ComplexACK), nil
		})

	case "decode_read_property":
		pdu, err := apdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "apdu")
		if err != nil {
			return
		}
		if pdu.ConfirmedRequest == nil {
			t.Fatal("want confirmed request")
		}
		req := pdu.ConfirmedRequest
		rp, err := service.DecodeReadProperty(req.Payload, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(req.InvokeID) != intNum(want["invoke_id"]) {
			t.Fatalf("invoke_id=%d", req.InvokeID)
		}
		if size, ok := apdu.DecodeMaxAPDUSize(apdu.MaxAPDUCode(req.MaxAPDU)); !ok || int(size) != intNum(want["max_apdu"]) {
			t.Fatalf("max_apdu code=%d size=%d", req.MaxAPDU, size)
		}
		if int(req.MaxSegments) != int(apdu.EncodeMaxSegments(intNum(want["max_segments"]))) {
			t.Fatalf("max_segments code=%d want encode(%d)", req.MaxSegments, intNum(want["max_segments"]))
		}
		obj := want["object"].(map[string]any)
		if int(rp.Object.Type) != intNum(obj["type"]) || int(rp.Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", rp.Object)
		}
		if int(rp.Property.Identifier) != intNum(want["property"]) {
			t.Fatalf("property=%d", rp.Property.Identifier)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return apdu.AppendConfirmedRequest(nil, *req), nil
		})

	case "decode_error":
		pdu, err := apdu.Parse(raw, limits)
		assertExpectedError(t, meta, err, "apdu")
		if err != nil {
			return
		}
		if pdu.Error == nil {
			t.Fatal("want Error PDU")
		}
		class, code, err := apdu.DecodeErrorClassCode(pdu.Error.Payload, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(pdu.Error.InvokeID) != intNum(want["invoke_id"]) {
			t.Fatalf("invoke_id=%d", pdu.Error.InvokeID)
		}
		if int(pdu.Error.ServiceChoice) != intNum(want["service"]) {
			t.Fatalf("service=%d", pdu.Error.ServiceChoice)
		}
		if int(class) != intNum(want["error_class"]) || int(code) != intNum(want["error_code"]) {
			t.Fatalf("class=%d code=%d", class, code)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return apdu.AppendError(nil, *pdu.Error), nil
		})

	case "decode_write_property":
		req, err := service.DecodeWriteProperty(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		obj := want["object"].(map[string]any)
		if int(req.Object.Type) != intNum(obj["type"]) || int(req.Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", req.Object)
		}
		if int(req.Property.Identifier) != intNum(want["property"]) {
			t.Fatalf("property=%d", req.Property.Identifier)
		}
		if req.Priority == nil || int(*req.Priority) != intNum(want["priority"]) {
			t.Fatalf("priority=%v", req.Priority)
		}
		if req.Value.Kind != bacnet.ValueNull {
			t.Fatalf("value kind=%d want Null", req.Value.Kind)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeWriteProperty(req) })

	case "decode_subscribe_cov":
		req, err := service.DecodeSubscribeCOV(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(req.ProcessIdentifier) != intNum(want["process_identifier"]) {
			t.Fatalf("pid=%d", req.ProcessIdentifier)
		}
		obj := want["object"].(map[string]any)
		if int(req.MonitoredObject.Type) != intNum(obj["type"]) || int(req.MonitoredObject.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", req.MonitoredObject)
		}
		if boolVal(want["issue_confirmed"]) != req.IssueConfirmed {
			t.Fatalf("confirmed=%v", req.IssueConfirmed)
		}
		if int(req.Lifetime) != intNum(want["lifetime"]) {
			t.Fatalf("lifetime=%d", req.Lifetime)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeSubscribeCOV(req) })

	case "decode_subscribe_cov_property":
		req, err := service.DecodeSubscribeCOVProperty(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(req.ProcessIdentifier) != intNum(want["process_identifier"]) {
			t.Fatalf("pid=%d", req.ProcessIdentifier)
		}
		obj := want["object"].(map[string]any)
		if int(req.MonitoredObject.Type) != intNum(obj["type"]) || int(req.MonitoredObject.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", req.MonitoredObject)
		}
		if !req.Cancellation {
			t.Fatal("want cancellation")
		}
		if int(req.Property.Identifier) != intNum(want["property"]) {
			t.Fatalf("property=%d", req.Property.Identifier)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeSubscribeCOVProperty(req) })

	case "decode_rpm_ack":
		ack, err := service.DecodeReadPropertyMultipleACK(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if len(ack) != intNum(want["result_count"]) {
			t.Fatalf("results=%d", len(ack))
		}
		var er *bacnet.ErrorResponse
		for _, pr := range ack[0].Properties {
			if pr.Err != nil && errors.As(pr.Err, &er) {
				break
			}
		}
		if er == nil {
			t.Fatal("expected a property-level ErrorResponse")
		}
		if int(er.Class) != intNum(want["error_class"]) || int(er.Code) != intNum(want["error_code"]) {
			t.Fatalf("err=%+v", er)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeReadPropertyMultipleACK(ack) })

	case "decode_write_property_multiple":
		specs, err := service.DecodeWritePropertyMultiple(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if len(specs) != intNum(want["spec_count"]) {
			t.Fatalf("specs=%d", len(specs))
		}
		obj := want["object"].(map[string]any)
		if int(specs[0].Object.Type) != intNum(obj["type"]) || int(specs[0].Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", specs[0].Object)
		}
		if int(specs[0].Properties[0].Property.Identifier) != intNum(want["property"]) {
			t.Fatalf("property=%d", specs[0].Properties[0].Property.Identifier)
		}
		if specs[0].Properties[0].Priority == nil || int(*specs[0].Properties[0].Priority) != intNum(want["priority"]) {
			t.Fatalf("priority=%v", specs[0].Properties[0].Priority)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeWritePropertyMultiple(specs) })

	case "decode_write_property_multiple_error":
		wpmErr, err := service.DecodeWritePropertyMultipleError(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(wpmErr.Class) != intNum(want["error_class"]) || int(wpmErr.Code) != intNum(want["error_code"]) {
			t.Fatalf("class/code=%d/%d", wpmErr.Class, wpmErr.Code)
		}
		obj := want["object"].(map[string]any)
		if int(wpmErr.FirstFailed.Type) != intNum(obj["type"]) || int(wpmErr.FirstFailed.Instance) != intNum(obj["instance"]) {
			t.Fatalf("firstFailed=%v", wpmErr.FirstFailed)
		}
		if int(wpmErr.FirstProperty.Identifier) != intNum(want["property"]) {
			t.Fatalf("property=%d", wpmErr.FirstProperty.Identifier)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) {
			return service.EncodeWritePropertyMultipleError(*wpmErr)
		})

	case "decode_who_has":
		req, err := service.DecodeWhoHas(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if name, ok := want["name"].(string); ok {
			if req.Name == nil || req.Name.Value != name {
				t.Fatalf("name=%v", req.Name)
			}
		}
		if objRaw, ok := want["object"].(map[string]any); ok {
			if req.Object == nil || int(req.Object.Type) != intNum(objRaw["type"]) || int(req.Object.Instance) != intNum(objRaw["instance"]) {
				t.Fatalf("object=%v", req.Object)
			}
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeWhoHas(req) })

	case "decode_i_have":
		msg, err := service.DecodeIHave(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		dev := want["device"].(map[string]any)
		obj := want["object"].(map[string]any)
		if int(msg.Device.Type) != intNum(dev["type"]) || int(msg.Device.Instance) != intNum(dev["instance"]) {
			t.Fatalf("device=%v", msg.Device)
		}
		if int(msg.Object.Type) != intNum(obj["type"]) || int(msg.Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", msg.Object)
		}
		if msg.Name.Value != want["name"].(string) {
			t.Fatalf("name=%q", msg.Name.Value)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeIHave(msg) })

	case "decode_read_range":
		req, err := service.DecodeReadRange(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		obj := want["object"].(map[string]any)
		if int(req.Object.Type) != intNum(obj["type"]) || int(req.Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", req.Object)
		}
		if int(req.Property.Identifier) != intNum(want["property"]) {
			t.Fatalf("property=%d", req.Property.Identifier)
		}
		if want["by"].(string) == "byPosition" && req.By != service.ReadRangeByPosition {
			t.Fatalf("by=%v", req.By)
		}
		if int(req.ReferenceIndex) != intNum(want["reference_index"]) || int(req.Count) != intNum(want["count"]) {
			t.Fatalf("range index=%d count=%d", req.ReferenceIndex, req.Count)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeReadRange(req) })

	case "decode_read_range_ack":
		ack, err := service.DecodeReadRangeACK(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		obj := want["object"].(map[string]any)
		if int(ack.Object.Type) != intNum(obj["type"]) || int(ack.Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", ack.Object)
		}
		if int(ack.ItemCount) != intNum(want["item_count"]) {
			t.Fatalf("itemCount=%d", ack.ItemCount)
		}
		if ack.FirstItem() != boolVal(want["first_item"]) || ack.LastItem() != boolVal(want["last_item"]) || ack.MoreItems() != boolVal(want["more_items"]) {
			t.Fatalf("flags first=%v last=%v more=%v", ack.FirstItem(), ack.LastItem(), ack.MoreItems())
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeReadRangeACK(ack) })

	case "decode_event_notification":
		note, err := service.DecodeEventNotification(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(note.ProcessIdentifier) != intNum(want["process_identifier"]) || int(note.Priority) != intNum(want["priority"]) || int(note.ToState) != intNum(want["to_state"]) {
			t.Fatalf("note=%+v", note)
		}
		dev := want["device"].(map[string]any)
		obj := want["object"].(map[string]any)
		if int(note.InitiatingDevice.Type) != intNum(dev["type"]) || int(note.InitiatingDevice.Instance) != intNum(dev["instance"]) {
			t.Fatalf("device=%v", note.InitiatingDevice)
		}
		if int(note.EventObject.Type) != intNum(obj["type"]) || int(note.EventObject.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", note.EventObject)
		}
		if note.TimeStamp.Choice != service.TimeStampSequence || int(note.TimeStamp.Sequence) != intNum(want["sequence"]) {
			t.Fatalf("ts=%+v", note.TimeStamp)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeEventNotification(note) })

	case "decode_acknowledge_alarm":
		req, err := service.DecodeAcknowledgeAlarm(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		obj := want["object"].(map[string]any)
		if int(req.ProcessIdentifier) != intNum(want["process_identifier"]) || int(req.EventStateAcked) != intNum(want["event_state_acked"]) {
			t.Fatalf("%+v", req)
		}
		if int(req.EventObject.Type) != intNum(obj["type"]) || int(req.EventObject.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", req.EventObject)
		}
		if req.AcknowledgmentSource.Value != want["source"].(string) {
			t.Fatalf("source=%q", req.AcknowledgmentSource.Value)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeAcknowledgeAlarm(req) })

	case "decode_get_event_information":
		req, err := service.DecodeGetEventInformation(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		obj := want["last_received"].(map[string]any)
		if req.LastReceived == nil || int(req.LastReceived.Type) != intNum(obj["type"]) || int(req.LastReceived.Instance) != intNum(obj["instance"]) {
			t.Fatalf("%+v", req)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeGetEventInformation(req) })

	case "decode_get_event_information_ack":
		ack, err := service.DecodeGetEventInformationACK(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if len(ack.Summaries) != intNum(want["summary_count"]) || ack.MoreEvents != boolVal(want["more_events"]) {
			t.Fatalf("%+v", ack)
		}
		obj := want["object"].(map[string]any)
		if int(ack.Summaries[0].Object.Type) != intNum(obj["type"]) || int(ack.Summaries[0].Object.Instance) != intNum(obj["instance"]) {
			t.Fatalf("object=%v", ack.Summaries[0].Object)
		}
		if int(ack.Summaries[0].EventState) != intNum(want["event_state"]) {
			t.Fatalf("state=%d", ack.Summaries[0].EventState)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeGetEventInformationACK(ack) })

	case "decode_device_communication_control":
		req, err := service.DecodeDeviceCommunicationControl(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(req.EnableDisable) != intNum(want["enable_disable"]) {
			t.Fatalf("%+v", req)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeDeviceCommunicationControl(req) })

	case "decode_reinitialize_device":
		req, err := service.DecodeReinitializeDevice(raw, limits)
		assertExpectedError(t, meta, err, "service")
		if err != nil {
			return
		}
		want := meta.Expected
		if int(req.State) != intNum(want["state"]) {
			t.Fatalf("%+v", req)
		}
		assertReencode(t, meta, raw, func() ([]byte, error) { return service.EncodeReinitializeDevice(req) })

	default:
		t.Fatalf("unsupported operation %q", meta.Operation)
	}
}

func assertExpectedBVLC(t *testing.T, meta fixtures.Meta, msg bvlc.Message) {
	t.Helper()
	if meta.Expected == nil {
		return
	}
	want := meta.Expected
	if int(msg.Function) != intNum(want["function"]) {
		t.Fatalf("function=%d", msg.Function)
	}
	if ttl, ok := want["ttl"]; ok && int(msg.TTL) != intNum(ttl) {
		t.Fatalf("ttl=%d", msg.TTL)
	}
	if origin, ok := want["origin_ip"].(string); ok {
		got := fmt.Sprintf("%d.%d.%d.%d", msg.OriginIP[0], msg.OriginIP[1], msg.OriginIP[2], msg.OriginIP[3])
		if got != origin {
			t.Fatalf("origin_ip=%s", got)
		}
	}
	if port, ok := want["origin_port"]; ok && int(msg.OriginPort) != intNum(port) {
		t.Fatalf("origin_port=%d", msg.OriginPort)
	}
	if payloadLen, ok := want["payload_len"]; ok && len(msg.Payload) != intNum(payloadLen) {
		t.Fatalf("payload_len=%d", len(msg.Payload))
	}
}

func assertExpectedNPDU(t *testing.T, meta fixtures.Meta, n npdu.NPDU) {
	t.Helper()
	if meta.Expected == nil {
		return
	}
	want := meta.Expected
	if boolVal(want["expecting_reply"]) != n.ExpectingReply {
		t.Fatalf("expecting_reply=%v", n.ExpectingReply)
	}
	if dest, ok := want["destination_network"]; ok {
		if n.Destination.Scope() != bacnet.AddressRemoteStation && n.Destination.Scope() != bacnet.AddressRemoteBroadcast {
			t.Fatalf("destination scope=%v", n.Destination.Scope())
		}
		if int(n.Destination.Network()) != intNum(dest) {
			t.Fatalf("dnet=%d", n.Destination.Network())
		}
	}
	if hop, ok := want["hop_count"]; ok && int(n.HopCount) != intNum(hop) {
		t.Fatalf("hop=%d", n.HopCount)
	}
	if len(n.APDU) == 0 {
		t.Fatal("expected APDU payload")
	}
}

func assertReencode(t *testing.T, meta fixtures.Meta, raw []byte, encode func() ([]byte, error)) {
	t.Helper()
	if !meta.Expect.DeterministicReencodeEqual && !meta.Expect.OriginalBytesEqual {
		return
	}
	enc, err := encode()
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic reencode without a separate expected_reencode_hex means
	// encode(decode(input)) must equal input_hex.
	if meta.Expect.DeterministicReencodeEqual || meta.Expect.OriginalBytesEqual {
		if !bytes.Equal(enc, raw) {
			t.Fatalf("reencode\n got %x\nwant %x", enc, raw)
		}
	}
}

// layerOrder ranks fixture decode stages for multi-stage operations
// (e.g. APDU framing then service payload). Unknown layers sort last.
func layerOrder(layer string) int {
	switch layer {
	case "tag", "bvlc":
		return 1
	case "npdu":
		return 2
	case "apdu":
		return 3
	case "service":
		return 4
	default:
		return 99
	}
}

// assertExpectedError validates error expectations across multi-stage parsers.
//
//	current before expected layer: require err == nil and continue
//	current equals expected layer: require error and validate category
//	current after expected layer: fail (expected layer was missed)
func assertExpectedError(t *testing.T, meta fixtures.Meta, err error, layer string) {
	t.Helper()
	if meta.ExpectedError == nil {
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	wantLayer := meta.ExpectedError.Layer
	switch {
	case wantLayer != "" && layer != wantLayer && layerOrder(layer) < layerOrder(wantLayer):
		if err != nil {
			t.Fatalf("unexpected error at layer %q (want failure at %q): %v", layer, wantLayer, err)
		}
		return
	case wantLayer != "" && layer != wantLayer && layerOrder(layer) > layerOrder(wantLayer):
		t.Fatalf("missed expected error at layer %q (now at %q, err=%v)", wantLayer, layer, err)
	case wantLayer != "" && layer != wantLayer:
		// Same rank, different name (e.g. tag vs bvlc).
		t.Fatalf("error layer=%q want %q (err=%v)", layer, wantLayer, err)
	}
	if err == nil {
		t.Fatal("expected error")
	}
	switch meta.ExpectedError.Category {
	case "malformed":
		if !errors.Is(err, bacnet.ErrMalformed) && !errors.Is(err, bacnet.ErrTrailingData) && !errors.Is(err, bacnet.ErrLimitExceeded) {
			t.Fatalf("expected malformed category, got %v", err)
		}
	case "unsupported":
		if !errors.Is(err, bacnet.ErrUnsupported) {
			t.Fatalf("expected unsupported category, got %v", err)
		}
	case "protocol":
		if !errors.Is(err, bacnet.ErrProtocolViolation) {
			t.Fatalf("expected protocol category, got %v", err)
		}
	default:
		t.Fatalf("unknown expected_error.category %q", meta.ExpectedError.Category)
	}
}

func intNum(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func floatNum(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

func boolVal(v any) bool {
	b, _ := v.(bool)
	return b
}
