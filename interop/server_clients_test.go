//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bbmd"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/server"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// Independent clients against the go-bacnet server: an adapter image in
// client mode (bacnet-interop docs/CLIENT_DRIVER_CONTRACT.md) runs the
// scenarios of a published scenario file against a go-bacnet server that
// serves the fixture the file requires, and reports one result line per
// scenario. The image compares what its stack decoded with the scenario's
// expectation; this test turns each line into a subtest. A scenario whose
// request has no reply (TimeSynchronization, UnconfirmedPrivateTransfer,
// WriteGroup) is checked on the server side: the hook must have run.
//
// The server runs in a Go container on the same docker network as the
// client image, on Linux as well as Docker Desktop: the bacnet-stack tools
// bind to the device by Who-Is on their own network.

const (
	serverReadyTimeout = 5 * time.Minute // includes compiling the test binary
	clientRunTimeout   = 10 * time.Minute
	serveEnv           = "BACNET_INTEROP_SERVE"
	serveFixtureEnv    = "BACNET_INTEROP_SERVE_FIXTURE"
	serveBBMDEnv       = "BACNET_INTEROP_SERVE_BBMD"
)

// clientRun is one client-mode run: a scenario file against a server
// serving its fixture.
type clientRun struct {
	scenarios string
	fixture   string
	bbmd      bool     // the server is a BBMD and the client registers with it
	args      []string // extra client arguments
}

var bacnetStackClientRuns = []clientRun{
	{scenarios: "server-baseline-v1", fixture: "device-baseline-v2"},
	{scenarios: "server-services-v1", fixture: "device-server-v1"},
	{scenarios: "server-bbmd-v1", fixture: "device-server-v1", bbmd: true},
}

// clientLine is one line of client-result-line-v1.
type clientLine struct {
	Event       string          `json:"event"`
	Mode        string          `json:"mode"`
	Adapter     string          `json:"adapter"`
	Version     string          `json:"version"`
	PeerVersion string          `json:"peer_version"`
	Scenarios   string          `json:"scenarios"`
	Scenario    string          `json:"scenario"`
	Service     string          `json:"service"`
	Outcome     string          `json:"outcome"`
	DurationMs  int             `json:"durationMs"`
	Observed    json.RawMessage `json:"observed"`
	Detail      string          `json:"detail"`
	Summary     map[string]int  `json:"summary"`
}

// scenarioFile is the part of a client-scenarios file this test reads.
type scenarioFile struct {
	ID              string `json:"id"`
	RequiresFixture string `json:"requires_fixture"`
	Scenarios       []struct {
		ID     string `json:"id"`
		Expect struct {
			Sent *struct {
				Operation string `json:"operation"`
			} `json:"sent"`
		} `json:"expect"`
	} `json:"scenarios"`
}

// The BACnet4J and BACpypes3 images run every scenario file; a file the
// image does not ship is skipped (requireClientMode).
var stackClientRuns = append(append([]clientRun(nil), bacnetStackClientRuns...),
	clientRun{scenarios: "server-extended-v1", fixture: "device-server-v1"})

