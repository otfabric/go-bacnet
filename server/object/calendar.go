// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"time"

	"github.com/otfabric/go-bacnet"
)

// CalendarConfig configures a Calendar object.
type CalendarConfig struct {
	Instance    uint32
	Name        string
	Description string
	DateList    []bacnet.CalendarEntry
	// MaxEntries bounds Date_List (default 64); a longer write or
	// AddListElement fails with resources/no-space-to-add-list-element.
	MaxEntries int
}

// Calendar is a Calendar object (clause 12.9). Present_Value is TRUE when
// the device's local date is in an entry of Date_List. It is evaluated
// against the server's clock on every Poll (about once a second) and when
// Date_List changes; before the first Poll the date of the host clock is
// used.
type Calendar struct {
	*Base
	max     int
	entries []bacnet.CalendarEntry
	today   bacnet.Date
	pv      bool
}

// NewCalendar returns a Calendar object.
func NewCalendar(cfg CalendarConfig) (*Calendar, error) {
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 64
	}
	if len(cfg.DateList) > cfg.MaxEntries {
		return nil, errorf("date list has %d entries, at most %d", len(cfg.DateList), cfg.MaxEntries)
	}
	for i, e := range cfg.DateList {
		if _, err := calendarEntryElement(e); err != nil {
			return nil, errorf("date list entry %d: %v", i, err)
		}
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeCalendar, Instance: cfg.Instance}
	c := &Calendar{Base: NewBase(id, cfg.Name), max: cfg.MaxEntries,
		entries: append([]bacnet.CalendarEntry(nil), cfg.DateList...), today: dateOf(time.Now())}
	c.pv = c.contains(c.today)
	if cfg.Description != "" {
		c.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	c.Define(bacnet.PropertyPresentValue, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.BoolValue(c.pv) }})
	c.Define(bacnet.PropertyDateList, &Property{Required: true, Writable: true, Get: c.dateList, Set: c.writeDateList})
	return c, nil
}

