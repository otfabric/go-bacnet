//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// Since bacnet-interop 0.10.0 the BACnet4J and Worldiety adapters seed
// trend-log:0 with the same fixed records. Record k (1-based position) has
//
//	time stamp       2024-01-01 00:00:00 + (k-1) × 15 minutes
//	logDatum         real-value 20 + (k-1)
//	sequence number  k
//
// with Record_Count = Total_Record_Count = 36. BACnet4J adds statusFlags (all
// clear) and the day of week; the Worldiety adapter encodes neither.
//
// The range selection on BACnet4J is upstream code (ReadRangeRequest.handle on
// a TrendLogObject buffer); the adapter only supplies the records. On
// Worldiety the selection is adapter code over the upstream stack's transport
// and tag codec: it says that go-bacnet's request is decodable by a second
// implementation, nothing about a second stack's ReadRange semantics.
const (
	seededLogRecords  = 36
	seededLogInterval = 15 * time.Minute
)

var seededLogStart = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)

// seededLogTime is the time stamp of the record at 1-based position pos.
func seededLogTime(pos int) time.Time {
	return seededLogStart.Add(time.Duration(pos-1) * seededLogInterval)
}

// seededLogPeer says how a peer encodes the seeded records.
type seededLogPeer struct {
	peer        interopPeer
	statusFlags bool // every record carries statusFlags, four clear bits
	weekday     bool // the record date carries the day of week
}

func bacnet4jSeededLog() seededLogPeer {
	return seededLogPeer{peer: bacnet4jPeer(), statusFlags: true, weekday: true}
}

func worldietySeededLog() seededLogPeer {
	return seededLogPeer{peer: worldietyPeer()}
}

func (p seededLogPeer) requireRecords(t *testing.T, what string, records []service.LogRecord, firstPos int) {
	t.Helper()
	for i, rec := range records {
		pos := firstPos + i
		want := bacnetDateTime(seededLogTime(pos))
		if !p.weekday {
			want.Date.Weekday = 255
		}
		if rec.Timestamp != want {
			t.Fatalf("%s: record %d (position %d) time stamp %+v, want %+v", what, i, pos, rec.Timestamp, want)
		}
		if rec.DatumChoice != service.LogDatumReal || rec.Datum.Kind != bacnet.ValueReal {
			t.Fatalf("%s: record %d logDatum choice %d kind %v, want real-value", what, i, rec.DatumChoice, rec.Datum.Kind)
		}
		if rec.Datum.Real != float32(20+pos-1) {
			t.Fatalf("%s: record %d (position %d) value %v, want %v", what, i, pos, rec.Datum.Real, float32(20+pos-1))
		}
		switch {
		case !p.statusFlags:
			if rec.StatusFlags != nil {
				t.Fatalf("%s: record %d has statusFlags %+v, the peer seeds none", what, i, *rec.StatusFlags)
			}
		case rec.StatusFlags == nil:
			t.Fatalf("%s: record %d has no statusFlags", what, i)
		case rec.StatusFlags.UnusedBits != 4 || len(rec.StatusFlags.Bytes) != 1 || rec.StatusFlags.Bytes[0] != 0:
			t.Fatalf("%s: record %d statusFlags %+v, want four clear bits", what, i, *rec.StatusFlags)
		}
	}
}

