//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// The native router is bacnet-stack's own apps/router, packaged by
// bacnet-interop 0.10.0 as a second image next to the bacnet-stack device
// server. Unlike bip-router (a topology aid written for these tests) it is a
// real stack's router: it does not assist the return path, so the device
// behind it has to answer through the router on its own.
//
//	BACNET_STACK_ROUTER_IMAGE  default bacnet-interop-bacnet-stack-router:local
const (
	defaultStackRouterImage = "bacnet-interop-bacnet-stack-router:local"
	stackRouterAdapter      = "bacnet-stack-router"
	stackRouterFixture      = "topology-router-v1"
)

func stackRouterImage() string {
	return getEnv("BACNET_STACK_ROUTER_IMAGE", defaultStackRouterImage)
}

// startNativeRoutedTopology is startRoutedTopology with the bacnet-stack
// router in place of bip-router: client and router on net A (BACnet network
// 1), device and router on net B (BACnet network 2). Assertions run inside
// net A; the host only brings the topology up and re-executes the test there.
//
// deviceEnv, when not nil, returns extra KEY=VALUE pairs for the device
// container given the address and prefix length it gets on net B.
func startNativeRoutedTopology(t *testing.T, deviceImage, wantAdapter string, deviceEnv func(ip string, prefix int) []string) *routedTopology {
	t.Helper()
	if rt := routedTopologyFromEnv(t); rt != nil {
		return rt
	}
	requireDocker(t)
	requireImage(t, deviceImage)
	routerImage := stackRouterImage()
	requireImage(t, routerImage)

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		base := fmt.Sprintf("%s-n%d", dockerContainerName(t), attempt)
		netA := base + "-a"
		netB := base + "-b"
		deviceName := base + "-dev"
		routerName := base + "-rtr"
		subnetA, gwA, routerA, subnetB, gwB, routerB, deviceB := routedSubnetPair(base, attempt)

		cleanup := func() {
			dockerStop(deviceName)
			dockerStop(routerName)
			_ = exec.Command("docker", "rm", "-f", deviceName).Run()
			_ = exec.Command("docker", "rm", "-f", routerName).Run()
			_ = exec.Command("docker", "network", "rm", netA).Run()
			_ = exec.Command("docker", "network", "rm", netB).Run()
		}
		docker := func(what string, args ...string) error {
			if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("%s: %v (%s)", what, err, out)
			}
			return nil
		}

		err := func() error {
			if err := docker("create net A", "network", "create", "--subnet", subnetA, "--gateway", gwA, netA); err != nil {
				return err
			}
			if err := docker("create net B", "network", "create", "--subnet", subnetB, "--gateway", gwB, netB); err != nil {
				return err
			}
			devArgs := []string{"run", "-d", "--name", deviceName, "--network", netB, "--ip", deviceB}
			if deviceEnv != nil {
				for _, e := range deviceEnv(deviceB, 24) { // routedSubnetPair hands out /24s
					devArgs = append(devArgs, "-e", e)
				}
			}
			if err := docker("start device", append(devArgs, deviceImage)...); err != nil {
				return err
			}
			waitReadyLogs(t, deviceName, wantAdapter, loadDeviceFixture(t).Fixture, cleanup)

			// The router binds its ports by interface; its entrypoint finds
			// the interface that carries each address, so the BACnet network
			// numbers do not depend on Docker's eth0/eth1 order.
			if err := docker("create router", "create", "--name", routerName,
				"--network", netA, "--ip", routerA,
				"-e", fmt.Sprintf("BACNET_NETWORKS=%d,%d", topologyLocalNet, topologyRemoteNet),
				"-e", "BACNET_ADDR_NET1="+routerA,
				"-e", "BACNET_ADDR_NET2="+routerB,
				routerImage,
			); err != nil {
				return err
			}
			if err := docker("connect router to net B", "network", "connect", "--ip", routerB, netB, routerName); err != nil {
				return err
			}
			if err := docker("start router", "start", routerName); err != nil {
				return err
			}
			waitReadyLogs(t, routerName, stackRouterAdapter, stackRouterFixture, cleanup)
			// Let the docker dataplane settle before the client joins.
			time.Sleep(500 * time.Millisecond)

			if err := runReexecInNetwork(t, netA, "",
				"BACNET_INTEROP_ROUTER="+fmt.Sprintf("%s:47808", routerA),
				"BACNET_INTEROP_DEVICE_IP="+deviceB,
				"BACNET_INTEROP_DEVICE_PORT=47808",
				fmt.Sprintf("BACNET_INTEROP_REMOTE_NET=%d", topologyRemoteNet),
			); err != nil {
				devLogs, _ := exec.Command("docker", "logs", "--tail", "40", deviceName).CombinedOutput()
				rtrLogs, _ := exec.Command("docker", "logs", "--tail", "60", routerName).CombinedOutput()
				return fmt.Errorf("in-network re-exec: %w\ndevice logs:\n%s\nrouter logs:\n%s", err, devLogs, rtrLogs)
			}
			return nil
		}()
		if err != nil {
			lastErr = err
			t.Logf("native routed topology attempt %d/%d failed: %v", attempt, maxAttempts, err)
			cleanup()
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
			continue
		}
		t.Cleanup(cleanup)
		return &routedTopology{assertedByReexec: true, stop: cleanup}
	}
	t.Fatalf("native routed topology failed after %d attempts: %v", maxAttempts, lastErr)
	return nil
}

