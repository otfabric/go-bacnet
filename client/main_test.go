// SPDX-License-Identifier: MIT

package client

import (
	"testing"

	"github.com/otfabric/go-bacnet/internal/leak"
)

// TestMain fails the package when a test leaves a goroutine behind.
func TestMain(m *testing.M) { leak.VerifyNoLeaks(m) }