func TestBACnetStackClientAgainstServer(t *testing.T) {
	runClientImage(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack", bacnetStackClientRuns)
}

func TestBACnet4JClientAgainstServer(t *testing.T) {
	runClientImage(t, getEnv("BACNET4J_IMAGE", defaultBACnet4JImage), "bacnet4j", stackClientRuns)
}

func TestBACpypes3ClientAgainstServer(t *testing.T) {
	runClientImage(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3", stackClientRuns)
}

// Legacy BACpypes (bacpypes 0.19.0, not BACpypes3): bacnet-interop adapter
// bacpypes. The minimum adapter release is checked in startBACpypes; here
// requireClientMode skips a file the image does not ship.
func TestBACpypesClientAgainstServer(t *testing.T) {
	requireAdapterVersion(t, bacpypesImage(), bacpypesMinAdapterVersion)
	runClientImage(t, bacpypesImage(), "bacpypes", stackClientRuns)
}

func runClientImage(t *testing.T, image, adapter string, runs []clientRun) {
	if image == "none" {
		t.Skipf("no %s image in the pinned bacnet-interop release", adapter)
	}
	requireDocker(t)
	requireImage(t, image)
	for _, run := range runs {
		t.Run(run.scenarios, func(t *testing.T) {
			requireClientMode(t, image, run.scenarios)
			runClientAgainstServer(t, image, adapter, run)
		})
	}
}

func runClientAgainstServer(t *testing.T, image, adapter string, run clientRun) {
	t.Helper()
	doc := loadScenarioFile(t, run.scenarios)
	if doc != nil && doc.RequiresFixture != run.fixture {
		t.Fatalf("%s requires %s, the run serves %s", run.scenarios, doc.RequiresFixture, run.fixture)
	}
	name := dockerContainerName(t)
	network := name + "-net"
	srv := name + "-server"
	t.Cleanup(func() {
		dockerStop(srv)
		_ = exec.Command("docker", "network", "rm", network).Run()
	})
	if out, err := exec.Command("docker", "network", "create", network).CombinedOutput(); err != nil {
		t.Fatalf("docker network create: %v (%s)", err, out)
	}
	env := []string{serveFixtureEnv + "=" + run.fixture}
	if os.Getenv(serveTraceEnv) != "" {
		env = append(env, serveTraceEnv+"=1")
	}
	if run.bbmd {
		env = append(env, serveBBMDEnv+"=1")
	}
	sc := startServerContainer(t, network, srv, env)

	args := []string{"run", "--rm", "--network", network, image,
		"client", "--target", sc.ip + ":47808", "--device", "1234",
		"--scenarios", "/scenarios/" + run.scenarios + ".json"}
	if run.bbmd {
		args = append(args, "--bbmd", sc.ip+":47808", "--fd-ttl", "60")
	}
	args = append(args, run.args...)
	ctx, cancel := context.WithTimeout(context.Background(), clientRunTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("client image: %v\n%s", err, out)
	}
	checkClientRun(t, parseClientLines(t, out), adapter, run.scenarios, doc, sc.operations)
}

// requireClientMode skips when the image predates client mode or the
// scenario file (a pinned release older than the contract). Like
// requireAdapterVersion it skips in required mode as well: an older pin is
// not a missing dependency.
func requireClientMode(t *testing.T, image, scenarios string) {
	t.Helper()
	err := exec.Command("docker", "run", "--rm", "--entrypoint", "test", image,
		"-f", "/scenarios/"+scenarios+".json").Run()
	if err != nil {
		t.Skipf("image %s has no client mode with %s", image, scenarios)
	}
}

type serverContainer struct {
	ip string
	mu sync.Mutex
	// ops are the operations the server's hooks reported.
	ops []string
}

func (s *serverContainer) operations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ops...)
}

// startServerContainer runs TestServeFixtureDevice in a Go container named
// name on network and returns once it serves.
func startServerContainer(t *testing.T, network, name string, env []string) *serverContainer {
	t.Helper()
	args, goImage, err := goContainerArgs(network)
	if err != nil {
		t.Fatalf("server container: %v", err)
	}
	args = append(args, "--name", name, "-e", serveEnv+"=1")
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, goImage,
		"go", "test", "-tags=interop", "-count=1", "-v", "-run", "^TestServeFixtureDevice$", "./interop/")
	cmd := exec.Command("docker", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server container: %v", err)
	}
	t.Cleanup(func() {
		dockerStop(name)
		_ = cmd.Wait()
	})
	sc := &serverContainer{}
	ready := make(chan struct{})
	go func() {
		s := bufio.NewScanner(stdout)
		signalled := false
		for s.Scan() {
			line := s.Text()
			if strings.HasPrefix(line, "trace ") {
				fmt.Fprintln(os.Stderr, line)
				continue
			}
			var ev operationEvent
			if json.Unmarshal([]byte(line), &ev) == nil && ev.Event == "operation" {
				sc.mu.Lock()
				sc.ops = append(sc.ops, ev.Operation)
				sc.mu.Unlock()
			}
			if !signalled && strings.Contains(line, `"event":"ready"`) {
				signalled = true
				close(ready)
			}
		}
		if !signalled {
			close(ready)
		}
	}()
	select {
	case <-ready:
	case <-time.After(serverReadyTimeout):
		t.Fatal("timed out waiting for the server container")
	}
	running, _ := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name).Output()
	if strings.TrimSpace(string(running)) != "true" {
		t.Fatal("server container exited before serving")
	}
	sc.ip = containerIPOnNetwork(t, name, network)
	return sc
}

