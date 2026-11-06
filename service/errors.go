// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// ErrUndefinedEnumeration reports a decoded enumerated parameter outside the
// values the standard defines for it. It wraps bacnet.ErrMalformed; a server
// answers it with Reject undefined-enumeration (clause 18.8).
var ErrUndefinedEnumeration = fmt.Errorf("%w: undefined enumeration", bacnet.ErrMalformed)
