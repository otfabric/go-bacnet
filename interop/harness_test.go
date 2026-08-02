//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/client"
)

const (
	defaultStackImage     = "bacnet-interop-bacnet-stack:local"
	defaultBACpypes3Image = "bacnet-interop-bacpypes3:local"
	defaultBACnet4JImage  = "bacnet-interop-bacnet4j:local"
	defaultWorldietyImage = "bacnet-interop-worldiety:local"
	defaultRouterImage    = "bacnet-interop-bip-router:local"
	readyTimeout          = 45 * time.Second

	topologyLocalNet  uint16 = 1
	topologyRemoteNet uint16 = 2
)

type deviceFixture struct {
	Fixture        string `json:"fixture"`
	DeviceInstance uint32 `json:"device_instance"`
	DeviceName     string `json:"device_name"`
	Port           int    `json:"port"`
	Objects        []struct {
		Type         string `json:"type"`
		Instance     uint32 `json:"instance"`
		ObjectName   string `json:"object_name"`
		PresentValue any    `json:"present_value"`
		Description  string `json:"description"`
	} `json:"objects"`
}

func loadDeviceFixture(t *testing.T) deviceFixture {
	t.Helper()
	candidates := []string{os.Getenv("BACNET_DEVICE_FIXTURE")}
	if root := os.Getenv("BACNET_INTEROP_ROOT"); root != "" {
		candidates = append(candidates,
			filepath.Join(root, "fixtures", "device", "device-baseline-v2.json"),
			filepath.Join(root, "fixtures", "device", "device-baseline-v1.json"),
		)
	}
	candidates = append(candidates,
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v2.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v2.json"),
		// Fall back to v1 only when v2 is absent (older checkouts / pinned images).
		filepath.Join("..", "bacnet-interop", "fixtures", "device", "device-baseline-v1.json"),
		filepath.Join("..", "..", "bacnet-interop", "fixtures", "device", "device-baseline-v1.json"),
	)
	for _, p := range candidates {
		if p == "" {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var f deviceFixture
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatalf("device fixture %s: %v", p, err)
		}
		return f
	}
	failOrSkip(t, "device-baseline-v2.json not found; set BACNET_INTEROP_ROOT")
	return deviceFixture{}
}

func interopRequired() bool {
	return os.Getenv("BACNET_INTEROP_REQUIRED") != "" || os.Getenv("CI") != ""
}