// TestServeFixtureDevice serves a fixture until the container is stopped.
// It runs only inside the server container.
func TestServeFixtureDevice(t *testing.T) {
	if os.Getenv(serveEnv) == "" {
		t.Skip("runs inside the server container of TestBACnetStackClientAgainstServer")
	}
	fx := loadServerFixture(t, getEnv(serveFixtureEnv, "device-baseline-v2"))
	d := fixtureDevice(t, fx)
	emit := func(op string) {
		fmt.Printf(`{"event":"operation","adapter":"go-bacnet-server","operation":%q,"result":"accepted"}`+"\n", op)
	}
	opts := []server.Option{server.WithHooks(server.Hooks{
		TimeSync: func(_ service.TimeSynchronization, utc bool) {
			if utc {
				emit("utc-time-synchronization")
				return
			}
			emit("time-synchronization")
		},
		PrivateTransfer: func(_ context.Context, _ service.PrivateTransfer, confirmed bool) ([]bacnet.Element, error) {
			if confirmed {
				emit("confirmed-private-transfer")
			} else {
				emit("unconfirmed-private-transfer")
			}
			return nil, nil
		},
		TextMessage: func(_ context.Context, _ service.TextMessage, confirmed bool) error {
			if confirmed {
				emit("confirmed-text-message")
			} else {
				emit("unconfirmed-text-message")
			}
			return nil
		},
		WriteGroup: func(context.Context, service.WriteGroup) { emit("write-group") },
	})}
	if os.Getenv(serveBBMDEnv) != "" {
		self := ownEndpoint(t)
		b, err := bbmd.New(bbmd.Config{Self: self, BDT: []bvlc.BDTEntry{{Endpoint: self, Mask: bvlc.IPv4Mask{255, 255, 255, 255}}}})
		if err != nil {
			t.Fatal(err)
		}
		opts = append(opts, server.WithBBMD(b))
	}
	if os.Getenv(serveTraceEnv) != "" {
		tr, err := bip.ListenUDP(bip.UDPConfig{})
		if err != nil {
			t.Fatal(err)
		}
		opts = append(opts, server.WithTransport(traceTransport{tr}))
	}
	s, err := server.New(d, opts...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.IAm(); err != nil {
		t.Logf("I-Am: %v", err)
	}
	fmt.Printf(`{"event":"ready","adapter":"go-bacnet-server","fixture":%q,"address":%q}`+"\n", fx.Fixture, s.Local().String())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(clientRunTimeout + serverReadyTimeout):
	}
}

// ownEndpoint is this container's IPv4 address on port 47808, the BBMD's
// own B/IP address.
func ownEndpoint(t *testing.T) bip.Endpoint {
	t.Helper()
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
			continue
		}
		ip, _ := netip.AddrFromSlice(ipn.IP.To4())
		return bip.NewEndpoint(netip.AddrPortFrom(ip, 47808))
	}
	t.Fatal("no IPv4 address")
	return bip.Endpoint{}
}

// serverFixture is a bacnet-interop device fixture (fixtures/device) with
// the fields a server needs.
type serverFixture struct {
	Fixture        string `json:"fixture"`
	DeviceInstance uint32 `json:"device_instance"`
	DeviceName     string `json:"device_name"`
	Objects        []struct {
		Type             string   `json:"type"`
		Instance         uint32   `json:"instance"`
		ObjectName       string   `json:"object_name"`
		Description      string   `json:"description"`
		PresentValue     any      `json:"present_value"`
		Commandable      bool     `json:"commandable"`
		AccessMethod     string   `json:"access_method"`
		StreamDataBase64 string   `json:"stream_data_base64"`
		Records          []string `json:"records"`
		LogBuffer        []struct {
			Timestamp string  `json:"timestamp"`
			Real      float32 `json:"real"`
		} `json:"log_buffer"`
		IntrinsicReporting *struct {
			NotificationClass uint32  `json:"notification_class"`
			HighLimit         float32 `json:"high_limit"`
			LowLimit          float32 `json:"low_limit"`
			Deadband          float32 `json:"deadband"`
			TimeDelay         uint32  `json:"time_delay"`
		} `json:"intrinsic_reporting"`
	} `json:"objects"`
	ObjectLifecycle *struct {
		CreatableTypes []string `json:"creatable_types"`
	} `json:"object_lifecycle"`
}

func interopRoot(t *testing.T) string {
	t.Helper()
	modRoot, _ := filepath.Abs("..")
	root, err := resolveInteropRootFrom(modRoot, os.Getenv("BACNET_INTEROP_ROOT"))
	if err != nil {
		failOrSkip(t, "bacnet-interop checkout: %v", err)
	}
	return root
}

