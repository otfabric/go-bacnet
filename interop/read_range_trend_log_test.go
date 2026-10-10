//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// The bacnet-stack adapter's trend-log:0 is frozen: upstream Trend_Log_Init
// seeds it and the adapter never samples. Record k (1-based position) has
//
//	time stamp       2009-01-01 00:00:00 + (k-1) × 15 minutes
//	logDatum         REAL k-1
//	statusFlags      present, all clear
//	sequence number  stackLogFirstSequence + k-1
//
// with Record_Count 1000 and Total_Record_Count 10000.
const (
	stackLogRecords       = 1000
	stackLogTotalRecords  = 10000
	stackLogFirstSequence = stackLogTotalRecords - stackLogRecords + 1 // 9001
	stackLogInterval      = 15 * time.Minute

	propertyRecordCount      bacnet.PropertyIdentifier = 141
	propertyTotalRecordCount bacnet.PropertyIdentifier = 145
)

var stackLogStart = time.Date(2009, time.January, 1, 0, 0, 0, 0, time.UTC)

// bacnetDateTime converts t to the BACnet date and time bacnet-stack encodes
// (day of week 1 = Monday … 7 = Sunday, hundredths 0).
func bacnetDateTime(t time.Time) service.DateTime {
	wd := uint8(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return service.DateTime{
		Date: bacnet.Date{Year: uint8(t.Year() - 1900), Month: uint8(t.Month()), Day: uint8(t.Day()), Weekday: wd},
		Time: bacnet.Time{Hour: uint8(t.Hour()), Minute: uint8(t.Minute()), Second: uint8(t.Second())},
	}
}

// stackLogTime is the time stamp of the record at 1-based position pos.
func stackLogTime(pos int) time.Time {
	return stackLogStart.Add(time.Duration(pos-1) * stackLogInterval)
}

// requireStackLogRecords checks that records are the seeded records at
// positions firstPos, firstPos+1, ….
func requireStackLogRecords(t *testing.T, what string, records []service.LogRecord, firstPos int) {
	t.Helper()
	for i, rec := range records {
		pos := firstPos + i
		if want := bacnetDateTime(stackLogTime(pos)); rec.Timestamp != want {
			t.Fatalf("%s: record %d (position %d) time stamp %+v, want %+v", what, i, pos, rec.Timestamp, want)
		}
		if rec.DatumChoice != service.LogDatumReal || rec.Datum.Kind != bacnet.ValueReal {
			t.Fatalf("%s: record %d logDatum choice %d kind %v, want REAL", what, i, rec.DatumChoice, rec.Datum.Kind)
		}
		if rec.Datum.Real != float32(pos-1) {
			t.Fatalf("%s: record %d (position %d) value %v, want %v", what, i, pos, rec.Datum.Real, float32(pos-1))
		}
		if rec.StatusFlags == nil {
			t.Fatalf("%s: record %d has no statusFlags", what, i)
		}
		if rec.StatusFlags.UnusedBits != 4 || len(rec.StatusFlags.Bytes) != 1 || rec.StatusFlags.Bytes[0] != 0 {
			t.Fatalf("%s: record %d statusFlags %+v, want four clear bits", what, i, *rec.StatusFlags)
		}
	}
}

type rangeExpect struct {
	items    int // records returned
	firstPos int // 1-based position of the first record; ignored when items == 0
	first    bool
	last     bool
	more     bool
}

func requireRange(t *testing.T, what string, ack service.ReadRangeACK, want rangeExpect) {
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
	if wantSeq := uint32(stackLogFirstSequence + want.firstPos - 1); *ack.FirstSequence != wantSeq {
		t.Fatalf("%s: firstSequenceNumber %d, want %d", what, *ack.FirstSequence, wantSeq)
	}
	requireStackLogRecords(t, what, ack.LogRecords, want.firstPos)
}

// stackTrendLog starts the bacnet-stack peer and checks the log is the seeded
// one before a test computes records from the constants above.
func stackTrendLog(t *testing.T) (*client.Client, *peerHandle, bacnet.ObjectIdentifier) {
	t.Helper()
	dev := loadDeviceFixture(t)
	tl, ok := trendLogObject(dev)
	if !ok {
		failOrSkip(t, "device fixture has no trend-log (need device-baseline-v2)")
	}
	peer := startPeer(t, stackPeer().image, stackPeer().name)
	if peer.assertedByReexec {
		return nil, peer, tl
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	for prop, want := range map[bacnet.PropertyIdentifier]uint64{
		propertyRecordCount:      stackLogRecords,
		propertyTotalRecordCount: stackLogTotalRecords,
	} {
		v, err := c.ReadProperty(ctx, peer.target, tl, bacnet.PropertyReference{Identifier: prop})
		if err != nil {
			t.Fatalf("ReadProperty %v property %d: %v", tl, prop, err)
		}
		got, err := bacnet.AsUnsigned(v)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%v property %d = %d, want %d: the log is not the frozen seed", tl, prop, got, want)
		}
	}
	return c, peer, tl
}

func TestBacnetStackReadRangeBySequenceNumber(t *testing.T) {
	c, peer, tl := stackTrendLog(t)
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

	requireRange(t, "first five", read(stackLogFirstSequence, 5),
		rangeExpect{items: 5, firstPos: 1, first: true})
	requireRange(t, "three from the middle", read(stackLogFirstSequence+500, 3),
		rangeExpect{items: 3, firstPos: 501})
	// The range runs past the newest record: truncated, LAST_ITEM set.
	requireRange(t, "ten at the end", read(stackLogTotalRecords-2, 10),
		rangeExpect{items: 3, firstPos: stackLogRecords - 2, last: true})
	// A negative count selects the records up to the reference.
	requireRange(t, "four back from the newest", read(stackLogTotalRecords, -4),
		rangeExpect{items: 4, firstPos: stackLogRecords - 3, last: true})
	// A reference before the oldest record is moved up to it.
	requireRange(t, "before the oldest", read(stackLogFirstSequence-2, 4),
		rangeExpect{items: 2, firstPos: 1, first: true})
	requireRange(t, "past the newest", read(stackLogTotalRecords+1, 5), rangeExpect{})
	// More records than one reply holds: MORE_ITEMS, and fewer than asked.
	big := read(stackLogFirstSequence, 500)
	if !big.MoreItems() || !big.FirstItem() || big.LastItem() {
		t.Fatalf("500 records: flags first=%v last=%v more=%v, want first and more", big.FirstItem(), big.LastItem(), big.MoreItems())
	}
	if big.ItemCount == 0 || big.ItemCount >= 500 || int(big.ItemCount) != len(big.LogRecords) {
		t.Fatalf("500 records: itemCount %d with %d decoded records, want a partial page", big.ItemCount, len(big.LogRecords))
	}
	if big.FirstSequence == nil || *big.FirstSequence != stackLogFirstSequence {
		t.Fatalf("500 records: firstSequenceNumber %v, want %d", big.FirstSequence, stackLogFirstSequence)
	}
	requireStackLogRecords(t, "500 records", big.LogRecords, 1)
	t.Logf("one reply holds %d of the 500 records asked for (MORE_ITEMS)", big.ItemCount)
}

func TestBacnetStackReadRangeByTime(t *testing.T) {
	c, peer, tl := stackTrendLog(t)
	if peer.assertedByReexec {
		return
	}
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	read := func(ref time.Time, count int32) service.ReadRangeACK {
		t.Helper()
		ack, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{
			Object:        tl,
			Property:      bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
			By:            service.ReadRangeByTime,
			ReferenceTime: bacnetDateTime(ref),
			Count:         count,
		})
		if err != nil {
			t.Fatalf("ReadRange byTime ref=%s count=%d: %v", ref.Format(time.DateTime), count, err)
		}
		if ack.Object != tl || ack.Property.Identifier != bacnet.PropertyLogBuffer {
			t.Fatalf("ACK is for %v property %d", ack.Object, ack.Property.Identifier)
		}
		return ack
	}
	last := stackLogTime(stackLogRecords) // 2009-01-11 09:45:00

	// A positive count returns the records after the reference time.
	requireRange(t, "before the oldest", read(stackLogStart.Add(-time.Second), 3),
		rangeExpect{items: 3, firstPos: 1, first: true})
	// The record stamped exactly at the reference time is not "after" it.
	requireRange(t, "at the oldest", read(stackLogStart, 4),
		rangeExpect{items: 4, firstPos: 2})
	requireRange(t, "between two records", read(stackLogTime(501).Add(7*time.Minute), 2),
		rangeExpect{items: 2, firstPos: 502})
	requireRange(t, "running into the end", read(last.Add(-45*time.Minute), 10),
		rangeExpect{items: 3, firstPos: stackLogRecords - 2, last: true})
	requireRange(t, "at the newest", read(last, 5), rangeExpect{})
	// A negative count returns the records before the reference time.
	requireRange(t, "two before 01:00", read(stackLogStart.Add(time.Hour), -2),
		rangeExpect{items: 2, firstPos: 3})
	requireRange(t, "more before 00:30 than exist", read(stackLogStart.Add(30*time.Minute), -5),
		rangeExpect{items: 2, firstPos: 1, first: true})
	requireRange(t, "three before the end of the log", read(last.Add(time.Hour), -3),
		rangeExpect{items: 3, firstPos: stackLogRecords - 2, last: true})
	requireRange(t, "before the oldest, backwards", read(stackLogStart, -3), rangeExpect{})
}