// tapTransport is a BACnet/IP UDP transport that keeps every datagram it
// receives, so a test can assert on the NPDU a router actually sent.
type tapTransport struct {
	conn  *net.UDPConn
	local bip.Endpoint

	mu     sync.Mutex
	closed bool
	seen   []client.InboundPacket
}

func newTapTransport(t *testing.T, port int) *tapTransport {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		t.Fatalf("listen UDP %d: %v", port, err)
	}
	return &tapTransport{
		conn:  conn,
		local: bip.NewEndpoint(netip.AddrPortFrom(netip.IPv4Unspecified(), uint16(port))),
	}
}

func (tt *tapTransport) Local() bip.Endpoint { return tt.local }

func (tt *tapTransport) Send(ctx context.Context, pkt client.OutboundPacket) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	addr := &net.UDPAddr{IP: net.IPv4bcast, Port: int(tt.local.Addr.Port())}
	if !pkt.Broadcast {
		a := pkt.Destination.Addr
		addr = &net.UDPAddr{IP: net.IP(a.Addr().AsSlice()), Port: int(a.Port())}
	}
	_, err := tt.conn.WriteToUDP(pkt.Data, addr)
	return err
}

func (tt *tapTransport) Recv(ctx context.Context) (client.InboundPacket, error) {
	buf := make([]byte, 65535)
	for {
		if err := ctx.Err(); err != nil {
			return client.InboundPacket{}, err
		}
		_ = tt.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, addr, err := tt.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				tt.mu.Lock()
				closed := tt.closed
				tt.mu.Unlock()
				if closed {
					return client.InboundPacket{}, net.ErrClosed
				}
				continue
			}
			return client.InboundPacket{}, err
		}
		ip, ok := netip.AddrFromSlice(addr.IP.To4())
		if !ok {
			continue
		}
		pkt := client.InboundPacket{
			Data:          append([]byte(nil), buf[:n]...),
			ImmediatePeer: bip.NewEndpoint(netip.AddrPortFrom(ip, uint16(addr.Port))),
			ReceivedAt:    time.Now(),
		}
		tt.mu.Lock()
		tt.seen = append(tt.seen, client.InboundPacket{
			Data: append([]byte(nil), pkt.Data...), ImmediatePeer: pkt.ImmediatePeer, ReceivedAt: pkt.ReceivedAt,
		})
		tt.mu.Unlock()
		return pkt, nil
	}
}