func failOrSkip(t *testing.T, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	if interopRequired() {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

type readyEvent struct {
	Event   string `json:"event"`
	Address string `json:"address"`
	Fixture string `json:"fixture"`
	Adapter string `json:"adapter"`
	Version string `json:"version"`
}

type adapterReady struct {
	addr    string
	fixture string
	adapter string
	version string
}

type peerHandle struct {
	endpoint bip.Endpoint
	target   client.Target
	stop     func()
	// assertedByReexec is set when startPeer already ran this test inside a
	// docker network (Docker Desktop). Callers should return immediately.
	assertedByReexec bool
}

// routedTopology is a dual-network peer: client + router on net A, device on net B.
type routedTopology struct {
	router     bip.Endpoint
	remoteNet  uint16
	deviceIP   string
	devicePort int
	stop       func()
	// assertedByReexec is set when the host already re-ran this test in-network.
	assertedByReexec bool
}

func (rt *routedTopology) deviceMAC() bacnet.MAC {
	return bipMAC(rt.deviceIP, rt.devicePort)
}

func (rt *routedTopology) remoteAddress() bacnet.Address {
	return bacnet.RemoteStation(rt.remoteNet, rt.deviceMAC())
}

func bipMAC(ip string, port int) bacnet.MAC {
	addr := netip.MustParseAddr(ip).As4()
	return bacnet.MustMAC([]byte{addr[0], addr[1], addr[2], addr[3], byte(port >> 8), byte(port)})
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("BACNET_INTEROP_SKIP") != "" {
		if interopRequired() {
			t.Fatal("BACNET_INTEROP_SKIP set but BACNET_INTEROP_REQUIRED/CI forbids skips")
		}
		t.Skip("BACNET_INTEROP_SKIP set")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		failOrSkip(t, "docker not available")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		failOrSkip(t, "docker daemon not reachable")
	}
}

func requireImage(t *testing.T, image string) {
	t.Helper()
	if err := exec.Command("docker", "image", "inspect", image).Run(); err != nil {
		failOrSkip(t, "image %s not present; build with make -C ../bacnet-interop build", image)
	}
}

func dockerContainerName(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("bacnet-interop-%d-%s", os.Getpid(), t.Name())
	return strings.NewReplacer("/", "_", " ", "_", "(", "", ")", "").Replace(name)
}

func dockerStop(name string) {
	_ = exec.Command("docker", "stop", "-t", "2", name).Run()
}

func validateAdapterMeta(t *testing.T, m adapterReady, wantAdapter string, wantFixture string) {
	t.Helper()
	if m.fixture != wantFixture {
		t.Errorf("adapter fixture: got %q, want %q", m.fixture, wantFixture)
	}
	if m.adapter != wantAdapter {
		t.Errorf("adapter name: got %q, want %q", m.adapter, wantAdapter)
	}
	if m.version == "" {
		t.Error("adapter version is empty")
	}
	if interopRequired() && m.version == "dev" {
		t.Errorf("adapter version is %q under CI/required mode; pass ADAPTER_VERSION build-arg", m.version)
	}
}

func targetFor(ip string, port int) (bip.Endpoint, client.Target) {
	ep := bip.NewEndpoint(netip.AddrPortFrom(netip.MustParseAddr(ip), uint16(port)))
	addr := netip.MustParseAddr(ip).As4()
	mac := []byte{addr[0], addr[1], addr[2], addr[3], byte(port >> 8), byte(port)}
	return ep, client.Target{
		Address:  bacnet.LocalStation(bacnet.MustMAC(mac)),
		Endpoint: ep,
		MaxAPDU:  1476,
	}
}

func peerFromEnv(t *testing.T) *peerHandle {
	t.Helper()
	epEnv := os.Getenv("BACNET_INTEROP_ENDPOINT")
	if epEnv == "" {
		return nil
	}
	ap, err := netip.ParseAddrPort(epEnv)
	if err != nil {
		t.Fatalf("BACNET_INTEROP_ENDPOINT: %v", err)
	}
	ep, target := targetFor(ap.Addr().String(), int(ap.Port()))
	return &peerHandle{endpoint: ep, target: target, stop: func() {}}
}

func waitReady(t *testing.T, stdout io.Reader, wantAdapter, wantFixture string, stop func()) {
	t.Helper()
	ready := make(chan adapterReady, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			var ev readyEvent
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Event == "ready" {
				ready <- adapterReady{
					addr:    ev.Address,
					fixture: ev.Fixture,
					adapter: ev.Adapter,
					version: ev.Version,
				}
				break
			}
		}
		_ = scanner.Err()
		_, _ = io.Copy(io.Discard, stdout)
		close(ready)
	}()

	startCtx, startCancel := context.WithTimeout(context.Background(), readyTimeout)
	defer startCancel()

	select {
	case m, ok := <-ready:
		if !ok {
			stop()
			t.Fatal("adapter exited before emitting readiness event")
		}
		validateAdapterMeta(t, m, wantAdapter, wantFixture)
	case <-startCtx.Done():
		stop()
		t.Fatal("timed out waiting for adapter readiness")
	}
}

// startPeer starts an adapter on a dedicated docker network.
//
// On Linux the host can usually reach the container IP. On Docker Desktop
// (macOS/Windows) bridge IPs are not host-routable and UDP port-publish return
// paths are unreliable for BACnet, so the test is re-executed inside the peer
// network with BACNET_INTEROP_ENDPOINT set.
//
// env is optional KEY=VALUE pairs passed to the adapter container (for example
// BACNET_MAX_APDU=50 for segmented-response stress).
func startPeer(t *testing.T, image, wantAdapter string, env ...string) *peerHandle {
	t.Helper()
	if p := peerFromEnv(t); p != nil {
		return p
	}
	requireDocker(t)
	requireImage(t, image)

	containerName := dockerContainerName(t)
	networkName := containerName + "-net"
	t.Cleanup(func() {
		dockerStop(containerName)
		_ = exec.Command("docker", "network", "rm", networkName).Run()
	})
	if out, err := exec.Command("docker", "network", "create", networkName).CombinedOutput(); err != nil {
		t.Fatalf("docker network create: %v (%s)", err, out)
	}

	cmdCtx, cmdCancel := context.WithCancel(context.Background())
	runArgs := []string{
		"run", "--rm",
		"--name", containerName,
		"--network", networkName,
	}
	for _, e := range env {
		runArgs = append(runArgs, "-e", e)
	}
	runArgs = append(runArgs, image)
	cmd := exec.CommandContext(cmdCtx, "docker", runArgs...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cmdCancel()
		t.Fatalf("stdout pipe: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		cmdCancel()
		t.Fatalf("start adapter: %v", err)
	}
	stop := func() {
		cmdCancel()
		dockerStop(containerName)
		_ = cmd.Wait()
		_ = exec.Command("docker", "network", "rm", networkName).Run()
	}
	dev := loadDeviceFixture(t)
	waitReady(t, stdout, wantAdapter, dev.Fixture, stop)

	ipOut, err := exec.Command("docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", containerName).Output()
	if err != nil {
		stop()
		t.Fatalf("docker inspect IP: %v", err)
	}
	containerIP := strings.TrimSpace(string(ipOut))
	if containerIP == "" {
		stop()
		t.Fatal("adapter container has no IP address")
	}

	if runtime.GOOS == "linux" {
		ep, target := targetFor(containerIP, 47808)
		t.Cleanup(stop)
		return &peerHandle{endpoint: ep, target: target, stop: stop}
	}

	t.Cleanup(stop)
	reexecInNetwork(t, networkName, containerIP)
	return &peerHandle{assertedByReexec: true, stop: stop}
}

func reexecInNetwork(t *testing.T, network, peerIP string, extraEnv ...string) {
	t.Helper()
	if err := runReexecInNetwork(t, network, peerIP, extraEnv...); err != nil {
		t.Fatalf("in-network re-exec: %v", err)
	}
}

// runReexecInNetwork is like reexecInNetwork but returns the error so callers
// can retry topology bring-up under flaky docker dataplanes.
func runReexecInNetwork(t *testing.T, network, peerIP string, extraEnv ...string) error {
	t.Helper()
	modRoot, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(modRoot, "go.mod")); err != nil {
		modRoot, err = filepath.Abs(".")
		if err != nil {
			return err
		}
	}
	goVer := strings.TrimPrefix(runtime.Version(), "go")
	parts := strings.Split(goVer, ".")
	if len(parts) >= 2 {
		goVer = parts[0] + "." + parts[1]
	}
	image := getEnv("BACNET_INTEROP_GO_IMAGE", "golang:"+goVer)
	interopRoot, err := resolveInteropRootFrom(modRoot, os.Getenv("BACNET_INTEROP_ROOT"))
	if err != nil {
		return err
	}
	cacheRoot := filepath.Join(os.TempDir(), "go-bacnet-interop-cache")
	_ = os.MkdirAll(filepath.Join(cacheRoot, "go"), 0o755)
	_ = os.MkdirAll(filepath.Join(cacheRoot, "mod"), 0o755)
	args := []string{
		"run", "--rm",
		"--network", network,
		"-v", modRoot + ":/src",
		"-v", interopRoot + ":/bacnet-interop:ro",
		"-v", filepath.Join(cacheRoot, "go") + ":/tmp/gocache",
		"-v", filepath.Join(cacheRoot, "mod") + ":/tmp/gomodcache",
		"-w", "/src",
		"-e", "BACNET_INTEROP_ROOT=/bacnet-interop",
		"-e", "GOWORK=off",
		"-e", "GOCACHE=/tmp/gocache",
		"-e", "GOMODCACHE=/tmp/gomodcache",
	}
	if peerIP != "" {
		args = append(args, "-e", "BACNET_INTEROP_ENDPOINT="+fmt.Sprintf("%s:47808", peerIP))
	}
	for _, e := range extraEnv {
		args = append(args, "-e", e)
	}
	if interopRequired() {
		args = append(args, "-e", "BACNET_INTEROP_REQUIRED=1")
		if os.Getenv("CI") != "" {
			args = append(args, "-e", "CI=1")
		}
	}
	args = append(args,
		image,
		"go", "test", "-tags=interop", "-count=1", "-v",
		"-run", "^"+t.Name()+"$",
		"./interop/",
	)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	t.Logf("re-exec in network %s via %s peerIP=%s extra=%v", network, image, peerIP, extraEnv)
	return cmd.Run()
}

func containerIPOnNetwork(t *testing.T, container, network string) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "-f",
		fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network),
		container).Output()
	if err != nil {
		t.Fatalf("docker inspect IP %s/%s: %v", container, network, err)
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" {
		t.Fatalf("container %s has no IP on %s", container, network)
	}
	return ip
}