func loadServerFixture(t *testing.T, name string) serverFixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(interopRoot(t), "fixtures", "device", name+".json"))
	if err != nil {
		failOrSkip(t, "fixture %s: %v", name, err)
	}
	var fx serverFixture
	if err := json.Unmarshal(b, &fx); err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return fx
}

// fixtureDevice builds the fixture's object model. In device-baseline-v2,
// which predates the commandable field, AV-1 is commandable: the baseline
// scenarios write and relinquish it at priority 8.
func fixtureDevice(t *testing.T, fx serverFixture) *object.Device {
	t.Helper()
	d, err := object.NewDevice(object.DeviceConfig{
		Instance: fx.DeviceInstance, Name: fx.DeviceName, VendorID: 999, VendorName: "otfabric",
		ModelName: "go-bacnet server", FirmwareRevision: "interop", ApplicationSoftwareVersion: "interop",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range fx.Objects {
		var obj object.Object
		switch o.Type {
		case "device":
			continue
		case "analog-value":
			v, _ := o.PresentValue.(float64)
			cfg := object.PointConfig{Instance: o.Instance, Name: o.ObjectName, Description: o.Description,
				Units: 95, Value: float32(v), Commandable: o.Commandable || fx.Fixture == "device-baseline-v2"}
			if ir := o.IntrinsicReporting; ir != nil {
				cfg.Events = &object.EventConfig{
					NotificationClass: ir.NotificationClass,
					EventEnable:       object.EnableToOffNormal | object.EnableToFault | object.EnableToNormal,
					NotifyType:        object.NotifyAlarm,
					TimeDelay:         ir.TimeDelay,
					HighLimit:         ir.HighLimit, LowLimit: ir.LowLimit, Deadband: ir.Deadband,
					LimitEnable: object.LimitEnableHigh | object.LimitEnableLow,
				}
			}
			obj, err = object.NewAnalogValue(cfg)
		case "binary-value":
			obj, err = object.NewBinaryValue(object.PointConfig{Instance: o.Instance, Name: o.ObjectName,
				Description: o.Description, Active: o.PresentValue == "active"})
		case "trend-log":
			var tl *object.TrendLog
			tl, err = object.NewTrendLog(object.TrendLogConfig{Instance: o.Instance, Name: o.ObjectName,
				Description: o.Description, Enable: len(o.LogBuffer) > 0})
			for _, r := range o.LogBuffer {
				if err != nil {
					break
				}
				var at time.Time
				if at, err = time.ParseInLocation("2006-01-02T15:04:05", r.Timestamp, time.Local); err == nil {
					err = tl.Log(at, bacnet.RealValue(r.Real), nil)
				}
			}
			obj = tl
		case "notification-class":
			obj, err = object.NewNotificationClass(object.NotificationClassConfig{Instance: o.Instance,
				Name: o.ObjectName, Description: o.Description})
		case "file":
			cfg := object.FileConfig{Instance: o.Instance, Name: o.ObjectName, Description: o.Description,
				Record: o.AccessMethod == "record"}
			if cfg.Record {
				for _, r := range o.Records {
					cfg.Records = append(cfg.Records, []byte(r))
				}
			} else if cfg.Data, err = base64.StdEncoding.DecodeString(o.StreamDataBase64); err != nil {
				t.Fatalf("file %d: %v", o.Instance, err)
			}
			obj, err = object.NewFile(cfg)
		default:
			t.Fatalf("fixture object type %s is not served by this test", o.Type)
		}
		if err != nil {
			t.Fatalf("%s %d: %v", o.Type, o.Instance, err)
		}
		if err := d.Add(obj); err != nil {
			t.Fatalf("%s %d: %v", o.Type, o.Instance, err)
		}
	}
	if lc := fx.ObjectLifecycle; lc != nil {
		for _, ty := range lc.CreatableTypes {
			if ty != "analog-value" {
				t.Fatalf("creatable type %s is not served by this test", ty)
			}
			d.RegisterFactory(bacnet.ObjectTypeAnalogValue, func(instance uint32, name string) (object.Object, error) {
				av, err := object.NewAnalogValue(object.PointConfig{Instance: instance, Name: name, Units: 95})
				if err == nil {
					av.SetDeletable(true)
				}
				return av, err
			})
		}
	}
	return d
}

// loadScenarioFile reads a scenario file from the bacnet-interop checkout,
// so a scenario the image silently skipped is noticed. Without the file
// neither the count nor the server-side operations are checked.
func loadScenarioFile(t *testing.T, id string) *scenarioFile {
	t.Helper()
	modRoot, _ := filepath.Abs("..")
	root, err := resolveInteropRootFrom(modRoot, os.Getenv("BACNET_INTEROP_ROOT"))
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(root, "fixtures", "scenarios", id+".json"))
	if err != nil {
		return nil
	}
	var doc scenarioFile
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return &doc
}

func parseClientLines(t *testing.T, out []byte) []clientLine {
	t.Helper()
	var lines []clientLine
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var l clientLine
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatalf("client output is not a result line: %q", raw)
		}
		lines = append(lines, l)
	}
	return lines
}

