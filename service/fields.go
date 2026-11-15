// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// fieldSet records which context tags of a SEQUENCE a decoder has taken, so
// that a tag that comes a second time is refused: the fields of a SEQUENCE
// come once each, and a decoder that kept the later value would return one
// the sender may not have meant.
type fieldSet uint32

// add records tag; it returns an error wrapping bacnet.ErrMalformed when the
// tag was recorded before. what names the message for the error text. Tags
// above 31 are not tracked (no SEQUENCE decoded here has that many fields).
func (s *fieldSet) add(tag uint8, what string) error {
	if tag > 31 {
		return nil
	}
	bit := fieldSet(1) << tag
	if *s&bit != 0 {
		return fmt.Errorf("%w: %s field [%d] repeated", bacnet.ErrMalformed, what, tag)
	}
	*s |= bit
	return nil
}

// dateTimePair returns the BACnetDateTime held by els: exactly a Date and a
// Time, application-tagged, in that order.
func dateTimePair(els []bacnet.Element, what string) (DateTime, error) {
	if len(els) != 2 || els[0].Context || els[0].Value.Kind != bacnet.ValueDate ||
		els[1].Context || els[1].Value.Kind != bacnet.ValueTime {
		return DateTime{}, fmt.Errorf("%w: %s is not a Date and a Time", bacnet.ErrMalformed, what)
	}
	return DateTime{Date: els[0].Value.Date, Time: els[1].Value.Time}, nil
}

// fieldTags checks that els are context-tagged [0], [1], … in order, for a
// SEQUENCE whose fields are all required and taken by position: a field
// with another tag is not the one its position names.
func fieldTags(els []bacnet.Element, what string) error {
	for i, el := range els {
		if !el.Context || int(el.TagNumber) != i {
			return fmt.Errorf("%w: %s field %d is not tagged [%d]", bacnet.ErrMalformed, what, i+1, i)
		}
	}
	return nil
}