// requireRange checks one ReadRange reply against the seeded log. The
// sequence number of the record at position pos is pos.
func (p seededLogPeer) requireRange(t *testing.T, what string, ack service.ReadRangeACK, want rangeExpect) {
	t.Helper()
	if int(ack.ItemCount) != want.items || len(ack.LogRecords) != want.items {
		t.Fatalf("%s: itemCount %d with %d decoded records, want %d", what, ack.ItemCount, len(ack.LogRecords), want.items)
	}
	if ack.FirstItem() != want.first || ack.LastItem() != want.last || ack.MoreItems() != want.more {
		t.Fatalf("%s: result flags first=%v last=%v more=%v, want first=%v last=%v more=%v", what,
			ack.FirstItem(), ack.LastItem(), ack.MoreItems(), want.first, want.last, want.more)
	}
	if want.items == 0 {
		if ack.FirstSequence != nil {
			t.Fatalf("%s: firstSequenceNumber %d in an empty reply", what, *ack.FirstSequence)
		}
		return
	}
	if ack.FirstSequence == nil {
		t.Fatalf("%s: no firstSequenceNumber in a reply with %d records", what, ack.ItemCount)
	}
	if *ack.FirstSequence != uint32(want.firstPos) {
		t.Fatalf("%s: firstSequenceNumber %d, want %d", what, *ack.FirstSequence, want.firstPos)
	}
	p.requireRecords(t, what, ack.LogRecords, want.firstPos)
}

// start starts the peer and checks the log is the seeded one.
func (p seededLogPeer) start(t *testing.T) (*client.Client, *peerHandle, bacnet.ObjectIdentifier) {
	t.Helper()
	requireAdapterVersion(t, p.peer.image, "0.10.0")
	dev := loadDeviceFixture(t)
	tl, ok := trendLogObject(dev)
	if !ok {
		failOrSkip(t, "device fixture has no trend-log (need device-baseline-v2)")
	}
	peer := startPeer(t, p.peer.image, p.peer.name)
	if peer.assertedByReexec {
		return nil, peer, tl
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	for _, prop := range []bacnet.PropertyIdentifier{propertyRecordCount, propertyTotalRecordCount} {
		v, err := c.ReadProperty(ctx, peer.target, tl, bacnet.PropertyReference{Identifier: prop})
		if err != nil {
			t.Fatalf("ReadProperty %v property %d: %v", tl, prop, err)
		}
		got, err := bacnet.AsUnsigned(v)
		if err != nil {
			t.Fatal(err)
		}
		if got != seededLogRecords {
			t.Fatalf("%v property %d = %d, want %d: the log is not the documented seed", tl, prop, got, seededLogRecords)
		}
	}
	return c, peer, tl
}

func (p seededLogPeer) runBySequenceNumber(t *testing.T) {
	c, peer, tl := p.start(t)
	if peer.assertedByReexec {
		return
	}
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	read := func(ref uint32, count int32) service.ReadRangeACK {
		t.Helper()
		ack, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
			Object:         tl,
			Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
			By:             service.ReadRangeBySequenceNumber,
			ReferenceIndex: ref,
			Count:          count,
		})
		if err != nil {
			t.Fatalf("ReadRange bySequenceNumber ref=%d count=%d: %v", ref, count, err)
		}
		if ack.Object != tl || ack.Property.Identifier != bacnet.PropertyLogBuffer {
			t.Fatalf("ACK is for %v property %d", ack.Object, ack.Property.Identifier)
		}
		return ack
	}

	p.requireRange(t, "first five", read(1, 5), rangeExpect{items: 5, firstPos: 1, first: true})
	p.requireRange(t, "three from the middle", read(20, 3), rangeExpect{items: 3, firstPos: 20})
	// The range runs past the newest record: truncated, LAST_ITEM set.
	p.requireRange(t, "ten at the end", read(seededLogRecords-1, 10),
		rangeExpect{items: 2, firstPos: seededLogRecords - 1, last: true})
	// A negative count selects the records up to and including the reference.
	p.requireRange(t, "four back from the newest", read(seededLogRecords, -4),
		rangeExpect{items: 4, firstPos: seededLogRecords - 3, last: true})
	p.requireRange(t, "ten back from the third", read(3, -10), rangeExpect{items: 3, firstPos: 1, first: true})
	p.requireRange(t, "two back from the middle", read(20, -2), rangeExpect{items: 2, firstPos: 19})
	// A reference sequence number that is not in the buffer selects nothing.
	// (bacnet-stack moves a reference before the oldest record up to it:
	// TestBacnetStackReadRangeBySequenceNumber.)
	p.requireRange(t, "before the oldest", read(0, 4), rangeExpect{})
	p.requireRange(t, "past the newest", read(seededLogRecords+1, 5), rangeExpect{})
	p.requireRange(t, "past the newest, backwards", read(seededLogRecords+1, -5), rangeExpect{})
	// More than the log holds: everything in one reply, FIRST_ITEM and
	// LAST_ITEM, MORE_ITEMS clear.
	p.requireRange(t, "500 from the oldest", read(1, 500),
		rangeExpect{items: seededLogRecords, firstPos: 1, first: true, last: true})
}