func waitReadyLogs(t *testing.T, container, wantAdapter, wantFixture string, stop func()) {
	t.Helper()
	deadline := time.Now().Add(readyTimeout)
	var last string
	for time.Now().Before(deadline) {
		out, _ := exec.Command("docker", "logs", container).CombinedOutput()
		last = string(out)
		for _, line := range strings.Split(last, "\n") {
			var ev readyEvent
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Event == "ready" {
				validateAdapterMeta(t, adapterReady{
					addr:    ev.Address,
					fixture: ev.Fixture,
					adapter: ev.Adapter,
					version: ev.Version,
				}, wantAdapter, wantFixture)
				return
			}
		}
		running, _ := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", container).Output()
		if strings.TrimSpace(string(running)) != "true" {
			stop()
			t.Fatalf("container %s exited before ready; logs:\n%s", container, last)
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	t.Fatalf("timed out waiting for %s ready; logs:\n%s", container, last)
}

func routedTopologyFromEnv(t *testing.T) *routedTopology {
	t.Helper()
	routerEnv := os.Getenv("BACNET_INTEROP_ROUTER")
	deviceIP := os.Getenv("BACNET_INTEROP_DEVICE_IP")
	if routerEnv == "" || deviceIP == "" {
		return nil
	}
	ap, err := netip.ParseAddrPort(routerEnv)
	if err != nil {
		t.Fatalf("BACNET_INTEROP_ROUTER: %v", err)
	}
	remoteNet := topologyRemoteNet
	if v := os.Getenv("BACNET_INTEROP_REMOTE_NET"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 || n > 65535 {
			t.Fatalf("BACNET_INTEROP_REMOTE_NET: %q", v)
		}
		remoteNet = uint16(n)
	}
	port := 47808
	if v := os.Getenv("BACNET_INTEROP_DEVICE_PORT"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &port); err != nil || port <= 0 {
			t.Fatalf("BACNET_INTEROP_DEVICE_PORT: %q", v)
		}
	}
	return &routedTopology{
		router:     bip.NewEndpoint(ap),
		remoteNet:  remoteNet,
		deviceIP:   deviceIP,
		devicePort: port,
		stop:       func() {},
	}
}

// routedSubnetPair returns deterministic, non-overlapping /24s and static host
// addresses for one routed-topology attempt. Docker's eth0/eth1 order is not
// stable across create+connect, so the router must bind by address with an
// explicit BACnet network number rather than assuming eth0=net1.
func routedSubnetPair(base string, attempt int) (subnetA, gwA, routerA, subnetB, gwB, routerB, deviceB string) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(base))
	v := h.Sum32() ^ uint32(attempt)*0x9e3779b9
	a := 20 + int(v%200) // 10.20.0.0/24 .. 10.219.0.0/24
	b := 20 + int((v>>9)%200)
	if b == a {
		b = 20 + (b+1)%200
	}
	subnetA = fmt.Sprintf("10.%d.0.0/24", a)
	gwA = fmt.Sprintf("10.%d.0.1", a)
	routerA = fmt.Sprintf("10.%d.0.2", a)
	subnetB = fmt.Sprintf("10.%d.0.0/24", b)
	gwB = fmt.Sprintf("10.%d.0.1", b)
	routerB = fmt.Sprintf("10.%d.0.2", b)
	deviceB = fmt.Sprintf("10.%d.0.3", b)
	return subnetA, gwA, routerA, subnetB, gwB, routerB, deviceB
}