// checkClientRun asserts the ready, result and done lines. pass passes;
// fail and infra-error fail; unsupported, not-tested and known-difference
// skip the subtest (never a pass). A passing scenario that expects a sent
// request also needs the server-side operation.
func checkClientRun(t *testing.T, lines []clientLine, adapter, scenarios string, doc *scenarioFile, ops func() []string) {
	t.Helper()
	if len(lines) < 2 || lines[0].Event != "ready" || lines[len(lines)-1].Event != "done" {
		t.Fatalf("client output must start with ready and end with done: %+v", lines)
	}
	ready, done := lines[0], lines[len(lines)-1]
	if ready.Mode != "client" || ready.Adapter != adapter || ready.Scenarios != scenarios {
		t.Errorf("ready: %+v", ready)
	}
	if interopRequired() && ready.Version == "dev" {
		t.Errorf("adapter version is %q under CI/required mode", ready.Version)
	}
	t.Logf("client %s %s (%s), scenarios %s", ready.Adapter, ready.Version, ready.PeerVersion, ready.Scenarios)
	sent := map[string]string{}
	if doc != nil {
		for _, s := range doc.Scenarios {
			if s.Expect.Sent != nil {
				sent[s.ID] = s.Expect.Sent.Operation
			}
		}
	}
	counts := map[string]int{}
	seen := map[string]bool{}
	for _, l := range lines[1 : len(lines)-1] {
		if l.Event != "result" {
			t.Errorf("unexpected line %+v", l)
			continue
		}
		counts[l.Outcome]++
		seen[l.Scenario] = true
		t.Run(l.Scenario, func(t *testing.T) {
			switch l.Outcome {
			case "pass":
				t.Logf("%s: pass, observed %s", l.Service, l.Observed)
				if op, ok := sent[l.Scenario]; ok && !waitOperation(ops, op, 5*time.Second) {
					t.Errorf("%s: the server did not report %s", l.Service, op)
				}
			case "fail":
				t.Errorf("%s: fail, observed %s", l.Service, l.Observed)
			case "infra-error":
				t.Errorf("%s: infra-error: %s", l.Service, l.Detail)
			case "unsupported", "not-tested", "known-difference":
				t.Skipf("%s: %s %s", l.Service, l.Outcome, l.Detail)
			default:
				t.Errorf("%s: unknown outcome %q", l.Service, l.Outcome)
			}
		})
	}
	for k, v := range done.Summary {
		if counts[k] != v {
			t.Errorf("done summary %s = %d, result lines %d", k, v, counts[k])
		}
	}
	if doc != nil {
		for _, s := range doc.Scenarios {
			if !seen[s.ID] {
				t.Errorf("scenario %s has no result line", s.ID)
			}
		}
	}
}

func waitOperation(ops func() []string, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		for _, op := range ops() {
			if op == want {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// serveTraceEnv, set for the server container, writes every datagram the
// server receives and sends to stderr in hex, for diagnosing a peer.
const serveTraceEnv = "BACNET_SERVE_TRACE"

type traceTransport struct{ bip.Transport }

func (t traceTransport) Recv(ctx context.Context) (bip.InboundPacket, error) {
	p, err := t.Transport.Recv(ctx)
	if err == nil {
		fmt.Fprintf(os.Stderr, "trace recv %s %x\n", p.ImmediatePeer, p.Data)
	}
	return p, err
}

func (t traceTransport) Send(ctx context.Context, p bip.OutboundPacket) error {
	err := t.Transport.Send(ctx, p)
	fmt.Fprintf(os.Stderr, "trace send %s broadcast=%v err=%v %x\n", p.Destination, p.Broadcast, err, p.Data)
	return err
}