func (c *Calendar) dateList() bacnet.ApplicationValue {
	els := make([]bacnet.Element, 0, len(c.entries))
	for _, e := range c.entries {
		el, _ := calendarEntryElement(e) // entries are validated when stored
		els = append(els, el)
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

// parseEntries decodes written calendar entries; first is the 1-based
// element that failed.
func parseEntries(els []bacnet.Element) ([]bacnet.CalendarEntry, uint32, error) {
	out := make([]bacnet.CalendarEntry, 0, len(els))
	for i, el := range els {
		e, err := parseCalendarEntry(el)
		if err != nil {
			return nil, uint32(i + 1), err
		}
		out = append(out, e)
	}
	return out, 0, nil
}

func (c *Calendar) writeDateList(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		return nil, ErrNotAnArray
	}
	entries, _, err := parseEntries(listElements(w.Value))
	if err != nil {
		return nil, err
	}
	if len(entries) > c.max {
		return nil, ErrNoSpaceToWriteProperty
	}
	return c.setEntriesLocked(entries), nil
}

// setEntriesLocked replaces Date_List and re-evaluates Present_Value.
func (c *Calendar) setEntriesLocked(entries []bacnet.CalendarEntry) []bacnet.PropertyIdentifier {
	c.entries = entries
	changed := []bacnet.PropertyIdentifier{bacnet.PropertyDateList}
	if pv := c.contains(c.today); pv != c.pv {
		c.pv = pv
		changed = append(changed, bacnet.PropertyPresentValue)
	}
	return changed
}

// contains reports whether d is in an entry of Date_List (lock held).
func (c *Calendar) contains(d bacnet.Date) bool {
	for _, e := range c.entries {
		if entryMatches(e, d) {
			return true
		}
	}
	return false
}

// Contains reports whether the date d is in an entry of Date_List.
func (c *Calendar) Contains(d bacnet.Date) bool {
	var ok bool
	c.View(func() { ok = c.contains(d) })
	return ok
}

// PresentValue returns Present_Value as last evaluated.
func (c *Calendar) PresentValue() bool {
	var v bool
	c.View(func() { v = c.pv })
	return v
}

// DateList returns Date_List.
func (c *Calendar) DateList() []bacnet.CalendarEntry {
	var out []bacnet.CalendarEntry
	c.View(func() { out = append(out, c.entries...) })
	return out
}

// SetDateList replaces Date_List from the application.
func (c *Calendar) SetDateList(entries []bacnet.CalendarEntry) error {
	if len(entries) > c.max {
		return errorf("date list has %d entries, at most %d", len(entries), c.max)
	}
	for i, e := range entries {
		if _, err := calendarEntryElement(e); err != nil {
			return errorf("date list entry %d: %v", i, err)
		}
	}
	c.Update(func() []bacnet.PropertyIdentifier {
		return c.setEntriesLocked(append([]bacnet.CalendarEntry(nil), entries...))
	})
	return nil
}

// Evaluate sets the local date and re-evaluates Present_Value.
func (c *Calendar) Evaluate(now time.Time) {
	c.Update(func() []bacnet.PropertyIdentifier {
		c.today = dateOf(now)
		if pv := c.contains(c.today); pv != c.pv {
			c.pv = pv
			return []bacnet.PropertyIdentifier{bacnet.PropertyPresentValue}
		}
		return nil
	})
}

// Poll implements Poller: Present_Value follows the server's clock.
func (c *Calendar) Poll(_ context.Context, now time.Time, _ *Device) { c.Evaluate(now) }

func (c *Calendar) listProperty(prop bacnet.PropertyReference) error {
	if prop.Identifier != bacnet.PropertyDateList {
		if _, err := c.ReadProperty(context.Background(), bacnet.PropertyReference{Identifier: prop.Identifier}); err != nil {
			return err
		}
		return ErrPropertyIsNotAList
	}
	if prop.ArrayIndex != nil {
		return ErrNotAnArray
	}
	return nil
}

// AddListElements implements ListModifier for Date_List; an entry already
// in the list is not added again (clause 15.1).
func (c *Calendar) AddListElements(_ context.Context, prop bacnet.PropertyReference, els []bacnet.Element) error {
	if err := c.listProperty(prop); err != nil {
		return err
	}
	add, first, err := parseEntries(els)
	if err != nil {
		return &ListError{Err: err, First: first}
	}
	var err2 error
	c.Update(func() []bacnet.PropertyIdentifier {
		next := append([]bacnet.CalendarEntry(nil), c.entries...)
		for i, e := range add {
			if indexOfEntry(next, e) >= 0 {
				continue
			}
			if len(next) >= c.max {
				err2 = &ListError{Err: ErrNoSpaceForListElement, First: uint32(i + 1)}
				return nil
			}
			next = append(next, e)
		}
		return c.setEntriesLocked(next)
	})
	return err2
}

// RemoveListElements implements ListModifier for Date_List; an entry that
// is not in the list fails the request and nothing is removed (clause 15.2).
func (c *Calendar) RemoveListElements(_ context.Context, prop bacnet.PropertyReference, els []bacnet.Element) error {
	if err := c.listProperty(prop); err != nil {
		return err
	}
	rm, first, err := parseEntries(els)
	if err != nil {
		return &ListError{Err: err, First: first}
	}
	var err2 error
	c.Update(func() []bacnet.PropertyIdentifier {
		next := append([]bacnet.CalendarEntry(nil), c.entries...)
		for i, e := range rm {
			j := indexOfEntry(next, e)
			if j < 0 {
				err2 = &ListError{Err: ErrListElementNotFound, First: uint32(i + 1)}
				return nil
			}
			next = append(next[:j], next[j+1:]...)
		}
		return c.setEntriesLocked(next)
	})
	return err2
}

func indexOfEntry(list []bacnet.CalendarEntry, e bacnet.CalendarEntry) int {
	for i, x := range list {
		if x == e {
			return i
		}
	}
	return -1
}