func (p seededLogPeer) runByTime(t *testing.T) {
	c, peer, tl := p.start(t)
	if peer.assertedByReexec {
		return
	}
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	request := func(ref service.DateTime, count int32) (service.ReadRangeACK, error) {
		return c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
			Object:        tl,
			Property:      bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
			By:            service.ReadRangeByTime,
			ReferenceTime: ref,
			Count:         count,
		})
	}
	read := func(ref time.Time, count int32) service.ReadRangeACK {
		t.Helper()
		ack, err := request(bacnetDateTime(ref), count)
		if err != nil {
			t.Fatalf("ReadRange byTime ref=%s count=%d: %v", ref.Format(time.DateTime), count, err)
		}
		if ack.Object != tl || ack.Property.Identifier != bacnet.PropertyLogBuffer {
			t.Fatalf("ACK is for %v property %d", ack.Object, ack.Property.Identifier)
		}
		return ack
	}
	last := seededLogTime(seededLogRecords) // 2024-01-01 08:45:00

	// A positive count returns the records after the reference time.
	p.requireRange(t, "before the oldest", read(seededLogStart.Add(-time.Second), 3),
		rangeExpect{items: 3, firstPos: 1, first: true})
	// The record stamped exactly at the reference time is not "after" it.
	p.requireRange(t, "at the oldest", read(seededLogStart, 4), rangeExpect{items: 4, firstPos: 2})
	p.requireRange(t, "between two records", read(seededLogTime(20).Add(7*time.Minute), 2),
		rangeExpect{items: 2, firstPos: 21})
	p.requireRange(t, "running into the end", read(last.Add(-45*time.Minute), 10),
		rangeExpect{items: 3, firstPos: seededLogRecords - 2, last: true})
	p.requireRange(t, "at the newest", read(last, 5), rangeExpect{})
	p.requireRange(t, "after the newest", read(last.Add(time.Hour), 5), rangeExpect{})
	// A negative count returns the records before the reference time.
	p.requireRange(t, "two before 01:00", read(seededLogStart.Add(time.Hour), -2),
		rangeExpect{items: 2, firstPos: 3})
	p.requireRange(t, "more before 00:30 than exist", read(seededLogStart.Add(30*time.Minute), -5),
		rangeExpect{items: 2, firstPos: 1, first: true})
	p.requireRange(t, "three before the end of the log", read(last.Add(time.Hour), -3),
		rangeExpect{items: 3, firstPos: seededLogRecords - 2, last: true})
	p.requireRange(t, "before the oldest, backwards", read(seededLogStart, -3), rangeExpect{})
	p.requireRange(t, "everything after a second before the oldest", read(seededLogStart.Add(-time.Second), 500),
		rangeExpect{items: seededLogRecords, firstPos: 1, first: true, last: true})

	// A reference time with an unspecified field is refused.
	wild := bacnetDateTime(seededLogStart)
	wild.Time.Hundredths = 255
	_, err := request(wild, 2)
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) || er.Class != bacnet.ErrorClassProperty || er.Code != bacnet.ErrorCodeDatatypeNotSupported {
		t.Fatalf("reference time with unspecified hundredths: %v, want Error property/datatype-not-supported", err)
	}
}

