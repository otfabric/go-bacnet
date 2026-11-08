// SPDX-License-Identifier: MIT

package capture

import (
	"fmt"

	"github.com/otfabric/go-bacnet/apdu"
)

// Transaction is one confirmed request and the answer that ended it,
// correlated by requester, responder and invoke ID (clause 5.4): the
// requester is the request's origin and NPDU source, the responder its
// destination and NPDU destination, and the answer travels the other way.
type Transaction struct {
	Request  []Message // every segment of the request, in order (one if unsegmented)
	Response []Message // every segment of the answer, in order; nil if none was captured
	// SegmentACKs are the SegmentACK PDUs of either side between the first
	// request segment and the last answer segment.
	SegmentACKs []Message
	InvokeID    uint8
	Service     uint8
}

// Answered reports whether the capture holds the answer.
func (t Transaction) Answered() bool { return len(t.Response) > 0 }

// Final returns the request's last segment and the answer's last segment
// (zero Message when unanswered): the ones carrying the decoded service.
func (t Transaction) Final() (req, resp Message) {
	req = t.Request[len(t.Request)-1]
	if len(t.Response) > 0 {
		resp = t.Response[len(t.Response)-1]
	}
	return req, resp
}

// Endpoint names one side of a transaction: B/IP node and NPDU address.
func Endpoint(m Message, source bool) string {
	if m.NPDU == nil {
		return ""
	}
	if source {
		return fmt.Sprintf("%s|%v", m.Origin, m.NPDU.Source)
	}
	return fmt.Sprintf("%s|%v", m.Dst, m.NPDU.Destination)
}

type txKey struct {
	requester, responder string
	invoke               uint8
}

// Transactions correlates the decoded messages of one capture. A request
// whose invoke ID is reused before an answer arrived starts a new
// transaction; the old one stays unanswered. Answers without a request in
// the capture are counted in orphans.
func Transactions(msgs []Message) (txs []Transaction, orphans []Message) {
	// latest is the index into txs of the last transaction per key; done
	// marks the ones whose answer is complete (their final SegmentACK may
	// still follow).
	latest := map[txKey]int{}
	done := map[int]bool{}
	for _, m := range msgs {
		if m.APDU == nil || m.Err != nil {
			continue
		}
		p := m.APDU
		switch p.Type {
		case apdu.TypeConfirmedRequest:
			r := p.ConfirmedRequest
			k := txKey{Endpoint(m, true), Endpoint(m, false), r.InvokeID}
			if i, ok := latest[k]; ok && !done[i] && r.SegmentedMessage && r.SequenceNumber > 0 {
				txs[i].Request = append(txs[i].Request, m)
				continue
			}
			if i, ok := latest[k]; ok && !done[i] && sameRequest(txs[i].Request[0], m) {
				continue // a retry of the same request
			}
			latest[k] = len(txs)
			txs = append(txs, Transaction{Request: []Message{m}, InvokeID: r.InvokeID, Service: r.ServiceChoice})
		case apdu.TypeSegmentACK:
			a := p.SegmentACK
			// From the server: acknowledges request segments, so the
			// requester is the receiver; from the client: the reverse.
			k := txKey{Endpoint(m, false), Endpoint(m, true), a.InvokeID}
			if !a.Server {
				k = txKey{Endpoint(m, true), Endpoint(m, false), a.InvokeID}
			}
			if i, ok := latest[k]; ok {
				txs[i].SegmentACKs = append(txs[i].SegmentACKs, m)
			}
		default:
			inv, last, ok := answer(p)
			if !ok {
				continue
			}
			k := txKey{Endpoint(m, false), Endpoint(m, true), inv}
			if p.Type == apdu.TypeAbort && !p.Abort.Server { // the requester gave up
				k = txKey{Endpoint(m, true), Endpoint(m, false), inv}
			}
			i, found := latest[k]
			if !found || done[i] {
				orphans = append(orphans, m)
				continue
			}
			txs[i].Response = append(txs[i].Response, m)
			if last {
				done[i] = true
			}
		}
	}
	return txs, orphans
}

// answer returns the invoke ID of a PDU answering a confirmed request and
// whether it ends the transaction (the last segment of a ComplexACK, or
// any other answer).
func answer(p *apdu.PDU) (invoke uint8, done, ok bool) {
	switch p.Type {
	case apdu.TypeSimpleACK:
		return p.SimpleACK.InvokeID, true, true
	case apdu.TypeComplexACK:
		return p.ComplexACK.InvokeID, !p.ComplexACK.MoreFollows, true
	case apdu.TypeError:
		return p.Error.InvokeID, true, true
	case apdu.TypeReject:
		return p.Reject.InvokeID, true, true
	case apdu.TypeAbort:
		return p.Abort.InvokeID, true, true
	default: // a request or a SegmentACK
		return 0, false, false
	}
}

func sameRequest(a, b Message) bool {
	return string(a.Data) == string(b.Data)
}
