// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

// State_Text holds one text per state, and Number_Of_States is not
// writable: a write of the whole array keeps its length. Texts are written
// one by one through the array index, or all together.
func TestStateTextKeepsNumberOfStates(t *testing.T) {
	ctx := context.Background()
	texts := func(s ...string) bacnet.ApplicationValue {
		els := make([]bacnet.Element, len(s))
		for i, v := range s {
			els[i] = bacnet.Element{Value: CharString(v)}
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}
	stateText := bacnet.PropertyReference{Identifier: bacnet.PropertyStateText}
	index := func(i uint32) bacnet.PropertyReference {
		return bacnet.PropertyReference{Identifier: bacnet.PropertyStateText, ArrayIndex: &i}
	}
	for _, tc := range []struct {
		name  string
		write Write
		want  error // nil: accepted
	}{
		{"all three texts", Write{Property: stateText, Value: texts("a", "b", "c")}, nil},
		{"no text", Write{Property: stateText, Value: texts()}, ErrValueOutOfRange},
		{"one text", Write{Property: stateText, Value: texts("a")}, ErrValueOutOfRange},
		{"two texts", Write{Property: stateText, Value: texts("a", "b")}, ErrValueOutOfRange},
		{"four texts", Write{Property: stateText, Value: texts("a", "b", "c", "d")}, ErrValueOutOfRange},
		{"three values of another type", Write{Property: stateText, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
			{Value: bacnet.UnsignedValue(1)}, {Value: bacnet.UnsignedValue(2)}, {Value: bacnet.UnsignedValue(3)}}}}, ErrInvalidDataType},
		{"a single text for the whole array", Write{Property: stateText, Value: CharString("a")}, ErrInvalidDataType},
		{"text 1", Write{Property: index(1), Value: CharString("one")}, nil},
		{"text 3", Write{Property: index(3), Value: CharString("three")}, nil},
		{"text 4", Write{Property: index(4), Value: CharString("four")}, ErrInvalidArrayIndex},
		{"the length", Write{Property: index(0), Value: bacnet.UnsignedValue(5)}, ErrInvalidArrayIndex},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewMultiStateValue(PointConfig{Instance: 1, Name: "MSV", NumberOfStates: 3, StateText: []string{"x", "y", "z"}})
			if err != nil {
				t.Fatal(err)
			}
			err = p.WriteProperty(ctx, tc.write)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("write: %v, want %v", err, tc.want)
			}
			n, err := p.ReadProperty(ctx, index(0))
			states, err2 := p.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyNumberOfStates})
			if err != nil || err2 != nil || n.Unsigned != 3 || states.Unsigned != 3 {
				t.Fatalf("State_Text length %d (%v), Number_Of_States %d (%v): want 3 and 3", n.Unsigned, err, states.Unsigned, err2)
			}
		})
	}
}
