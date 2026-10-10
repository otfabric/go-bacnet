// SPDX-License-Identifier: MIT

package bacnet

// Life-safety and access enumerations used by supervisory decode helpers.
// Values follow ASHRAE 135 Clause 21; unknown numeric values remain usable.

// LifeSafetyMode is BACnetLifeSafetyMode.
type LifeSafetyMode uint32

const (
	LifeSafetyModeOff                      LifeSafetyMode = 0
	LifeSafetyModeOn                       LifeSafetyMode = 1
	LifeSafetyModeTest                     LifeSafetyMode = 2
	LifeSafetyModeManned                   LifeSafetyMode = 3
	LifeSafetyModeUnmanned                 LifeSafetyMode = 4
	LifeSafetyModeArmed                    LifeSafetyMode = 5
	LifeSafetyModeDisarmed                 LifeSafetyMode = 6
	LifeSafetyModePrearmed                 LifeSafetyMode = 7
	LifeSafetyModeSlow                     LifeSafetyMode = 8
	LifeSafetyModeFast                     LifeSafetyMode = 9
	LifeSafetyModeDisconnected             LifeSafetyMode = 10
	LifeSafetyModeEnabled                  LifeSafetyMode = 11
	LifeSafetyModeDisabled                 LifeSafetyMode = 12
	LifeSafetyModeAutomaticReleaseDisabled LifeSafetyMode = 13
	LifeSafetyModeDefault                  LifeSafetyMode = 14
)

// LifeSafetyState is BACnetLifeSafetyState.
type LifeSafetyState uint32

const (
	LifeSafetyStateQuiet           LifeSafetyState = 0
	LifeSafetyStatePreAlarm        LifeSafetyState = 1
	LifeSafetyStateAlarm           LifeSafetyState = 2
	LifeSafetyStateFault           LifeSafetyState = 3
	LifeSafetyStateFaultPreAlarm   LifeSafetyState = 4
	LifeSafetyStateFaultAlarm      LifeSafetyState = 5
	LifeSafetyStateNotReady        LifeSafetyState = 6
	LifeSafetyStateActive          LifeSafetyState = 7
	LifeSafetyStateTamper          LifeSafetyState = 8
	LifeSafetyStateTestAlarm       LifeSafetyState = 9
	LifeSafetyStateTestActive      LifeSafetyState = 10
	LifeSafetyStateTestFault       LifeSafetyState = 11
	LifeSafetyStateTestFaultAlarm  LifeSafetyState = 12
	LifeSafetyStateHoldup          LifeSafetyState = 13
	LifeSafetyStateDuress          LifeSafetyState = 14
	LifeSafetyStateTamperAlarm     LifeSafetyState = 15
	LifeSafetyStateAbnormal        LifeSafetyState = 16
	LifeSafetyStateEmergencyPower  LifeSafetyState = 17
	LifeSafetyStateDelayed         LifeSafetyState = 18
	LifeSafetyStateBlocked         LifeSafetyState = 19
	LifeSafetyStateLocalAlarm      LifeSafetyState = 20
	LifeSafetyStateGeneralAlarm    LifeSafetyState = 21
	LifeSafetyStateSupervisory     LifeSafetyState = 22
	LifeSafetyStateTestSupervisory LifeSafetyState = 23
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
	LockStatusLocked    LockStatus = 0
	LockStatusUnlocked  LockStatus = 1
	LockStatusLockFault LockStatus = 2
	LockStatusUnused    LockStatus = 3
	LockStatusUnknown   LockStatus = 4
)