func (tt *tapTransport) Close() error {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	if tt.closed {
		return nil
	}
	tt.closed = true
	return tt.conn.Close()
}

// tappedNPDU is one received Original-Unicast/Broadcast-NPDU.
type tappedNPDU struct {
	from      bip.Endpoint
	broadcast bool // Original-Broadcast-NPDU
	npdu      npdu.NPDU
	raw       []byte
}

// npdus returns the NPDUs received so far from the given immediate peer.
func (tt *tapTransport) npdus(from bip.Endpoint) []tappedNPDU {
	tt.mu.Lock()
	seen := append([]client.InboundPacket(nil), tt.seen...)
	tt.mu.Unlock()
	limits := bacnet.DefaultDecodeLimits()
	var out []tappedNPDU
	for _, pkt := range seen {
		if !pkt.ImmediatePeer.Equal(from) {
			continue
		}
		msg, err := bvlc.Parse(pkt.Data, limits)
		if err != nil {
			continue
		}
		if msg.Function != bvlc.FunctionOriginalUnicastNPDU && msg.Function != bvlc.FunctionOriginalBroadcastNPDU {
			continue
		}
		n, _, err := npdu.Parse(msg.Payload, limits)
		if err != nil {
			continue
		}
		out = append(out, tappedNPDU{
			from:      pkt.ImmediatePeer,
			broadcast: msg.Function == bvlc.FunctionOriginalBroadcastNPDU,
			npdu:      n,
			raw:       pkt.Data,
		})
	}
	return out
}

type diagLog struct {
	mu   sync.Mutex
	list []client.Diagnostic
}

func (d *diagLog) add(ev client.Diagnostic) {
	d.mu.Lock()
	d.list = append(d.list, ev)
	d.mu.Unlock()
}

func (d *diagLog) dump(t *testing.T) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, ev := range d.list {
		t.Logf("client diagnostic: %s: %s %v", ev.Kind, ev.Message, ev.Fields)
	}
}

// Routing through a real stack's router, without return-path assistance:
// Who-Is-Router-To-Network answered by the router, a route resolved from that
// answer, routed ReadProperty and ReadPropertyMultiple, and a global Who-Is
// whose I-Am comes back through the router carrying the device's network
// address.
func TestBacnetStackNativeRouter(t *testing.T) {
	// The router image is released together with the bacnet-stack adapter:
	// an older bacnet-stack pin means no router image was published.
	requireAdapterVersion(t, stackPeer().image, "0.10.0")
	requireAdapterVersion(t, stackRouterImage(), "0.10.0")
	t.Run(stackPeer().name, func(t *testing.T) {
		runNativeRouter(t, stackPeer(), nil)
	})
	t.Run(bacpypes3Peer().name, func(t *testing.T) {
		// The router forwards a broadcast to the subnet broadcast address of
		// network 2. By default the BACpypes3 adapter binds its unicast
		// address only and never sees it; an address with a prefix length
		// makes BACpypes3 open the broadcast socket too.
		runNativeRouter(t, bacpypes3Peer(), func(ip string, prefix int) []string {
			return []string{fmt.Sprintf("BACNET_DEVICE_ADDRESS=%s/%d:47808", ip, prefix)}
		})
	})
}