// startRoutedTopology brings up net A (client+router) and net B (device+router).
// Assertions always run inside net A so Who-Is-Router local broadcast works on
// Linux and Docker Desktop alike.
//
// The bip-router is started with explicit address→network bindings (not eth0/
// eth1 order). Docker Desktop occasionally drops the first UDP flows after
// network create; the host retries the full topology a few times before failing.
func startRoutedTopology(t *testing.T, deviceImage, wantAdapter string, deviceEnv ...string) *routedTopology {
	t.Helper()
	if rt := routedTopologyFromEnv(t); rt != nil {
		return rt
	}
	requireDocker(t)
	requireImage(t, deviceImage)
	routerImage := getEnv("BIP_ROUTER_IMAGE", defaultRouterImage)
	requireImage(t, routerImage)

	const maxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		base := fmt.Sprintf("%s-%d", dockerContainerName(t), attempt)
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

		if err := func() error {
			if out, err := exec.Command("docker", "network", "create",
				"--subnet", subnetA, "--gateway", gwA, netA,
			).CombinedOutput(); err != nil {
				return fmt.Errorf("docker network create %s: %v (%s)", netA, err, out)
			}
			if out, err := exec.Command("docker", "network", "create",
				"--subnet", subnetB, "--gateway", gwB, netB,
			).CombinedOutput(); err != nil {
				return fmt.Errorf("docker network create %s: %v (%s)", netB, err, out)
			}

			devArgs := []string{"run", "-d", "--name", deviceName,
				"--network", netB, "--ip", deviceB,
			}
			for _, e := range deviceEnv {
				devArgs = append(devArgs, "-e", e)
			}
			devArgs = append(devArgs, deviceImage)
			if out, err := exec.Command("docker", devArgs...).CombinedOutput(); err != nil {
				return fmt.Errorf("start device: %v (%s)", err, out)
			}
			dev := loadDeviceFixture(t)
			waitReadyLogs(t, deviceName, wantAdapter, dev.Fixture, cleanup)

			// Bypass entrypoint eth0/eth1 mapping: bind BACnet net 1 to the
			// client-facing address and net 2 to the device-facing address.
			port1 := fmt.Sprintf("name=net1,network=%d,addr=%s,port=47808", topologyLocalNet, routerA)
			port2 := fmt.Sprintf("name=net2,network=%d,addr=%s,port=47808", topologyRemoteNet, routerB)
			if out, err := exec.Command("docker", "create",
				"--name", routerName,
				"--network", netA,
				"--ip", routerA,
				"--entrypoint", "python3",
				routerImage,
				"/usr/local/bin/bip_router.py",
				"--port", port1,
				"--port", port2,
			).CombinedOutput(); err != nil {
				return fmt.Errorf("create router: %v (%s)", err, out)
			}
			if out, err := exec.Command("docker", "network", "connect",
				"--ip", routerB, netB, routerName,
			).CombinedOutput(); err != nil {
				return fmt.Errorf("connect router to net B: %v (%s)", err, out)
			}
			if out, err := exec.Command("docker", "start", routerName).CombinedOutput(); err != nil {
				return fmt.Errorf("start router: %v (%s)", err, out)
			}
			waitReadyLogs(t, routerName, "bip-router", "topology-router-v1", cleanup)

			// Allow startup I-Am-Router broadcasts and docker dataplane to settle
			// before the in-network client joins (Docker Desktop / GHA bridges
			// occasionally drop the first UDP flows after network create).
			time.Sleep(500 * time.Millisecond)

			if err := runReexecInNetwork(t, netA, "",
				"BACNET_INTEROP_ROUTER="+fmt.Sprintf("%s:47808", routerA),
				"BACNET_INTEROP_DEVICE_IP="+deviceB,
				"BACNET_INTEROP_DEVICE_PORT=47808",
				fmt.Sprintf("BACNET_INTEROP_REMOTE_NET=%d", topologyRemoteNet),
			); err != nil {
				devLogs, _ := exec.Command("docker", "logs", "--tail", "40", deviceName).CombinedOutput()
				rtrLogs, _ := exec.Command("docker", "logs", "--tail", "40", routerName).CombinedOutput()
				return fmt.Errorf("in-network re-exec: %w\ndevice logs:\n%s\nrouter logs:\n%s",
					err, devLogs, rtrLogs)
			}
			return nil
		}(); err != nil {
			lastErr = err
			t.Logf("routed topology attempt %d/%d failed: %v", attempt, maxAttempts, err)
			cleanup()
			time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
			continue
		}

		t.Cleanup(cleanup)
		return &routedTopology{assertedByReexec: true, stop: cleanup}
	}
	t.Fatalf("routed topology failed after %d attempts: %v", maxAttempts, lastErr)
	return nil
}

