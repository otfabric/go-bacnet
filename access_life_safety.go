// SPDX-License-Identifier: MIT

package bacnet

// Life-safety and access enumerations used by supervisory decode helpers.
// Values follow ASHRAE 135 Clause 21; unknown numeric values remain usable.

// LifeSafetyMode is BACnetLifeSafetyMode.
type LifeSafetyMode uint32

const (
	LifeSafetyModeOff LifeSafetyMode = iota
	LifeSafetyModeOn
	LifeSafetyModeTest
	LifeSafetyModeManned
	LifeSafetyModeArmed
	LifeSafetyModeDisarmed
	LifeSafetyModePrearmed
	LifeSafetyModeQuiet
	LifeSafetyModeFaultSealed
	LifeSafetyModeFaultUnsealed
	LifeSafetyModeFaultImmediate
	LifeSafetyModeFaultDelayed
	LifeSafetyModePullActive
	LifeSafetyModeManual
	LifeSafetyModeInterlocked
	LifeSafetyModeBlocked
	LifeSafetyModeTamper
	LifeSafetyModeTestActiveAlarm
	LifeSafetyModeTestActiveFault
	LifeSafetyModeTestActiveFaultAlarm
	LifeSafetyModeTestHold
	LifeSafetyModeTestHoldAlarm
	LifeSafetyModeTestHoldFault
	LifeSafetyModeTestHoldFaultAlarm
)

// LifeSafetyState is BACnetLifeSafetyState.
type LifeSafetyState uint32

const (
	LifeSafetyStateQuiet LifeSafetyState = iota
	LifeSafetyStatePreAlarm
	LifeSafetyStateAlarm
	LifeSafetyStateFault
	LifeSafetyStateFaultPreAlarm
	LifeSafetyStateFaultAlarm
	LifeSafetyStateNotReady
	LifeSafetyStateActive
	LifeSafetyStateTamper
	LifeSafetyStateTestAlarm
	LifeSafetyStateTestActive
	LifeSafetyStateTestFault
	LifeSafetyStateTestFaultAlarm
	LifeSafetyStateHoldup
	LifeSafetyStateHoldupAlarm
	LifeSafetyStateDuress
	LifeSafetyStateTamperAlarm
	LifeSafetyStateAbnormal
	LifeSafetyStateEmergencyPower
	LifeSafetyStateDelayed
	LifeSafetyStateBlocked
	LifeSafetyStateLocalAlarm
	LifeSafetyStateGeneralAlarm
	LifeSafetyStateSupervisory
	LifeSafetyStateTestSupervisory
)

// LifeSafetyOperation is BACnetLifeSafetyOperation.
type LifeSafetyOperation uint32

const (
	LifeSafetyOperationNone LifeSafetyOperation = iota
	LifeSafetyOperationSilence
	LifeSafetyOperationSilenceAudible
	LifeSafetyOperationSilenceVisual
	LifeSafetyOperationReset
	LifeSafetyOperationResetAlarm
	LifeSafetyOperationResetFault
	LifeSafetyOperationUnsilence
	LifeSafetyOperationUnsilenceAudible
	LifeSafetyOperationUnsilenceVisual
)

// DoorStatus is BACnetDoorStatus.
type DoorStatus uint32

const (
	DoorStatusClosed DoorStatus = iota
	DoorStatusOpened
	DoorStatusUnknown
	DoorStatusDoorFault
	DoorStatusUnused
	DoorStatusNone
	DoorStatusClosing
	DoorStatusOpening
	DoorStatusSafetyLocked
	DoorStatusLimitedOpened
)

// LockStatus is BACnetLockStatus.
type LockStatus uint32

const (
	LockStatusLocked LockStatus = iota
	LockStatusUnlocked
	LockStatusFault
	LockStatusUnknown
	LockStatusLockedOut
	LockStatusUnused
)
