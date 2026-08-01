//go:build interop

// SPDX-License-Identifier: MIT

package interop

import "testing"

func TestRoutedSubnetPairDistinct(t *testing.T) {
	for attempt := 1; attempt <= 8; attempt++ {
		subnetA, _, routerA, subnetB, _, routerB, deviceB := routedSubnetPair("test-base", attempt)
		if subnetA == subnetB {
			t.Fatalf("attempt %d: overlapping subnets %s", attempt, subnetA)
		}
		if routerA == routerB {
			t.Fatalf("attempt %d: router addresses collide", attempt)
		}
		if deviceB == routerB {
			t.Fatalf("attempt %d: device and router share address on net B", attempt)
		}
	}
}