// ReadRangeAll pages a log by sequence number to LAST_ITEM: with pages the
// peer returns whole, and with pages larger than one reply holds (MORE_ITEMS).
func TestBacnetStackReadRangeAllBySequenceNumber(t *testing.T) {
	c, peer, tl := stackTrendLog(t)
	if peer.assertedByReexec {
		return
	}
	ctx, cancel := withTimeout(t, 60*time.Second)
	defer cancel()
	req := service.ReadRangeRequest{
		Object:         tl,
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeBySequenceNumber,
		ReferenceIndex: stackLogFirstSequence,
	}
	for _, tc := range []struct {
		name      string
		pageCount int32
		whole     bool // every page but the last holds exactly pageCount records
	}{
		{"pages of 40", 40, true},
		{"pages of 200", 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.ReadRangeAll(ctx, peer.target, req, client.ReadRangePageOptions{PageCount: tc.pageCount})
			if err != nil {
				t.Fatalf("ReadRangeAll: %v (stopped: %q after %d pages)", err, res.StoppedReason, len(res.Pages))
			}
			if res.Partial {
				t.Fatalf("ReadRangeAll stopped early: %q after %d pages", res.StoppedReason, len(res.Pages))
			}
			if len(res.LogRecords) != stackLogRecords {
				t.Fatalf("%d records in %d pages, the log holds %d", len(res.LogRecords), len(res.Pages), stackLogRecords)
			}
			requireStackLogRecords(t, tc.name, res.LogRecords, 1)
			next := uint32(stackLogFirstSequence)
			for i, page := range res.Pages {
				lastPage := i == len(res.Pages)-1
				if page.FirstSequence == nil || *page.FirstSequence != next {
					t.Fatalf("page %d firstSequenceNumber %v, want %d (pages must be contiguous)", i, page.FirstSequence, next)
				}
				next += page.ItemCount
				if page.FirstItem() != (i == 0) || page.LastItem() != lastPage {
					t.Fatalf("page %d of %d: first=%v last=%v", i, len(res.Pages), page.FirstItem(), page.LastItem())
				}
				if lastPage {
					continue
				}
				if tc.whole && (page.ItemCount != uint32(tc.pageCount) || page.MoreItems()) {
					t.Fatalf("page %d: %d records more=%v, want %d whole", i, page.ItemCount, page.MoreItems(), tc.pageCount)
				}
				if !tc.whole && (page.ItemCount >= uint32(tc.pageCount) || !page.MoreItems()) {
					t.Fatalf("page %d: %d records more=%v, want fewer than %d with MORE_ITEMS", i, page.ItemCount, page.MoreItems(), tc.pageCount)
				}
			}
			if next != stackLogTotalRecords+1 {
				t.Fatalf("pages end at sequence %d, want %d", next-1, stackLogTotalRecords)
			}
			t.Logf("%d records in %d pages to LAST_ITEM", len(res.LogRecords), len(res.Pages))
		})
	}
}
