// SPDX-License-Identifier: MIT

package client

import (
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
)

// RetransmitPolicy controls exact-APDU retransmission within one transaction.
type RetransmitPolicy int

const (
	RetransmitEnabled RetransmitPolicy = iota
	RetransmitDisabled
)

// DefaultRetransmitPolicy returns per-service exact-APDU retransmission defaults.
func DefaultRetransmitPolicy(serviceChoice uint8) RetransmitPolicy {
	switch serviceChoice {
	case apdu.ServiceReadProperty, apdu.ServiceReadPropertyMultiple, apdu.ServiceReadRange,
		apdu.ServiceGetEventInformation, apdu.ServiceGetAlarmSummary, apdu.ServiceGetEnrollmentSummary,
		apdu.ServiceAtomicReadFile, apdu.ServiceAuditLogQuery:
		return RetransmitEnabled
	case apdu.ServiceWriteProperty, apdu.ServiceWritePropertyMultiple,
		apdu.ServiceAtomicWriteFile, apdu.ServiceAddListElement, apdu.ServiceRemoveListElement,
		apdu.ServiceCreateObject, apdu.ServiceDeleteObject,
		apdu.ServiceSubscribeCOV, apdu.ServiceSubscribeCOVProperty, apdu.ServiceSubscribeCOVPropertyMultiple,
		apdu.ServiceAcknowledgeAlarm, apdu.ServiceDeviceCommunicationControl, apdu.ServiceReinitializeDevice,
		apdu.ServiceConfirmedPrivateTransfer, apdu.ServiceConfirmedTextMessage,
		apdu.ServiceLifeSafetyOperation, apdu.ServiceVTOpen, apdu.ServiceVTClose, apdu.ServiceVTData,
		apdu.ServiceAuthRequest:
		return RetransmitDisabled
	default:
		return RetransmitDisabled
	}
}

type txPhase uint8

const (
	txAwaitingInitial txPhase = iota
	txReceivingSegments
	txSendingSegments
)

type pendingTx struct {
	invokeID    uint8
	service     uint8
	address     bacnet.Address
	origin      bip.Endpoint
	immediate   bip.Endpoint
	encodedAPDU []byte
	retriesLeft int
	result      chan txResult
	timer       interface {
		Stop() bool
		Reset(d time.Duration) bool
	}
	sent  bool
	phase txPhase
}

type txResult struct {
	pdu apdu.PDU
	src packetSource
	err error
}

type txManager struct {
	mu         sync.Mutex
	nextID     uint8
	inUse      map[uint8]*pendingTx
	quarantine map[uint8]time.Time // invoke ID -> reusable after
	max        int
	clockNow   func() time.Time
}

func newTxManager(max int, now func() time.Time) *txManager {
	if max <= 0 || max > 255 {
		max = 255
	}
	return &txManager{
		inUse:      make(map[uint8]*pendingTx),
		quarantine: make(map[uint8]time.Time),
		max:        max,
		clockNow:   now,
	}
}

func (m *txManager) tryAllocLocked() (uint8, bool) {
	if len(m.inUse) >= m.max {
		return 0, false
	}
	now := m.clockNow()
	for i := 0; i < 256; i++ {
		id := m.nextID
		m.nextID++
		if _, used := m.inUse[id]; used {
			continue
		}
		if until, q := m.quarantine[id]; q && now.Before(until) {
			continue
		}
		delete(m.quarantine, id)
		return id, true
	}
	return 0, false
}

func (m *txManager) register(tx *pendingTx) {
	m.mu.Lock()
	m.inUse[tx.invokeID] = tx
	m.mu.Unlock()
}

// complete publishes a terminal outcome if this caller wins the race.
// Returns true when this call removed the pending transaction.
// Losers must consume the already-published result from the transaction channel.
func (m *txManager) complete(invokeID uint8, res txResult, quarantine time.Duration) bool {
	m.mu.Lock()
	tx, ok := m.inUse[invokeID]
	if ok {
		delete(m.inUse, invokeID)
		if quarantine > 0 {
			m.quarantine[invokeID] = m.clockNow().Add(quarantine)
		}
	}
	m.mu.Unlock()
	if !ok {
		return false
	}
	if tx.timer != nil {
		tx.timer.Stop()
	}
	select {
	case tx.result <- res:
	default:
	}
	return true
}

// enterSegmented stops the APDU timer and disables retransmission for the
// remainder of the transaction. Returns false if the transaction is gone.
func (m *txManager) enterSegmented(invokeID uint8) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.inUse[invokeID]
	if !ok {
		return false
	}
	tx.phase = txReceivingSegments
	tx.retriesLeft = 0
	if tx.timer != nil {
		tx.timer.Stop()
	}
	return true
}

// enterSendingSegments marks segmented confirmed-request transmission. The
// APDU timer is stopped; the segment sender owns segment timeouts.
func (m *txManager) enterSendingSegments(invokeID uint8) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.inUse[invokeID]
	if !ok {
		return false
	}
	tx.phase = txSendingSegments
	tx.retriesLeft = 0
	if tx.timer != nil {
		tx.timer.Stop()
	}
	return true
}

// enterAwaitingResponse resumes waiting for the service response after the
// last request segment was acknowledged.
func (m *txManager) enterAwaitingResponse(invokeID uint8, d time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.inUse[invokeID]
	if !ok {
		return false
	}
	tx.phase = txAwaitingInitial
	if tx.timer != nil && d > 0 {
		tx.timer.Reset(d)
	}
	return true
}

type timeoutAction uint8

const (
	timeoutGone timeoutAction = iota
	timeoutIgnoreSegmented
	timeoutRetransmit
	timeoutFail
)

// onTimeout decides the APDU-timer outcome under the manager mutex so phase
// and retriesLeft cannot race with enterSegmented.
func (m *txManager) onTimeout(invokeID uint8, retransmitAllowed bool) timeoutAction {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.inUse[invokeID]
	if !ok {
		return timeoutGone
	}
	if tx.phase == txReceivingSegments || tx.phase == txSendingSegments {
		return timeoutIgnoreSegmented
	}
	if retransmitAllowed && tx.retriesLeft > 0 {
		tx.retriesLeft--
		return timeoutRetransmit
	}
	return timeoutFail
}

func (m *txManager) phase(invokeID uint8) (txPhase, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tx, ok := m.inUse[invokeID]
	if !ok {
		return 0, false
	}
	return tx.phase, true
}

func (m *txManager) lookup(invokeID uint8) *pendingTx {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.inUse[invokeID]
}

func (m *txManager) matchSource(tx *pendingTx, src packetSource) bool {
	return matchTargetSource(Target{
		Address:  tx.address,
		Endpoint: tx.immediate,
		Origin:   tx.origin,
	}, src)
}

func (m *txManager) abortAll(err error) {
	m.mu.Lock()
	pending := make([]*pendingTx, 0, len(m.inUse))
	for _, tx := range m.inUse {
		pending = append(pending, tx)
	}
	m.inUse = make(map[uint8]*pendingTx)
	m.mu.Unlock()
	for _, tx := range pending {
		if tx.timer != nil {
			tx.timer.Stop()
		}
		resErr := err
		if tx.sent {
			resErr = wrapOutcomeUnknown(tx.service, err)
		}
		select {
		case tx.result <- txResult{err: resErr}:
		default:
		}
	}
}