func runNativeRouter(t *testing.T, device interopPeer, deviceEnv func(ip string, prefix int) []string) {
	dev := loadDeviceFixture(t)
	topo := startNativeRoutedTopology(t, device.image, device.name, deviceEnv)
	if topo.assertedByReexec {
		return
	}
	// The router answers Who-Is-Router-To-Network and forwards I-Am as
	// broadcasts to UDP 47808: the client must own that port.
	port := dev.Port
	if port == 0 {
		port = bip.DefaultPort
	}
	tap := newTapTransport(t, port)
	diags := &diagLog{}
	c := newClientOpts(t,
		client.WithTransport(tap),
		client.WithPort(port),
		client.WithDiagnosticFunc(diags.add),
	)
	t.Cleanup(func() {
		if t.Failed() {
			diags.dump(t)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	remote := topo.remoteNet
	deviceAddr := topo.remoteAddress()
	deviceID := deviceObject(dev)

	// 1. Who-Is-Router-To-Network as a local broadcast; the router's
	// I-Am-Router-To-Network must fill the route cache.
	if _, err := c.ResolveTarget(deviceAddr, bip.Endpoint{}); err == nil {
		t.Fatalf("a route to network %d is known before Who-Is-Router-To-Network", remote)
	}
	var target client.Target
	deadline := time.Now().Add(10 * time.Second)
	for attempt := 1; ; attempt++ {
		if err := c.WhoIsRouterToNetwork(ctx, &remote); err != nil {
			t.Fatalf("WhoIsRouterToNetwork: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
		resolved, err := c.ResolveTarget(deviceAddr, bip.Endpoint{})
		if err == nil {
			target = resolved
			t.Logf("route to network %d learned from the router on attempt %d", remote, attempt)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no route to network %d after Who-Is-Router-To-Network: %v (routes: %v)", remote, err, c.Routes(remote))
		}
	}
	if !target.Endpoint.Equal(topo.router) {
		t.Fatalf("next hop for network %d is %v, want the router %v", remote, target.Endpoint, topo.router)
	}
	if !target.Address.Equal(deviceAddr) {
		t.Fatalf("resolved address %v, want %v", target.Address, deviceAddr)
	}
	sawIAmRouter := false
	for _, f := range tap.npdus(topo.router) {
		if !f.npdu.NetworkMessage || f.npdu.NetMsgType != npdu.NetMsgIAmRouterToNetwork {
			continue
		}
		nets, err := npdu.DecodeNetworkList(f.npdu.NetMsgData)
		if err != nil {
			t.Fatalf("I-Am-Router-To-Network network list % x: %v", f.npdu.NetMsgData, err)
		}
		for _, n := range nets {
			if n == remote {
				sawIAmRouter = true
				t.Logf("I-Am-Router-To-Network %v from %v (broadcast=%v): % x", nets, f.from, f.broadcast, f.raw)
			}
		}
	}
	if !sawIAmRouter {
		t.Fatalf("no I-Am-Router-To-Network for network %d received from %v", remote, topo.router)
	}

	// 2. Routed confirmed services. The reply has to come back through the
	// router by the device's own doing.
	target.MaxAPDU = 1476
	val, err := c.ReadProperty(ctx, target, deviceID, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("routed ReadProperty: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("routed object-name = %q, want %q", name, dev.DeviceName)
	}
	av, wantPV := analogValueObject(dev)
	results, err := c.ReadPropertyMultiple(ctx, target, []service.ReadAccessSpecification{
		{Object: deviceID, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectIdentifier}}},
		{Object: av, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
	})
	if err != nil {
		t.Fatalf("routed ReadPropertyMultiple: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("routed RPM: %d results, want 2", len(results))
	}
	if id, err := bacnet.AsObjectID(findPropertyValue(t, results[0], bacnet.PropertyObjectIdentifier)); err != nil || id != deviceID {
		t.Fatalf("routed RPM object-identifier = %v (%v), want %v", id, err, deviceID)
	}
	if pv, err := bacnet.AsReal(findPropertyValue(t, results[1], bacnet.PropertyPresentValue)); err != nil || pv != wantPV {
		t.Fatalf("routed RPM present-value = %v (%v), want %v", pv, err, wantPV)
	}

	// 3. Global Who-Is. The router forwards it to network 2 and forwards the
	// device's I-Am back with SNET/SADR added.
	low, high := dev.DeviceInstance, dev.DeviceInstance
	var obs client.DeviceObservation
	found := false
	for attempt := 1; attempt <= 4 && !found; attempt++ {
		devices, err := c.Discover(ctx, client.DiscoveryOptions{LowLimit: &low, HighLimit: &high, Window: 1500 * time.Millisecond})
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		for _, d := range devices {
			if d.Instance == dev.DeviceInstance {
				obs, found = d, true
			}
		}
	}
	// The router forwards the global Who-Is to network 2 as a subnet-directed
	// broadcast. The BACnet4J adapter binds the device's unicast address and
	// receives the limited broadcast only (see TestBroadcastDiscover), so the
	// device never sees it. Asserted, so a change in the adapter is noticed.
	if device.name == "bacnet4j" {
		if found {
			t.Fatalf("BACnet4J answered a routed global Who-Is (%+v): the adapter now receives subnet broadcasts; assert the discovery like the other peers", obs)
		}
		t.Log("BACnet4J does not hear the routed global Who-Is (subnet-directed broadcast); routed requests work")
		return
	}
	if !found {
		t.Fatalf("device %d not discovered through the router; known devices: %v", dev.DeviceInstance, c.Devices())
	}
	if !obs.Address.Equal(deviceAddr) {
		t.Fatalf("discovered device address %v, want %v (network %d, MAC % x)",
			obs.Address, deviceAddr, remote, topo.deviceMAC().Bytes())
	}
	if !obs.ImmediatePeer.Equal(topo.router) {
		t.Fatalf("discovered device was heard from %v, want the router %v", obs.ImmediatePeer, topo.router)
	}

	// The I-Am as the router sent it.
	var iAm []tappedNPDU
	for _, f := range tap.npdus(topo.router) {
		if f.npdu.NetworkMessage || len(f.npdu.APDU) < 2 {
			continue
		}
		if apdu.PDUType(f.npdu.APDU[0]&0xF0) != apdu.TypeUnconfirmedRequest || f.npdu.APDU[1] != apdu.ServiceIAm {
			continue
		}
		iAm = append(iAm, f)
		t.Logf("I-Am from the router (broadcast=%v) DNET=%v SNET/SADR=%v hop count %d: % x",
			f.broadcast, f.npdu.Destination, f.npdu.Source, f.npdu.HopCount, f.raw)
	}
	if len(iAm) == 0 {
		t.Fatalf("no I-Am NPDU received from the router %v", topo.router)
	}
	withBoth := 0
	for _, f := range iAm {
		if !f.npdu.Source.Equal(deviceAddr) {
			t.Fatalf("I-Am source %v, want SNET/SADR %v", f.npdu.Source, deviceAddr)
		}
		if f.npdu.Destination.Scope() == bacnet.AddressGlobalBroadcast {
			withBoth++
		}
	}
	// bacnet-stack answers a Who-Is with a global-broadcast I-Am, so the
	// routed frame carries DNET 0xFFFF, the hop count, and SNET/SADR.
	// BACpypes3 answers the requester directly; the router delivers that
	// I-Am on the final network with SNET/SADR only.
	wantBoth := device.name == stackPeer().name
	if wantBoth && withBoth == 0 {
		t.Fatalf("no I-Am with both DNET (global broadcast) and SNET/SADR among %d routed I-Am frames", len(iAm))
	}
	if !wantBoth && withBoth != 0 {
		t.Fatalf("%d of %d routed I-Am frames carry a DNET; %s was seen to answer the requester directly", withBoth, len(iAm), device.name)
	}
	t.Logf("%d routed I-Am frame(s), %d with both DNET and SNET/SADR", len(iAm), withBoth)

	// The observation alone addresses the device.
	obsTarget := obs.Target()
	if !obsTarget.Endpoint.Equal(topo.router) || !obsTarget.Address.Equal(deviceAddr) {
		t.Fatalf("observation target %+v, want %v via %v", obsTarget, deviceAddr, topo.router)
	}
	val, err = c.ReadProperty(ctx, obsTarget, deviceID, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("ReadProperty with the discovered target: %v", err)
	}
	if name := characterString(t, val); name != dev.DeviceName {
		t.Fatalf("object-name via the discovered target = %q, want %q", name, dev.DeviceName)
	}
}
