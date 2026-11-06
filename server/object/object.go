// SPDX-License-Identifier: MIT

// Package object is the object model served by package server: the Object
// interface, the Device object (clause 12.11) and the standard objects a
// supervisory client usually meets.
//
// Objects are in-memory and safe for concurrent use. The server never holds
// a lock of its own while it calls an object. Applications may implement
// Object themselves; the built-in types embed Base.
//
// This package does not import the server runtime, the client or the wire
// codecs below package service.
package object

import (
	"context"
	"errors"
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// Object is one BACnet object served by a Device.
type Object interface {
	// Identifier returns the object identifier. It never changes.
	Identifier() bacnet.ObjectIdentifier
	// Name returns the current Object_Name.
	Name() string
	// ReadProperty returns a property value. An array property read without
	// an index returns the whole array as a constructed value; index 0 is the
	// array length. Errors are *bacnet.ErrorResponse values (see Err).
	ReadProperty(ctx context.Context, ref bacnet.PropertyReference) (bacnet.ApplicationValue, error)
	// WriteProperty writes a property. Errors are *bacnet.ErrorResponse
	// values (see Err).
	WriteProperty(ctx context.Context, w Write) error
	// Properties returns the identifiers of every property the object has, in
	// a stable order, and which of them are required by the standard for its
	// type (used for ReadPropertyMultiple ALL / REQUIRED / OPTIONAL).
	Properties() (all []bacnet.PropertyIdentifier, required map[bacnet.PropertyIdentifier]bool)
}

// Write is one WriteProperty or one element of a WritePropertyMultiple.
type Write struct {
	Property bacnet.PropertyReference
	Value    bacnet.ApplicationValue
	// Priority is 1..16, or 0 when the request carried none.
	Priority uint8
}

// Err returns a BACnet error (clause 18) with the given class and code. The
// server fills in the invoke ID and service of the request it answers.
func Err(class, code uint16) error {
	return &bacnet.ErrorResponse{Class: class, Code: code}
}

// Common errors.
var (
	ErrUnknownProperty   = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	ErrWriteAccessDenied = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	ErrReadAccessDenied  = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeReadAccessDenied)
	ErrInvalidDataType   = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType)
	ErrValueOutOfRange   = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeValueOutOfRange)
	ErrNotAnArray        = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodePropertyIsNotAnArray)
	ErrInvalidArrayIndex = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidArrayIndex)
	ErrUnknownObject     = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	ErrDuplicateName     = Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeDuplicateName)
	ErrObjectExists      = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeObjectIdentifierAlreadyExists)
	ErrNotDeletable      = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
	ErrNotCreatable      = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeDynamicCreationNotSupported)
	ErrUnsupportedType   = Err(bacnet.ErrorClassObject, bacnet.ErrorCodeUnsupportedObjectType)
	ErrNoSpaceForObject  = Err(bacnet.ErrorClassResources, bacnet.ErrorCodeNoSpaceForObject)
)

// AsError returns err as a BACnet error. Anything that is not one becomes
// device/operational-problem, so an application failure never leaks to the
// network as anything but a well-formed Error.
func AsError(err error) *bacnet.ErrorResponse {
	var e *bacnet.ErrorResponse
	if errors.As(err, &e) {
		return &bacnet.ErrorResponse{Class: e.Class, Code: e.Code}
	}
	return &bacnet.ErrorResponse{Class: bacnet.ErrorClassDevice, Code: bacnet.ErrorCodeOperationalProblem}
}

// Change reports that properties of an object changed value.
type Change struct {
	Object     bacnet.ObjectIdentifier
	Properties []bacnet.PropertyIdentifier
}

// Notifier receives changes; a Device installs one in every object it
// holds. Implementations of Object that report COV or events call it after
// a value changed, never while holding their own lock.
type Notifier func(Change)

// Notifying is implemented by objects that report changes.
type Notifying interface {
	SetNotifier(Notifier)
}

// COVReporter is implemented by objects that support SubscribeCOV
// (clause 13.1): the values sent in a notification and the change that
// triggers one.
type COVReporter interface {
	// COVValues returns the properties a COV notification carries.
	COVValues(ctx context.Context) []PropertyValue
	// COVIncrement returns the increment of Present_Value that triggers a
	// notification for an analog object; ok is false for objects where every
	// change of Present_Value does.
	COVIncrement() (increment float32, ok bool)
}

// PropertyValue is one property and its value.
type PropertyValue struct {
	Property bacnet.PropertyIdentifier
	Value    bacnet.ApplicationValue
}

// Deletable is implemented by objects that DeleteObject may remove.
type Deletable interface {
	Deletable() bool
}

func errorf(format string, a ...any) error { return fmt.Errorf("object: "+format, a...) }
