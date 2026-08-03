// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestReadRangeAllRejectsByTime(t *testing.T) {
	c := &Client{}
	_, err := c.ReadRangeAll(context.Background(), Target{}, service.ReadRangeRequest{
		Object:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:       service.ReadRangeByTime,
	}, ReadRangePageOptions{})
	if err == nil {
		t.Fatal("expected unsupported")
	}
}