// ReadRangeAll pages the log by sequence number to LAST_ITEM. Every page but
// the last holds exactly the count asked for and has MORE_ITEMS clear, so the
// pager must go on until LAST_ITEM.
func (p seededLogPeer) runAllBySequenceNumber(t *testing.T) {
	c, peer, tl := p.start(t)
	if peer.assertedByReexec {
		return
	}
	ctx, cancel := withTimeout(t, 60*time.Second)
	defer cancel()
	req := service.ReadRangeRequest{
		Object:         tl,
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeBySequenceNumber,
		ReferenceIndex: 1,
	}
	for _, tc := range []struct {
		name      string
		pageCount int32
		pages     int
	}{
		{"pages of 10", 10, 4},
		{"pages of 12", 12, 3}, // the last page is full and carries LAST_ITEM
		{"pages of 1", 1, seededLogRecords},
		{"one page of 500", 500, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.ReadRangeAll(ctx, peer.target, req, client.ReadRangePageOptions{PageCount: tc.pageCount})
			if err != nil {
				t.Fatalf("ReadRangeAll: %v (stopped: %q after %d pages)", err, res.StoppedReason, len(res.Pages))
			}
			if res.Partial {
				t.Fatalf("ReadRangeAll stopped early: %q after %d pages", res.StoppedReason, len(res.Pages))
			}
			if len(res.LogRecords) != seededLogRecords || len(res.Pages) != tc.pages {
				t.Fatalf("%d records in %d pages, want %d records in %d pages",
					len(res.LogRecords), len(res.Pages), seededLogRecords, tc.pages)
			}
			p.requireRecords(t, tc.name, res.LogRecords, 1)
			next := uint32(1)
			for i, page := range res.Pages {
				lastPage := i == len(res.Pages)-1
				if page.FirstSequence == nil || *page.FirstSequence != next {
					t.Fatalf("page %d firstSequenceNumber %v, want %d (pages must be contiguous)", i, page.FirstSequence, next)
				}
				next += page.ItemCount
				if page.FirstItem() != (i == 0) || page.LastItem() != lastPage || page.MoreItems() {
					t.Fatalf("page %d of %d: first=%v last=%v more=%v", i, len(res.Pages),
						page.FirstItem(), page.LastItem(), page.MoreItems())
				}
				if !lastPage && page.ItemCount != uint32(tc.pageCount) {
					t.Fatalf("page %d: %d records, want %d", i, page.ItemCount, tc.pageCount)
				}
			}
			if next != seededLogRecords+1 {
				t.Fatalf("pages end at sequence %d, want %d", next-1, seededLogRecords)
			}
		})
	}
}

func TestBACnet4JReadRangeBySequenceNumber(t *testing.T) { bacnet4jSeededLog().runBySequenceNumber(t) }

func TestBACnet4JReadRangeByTime(t *testing.T) { bacnet4jSeededLog().runByTime(t) }

func TestBACnet4JReadRangeAllBySequenceNumber(t *testing.T) {
	bacnet4jSeededLog().runAllBySequenceNumber(t)
}

// A ReadRange byPosition reply of BACnet4J carries firstSequenceNumber too,
// which the standard has only for bySequenceNumber and byTime replies. The
// client decodes it; bacnet-stack omits it for byPosition.
func TestBACnet4JReadRangeByPositionCarriesFirstSequence(t *testing.T) {
	p := bacnet4jSeededLog()
	c, peer, tl := p.start(t)
	if peer.assertedByReexec {
		return
	}
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	ack, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
		Object:         tl,
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 7,
		Count:          3,
	})
	if err != nil {
		t.Fatalf("ReadRange byPosition: %v", err)
	}
	p.requireRange(t, "position 7 count 3", ack, rangeExpect{items: 3, firstPos: 7})
}

// The Worldiety variants are adapter-shim evidence: see the comment at the
// top of this file.
func TestWorldietyReadRangeBySequenceNumber(t *testing.T) {
	worldietySeededLog().runBySequenceNumber(t)
}

func TestWorldietyReadRangeByTime(t *testing.T) { worldietySeededLog().runByTime(t) }

func TestWorldietyReadRangeAllBySequenceNumber(t *testing.T) {
	worldietySeededLog().runAllBySequenceNumber(t)
}