func newClient(t *testing.T) *client.Client {
	t.Helper()
	return newClientOpts(t)
}

// newClientWithAdvertisedMaxAPDU advertises a small MaxAPDU in confirmed
// requests so peers segment ComplexACK responses, while keeping the default
// decode limit so oversized-but-common peer segments still parse.
func newClientWithAdvertisedMaxAPDU(t *testing.T, maxAPDU uint16) *client.Client {
	t.Helper()
	return newClientOpts(t, client.WithAdvertisedMaxAPDU(maxAPDU))
}

func newClientOpts(t *testing.T, opts ...client.Option) *client.Client {
	t.Helper()
	base := []client.Option{
		client.WithLocalAddr("0.0.0.0:0"),
		client.WithTransactionOptions(5*time.Second, 1, 2*time.Second),
	}
	base = append(base, opts...)
	c, err := client.New(base...)
	if err != nil {
		t.Fatalf("client.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// newDiscoveryClient binds the standard BACnet/IP port so broadcast I-Am
// responses from bacserv are receivable inside the isolated docker network.
func newDiscoveryClient(t *testing.T) *client.Client {
	t.Helper()
	dev := loadDeviceFixture(t)
	port := dev.Port
	if port == 0 {
		port = bip.DefaultPort
	}
	c, err := client.New(
		client.WithLocalAddr(fmt.Sprintf("0.0.0.0:%d", port)),
		client.WithPort(port),
		client.WithTransactionOptions(5*time.Second, 1, 2*time.Second),
	)
	if err != nil {
		t.Fatalf("client.New discovery: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func characterString(t *testing.T, v bacnet.ApplicationValue) string {
	t.Helper()
	if v.Kind != bacnet.ValueCharacterString {
		t.Fatalf("value kind %v, want CharacterString", v.Kind)
	}
	return v.Character.Value
}
