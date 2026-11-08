// SPDX-License-Identifier: MIT

package capture

import (
	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// logBuffer selects ReadRange answers holding records of the given object
// type's Log_Buffer.
func logBuffer(ot bacnet.ObjectType) func(Message) bool {
	return func(m Message) bool {
		ack, ok := m.Service.(service.ReadRangeACK)
		return ok && ack.Object.Type == ot && ack.Property.Identifier == bacnet.PropertyLogBuffer && ack.ItemCount > 0
	}
}

// The capture sources and golden cases TestGenerate builds. Stations are
// Endpoint strings: the B/IP node and the NPDU address of the device.

const (
	fetchedDate = "2026-11-08"
	kargsTerms  = "No licence or redistribution statement on the site; the files are committed byte-identical."
)

var sources = []ManifestGroup{{
	ID:       "kargs",
	URL:      "https://kargs.net/captures/",
	Revision: "directory listing of 2026-11-08 (217 files)",
	Fetched:  fetchedDate,
	Terms:    kargsTerms,
	Notes: "Steve Karg's collection of BACnet captures (BACnet Stack author; ASHRAE SSPC 135 member): plugfest and field recordings of vendor devices, " +
		"and test traffic of the BACnet Stack demo applications.",
}, {
	ID:       "icspcaps",
	URL:      "https://raw.githubusercontent.com/EmreEkin/ICS-Pcaps/0ffbf023afb4e39373228b54851bb4a1f33a3ab8/BacNET/",
	Revision: "commit 0ffbf023afb4e39373228b54851bb4a1f33a3ab8",
	Fetched:  fetchedDate,
	Terms:    "No licence file in the repository; the files are committed byte-identical.",
	Notes:    "github.com/EmreEkin/ICS-Pcaps BacNET/: 16 of the kargs.net captures re-saved as pcap, and bacnet_test.pcap.",
}}

var cases = []caseSpec{
	{
		ID: "trane-plugfest-2011", File: "plugfest-2011-trane-1.pcap", PerKind: 2,
		Summary: "BACnet Plugfest 2011: a Trane Tracer SC system controller, the Trane UC400 controller it routes to (network 31) and a Trane BSV, read by a client",
		Devices: map[string]string{
			"172.16.2.10:47808|local:":    "system controller and router to network 31",
			"172.16.2.10:47808|net=31:03": "controller behind the Tracer SC",
			"172.16.2.100:47808|local:":   "device answering reads",
		},
	},
	{
		ID: "tridium-plugfest", File: "plugfest-tridium-1.pcap", PerKind: 2,
		Summary: "Plugfest: NiagaraAX Station 3.6.38 and the NiagaraAX BACnet Advanced Workstation 3.7.18 with a PolarSoft server routing to a WattStopper LMBC-300",
		Devices: map[string]string{
			"172.16.36.205:47808|local:":    "NiagaraAX Station (Tridium software)",
			"172.16.36.1:47808|local:":      "NiagaraAX workstation (Tridium software)",
			"172.16.86.1:47808|local:":      "PolarSoft server, router to network 8601",
			"172.16.86.1:47808|net=8601:52": "WattStopper LMBC-300 lighting controller on network 8601",
		},
	},
	{
		ID: "reliable-controls-routed", File: "TestRun8 - Outside of Router.pcap", PerKind: 2,
		Summary: "Reliable Controls MACH-ProSys and an ETHER-Link router to a MACH2 on network 1111: routed reads and three-segment answers acknowledged one by one",
		Devices: map[string]string{
			"192.168.21.100:47808|local:":     "MACH-ProSys controller",
			"192.168.21.21:47808|local:":      "ETHER-Link router",
			"192.168.21.21:47808|net=1111:03": "MACH2 controller behind the ETHER-Link",
		},
	},
	{
		ID: "delta-plugfest", File: "plugfest-delta-2b.cap", PerKind: 1,
		Summary: "Plugfest: Delta Controls DSC-1616E routing to DNT-T103 and DNT-T221 on network 802, an OWS340 workstation, a Lithonia SYSC MLX; segmented answers and requests with windows 1 and 2",
		Devices: map[string]string{
			"172.16.8.10:47808|local:":     "DSC-1616E controller, router to network 802",
			"172.16.8.10:47808|net=802:0f": "DNT-T103 thermostat on network 802",
			"172.16.8.10:47808|net=802:0a": "DNT-T221 thermostat on network 802",
			"172.16.42.2:47808|local:":     "Lithonia SYSC MLX lighting controller",
		},
	},
	{
		ID: "alerton-plugfest", File: "alerton-plugfest-2.cap", PerKind: 1,
		Summary: "Plugfest: Alerton BCM-Eth routing to VLC-853 and VAV-SD controllers, the Envision for BACtalk workstation, and a Lithonia SYSC MLX answering in up to nine segments acknowledged one by one",
		Devices: map[string]string{
			"172.16.18.1:47808|local:":       "BCM-Eth controller, router",
			"172.16.18.1:47808|net=18100:01": "VLC-853 controller on network 18100",
			"172.16.18.200:47808|local:":     "Envision for BACtalk workstation",
			"172.16.42.2:47808|local:":       "Lithonia SYSC MLX lighting controller",
		},
	},
	{
		ID: "lithonia-btl-plugfest", File: "btl-plugfest.cap", PerKind: 2, Frames: []int{303, 304},
		Summary: "BTL plugfest: test clients against a Lithonia SYSC MLX 2x63w, including a ReadPropertyMultiple with an empty list of property references",
		Devices: map[string]string{"172.16.42.9:47808|local:": "Lithonia SYSC MLX lighting controller under test"},
		Differences: []Difference{
			{Frames: []int{303}, Classification: "intentional implementation difference",
				Detail: "the BTL test client asks ReadPropertyMultiple for AI-0 with an empty listOfPropertyReferences; Wireshark decodes it and the SYSC MLX answers an empty listOfResults (frame 304); go-bacnet's decoder refuses it and its server answers Reject invalid-tag"},
			{Frames: []int{423}, Classification: "intentional implementation difference",
				Detail: "the test client sends max-APDU code 6, reserved by clause 20.1.2.5; Wireshark decodes it, the SYSC MLX answers Abort other (frame 424), go-bacnet refuses the APDU and its server drops it"},
		},
	},
	{
		ID: "siemens-plugfest-2011", File: "plugfest-2011-siemens-1.pcap", PerKind: 2,
		Summary: "BACnet Plugfest 2011: Siemens devices and a WattStopper LMBC-300 behind a PolarSoft router; Siemens ConfirmedPrivateTransfer 511",
		Devices: map[string]string{
			"172.16.86.1:47808|net=8601:3f": "WattStopper LMBC-300 lighting controller on network 8601",
		},
	},
	{
		ID: "sauter-plugfest-2011", File: "plugfest-2011-sauter-1.pcap", PerKind: 1,
		Summary: "BACnet Plugfest 2011: Fr. Sauter AG devices (vendor 80) with a PolarSoft 9260 server routing to a WattStopper LMBC-300",
		Devices: map[string]string{
			"172.16.86.1:47808|local:":      "PolarSoft 9260 Linux server, router to network 8601",
			"172.16.86.1:47808|net=8601:3f": "WattStopper LMBC-300 lighting controller on network 8601",
		},
	},
	{
		ID: "trend-log-multiple", File: "TrendLogMultipleReadRangeSimple.pcap", PerKind: 2, Frames: []int{1, 2, 714, 799},
		Summary:    "ReadRange of a Trend Log Multiple's Log_Buffer (BACnetLogMultipleRecord) at a PolarSoft device and of a Trend Log at an Automated Logic device",
		ClassBasis: "vendor IDs in I-Am only (4 PolarSoft, 24 Automated Logic); model not in the traffic",
		Class:      "B",
		Regression: logBuffer(bacnet.ObjectTypeTrendLogMultiple),
		RegressionDetail: "service.DecodeReadRangeACK decoded every Log_Buffer as BACnetLogRecord and refused a Trend Log Multiple's " +
			"BACnetLogMultipleRecords (\"not context boolean\"), so client.ReadRange failed on them; now LogMultipleRecords",
	},
	{
		ID: "event-log", File: "eventLog_ReadRange.pcap", PerKind: 2,
		Summary:    "ReadRange of an Event Log's Log_Buffer (BACnetEventLogRecord with event notifications)",
		Class:      "B",
		ClassBasis: "no I-Am in this file; 192.168.1.104 announces vendor 24 (Automated Logic) in TrendLogMultipleReadRangeSimple.pcap of the same lab network",
		Regression: logBuffer(bacnet.ObjectTypeEventLog),
		RegressionDetail: "service.DecodeReadRangeACK decoded every Log_Buffer as BACnetLogRecord and refused an Event Log's " +
			"BACnetEventLogRecords (\"not context boolean\"); now EventLogRecords with the decoded notifications",
	},
	{
		ID: "critical-room-control", File: "bip-readprop.cap", PerKind: 2,
		Summary: "Critical Room Control Room Pressure Monitor 5.5 on network 26001, read through a router",
		Devices: map[string]string{"192.168.0.24:47808|net=26001:19": "Room Pressure Monitor on network 26001"},
	},
	{
		ID: "router-drops-apdu-type", File: "CriticalRoom55-1.cap", PerKind: 3,
		Summary: "A router at a plugfest forwards answers from network 26001 without their first APDU octet",
		Differences: []Difference{
			{Frames: []int{8, 10, 12}, Classification: "peer defect",
				Detail: "the router forwards a ComplexACK from network 26001 without its first octet (PDU type 0x30): the APDU starts with the invoke ID. Wireshark reads it as a Reject with an unknown reason, go-bacnet refuses it (a Reject is three octets)"},
		},
	},
	{
		ID: "wattstopper-weblink", File: "DRI CAVE log udp-0168-20081216-1117-03.cap", PerKind: 2,
		Summary: "WattStopper WebLink router (instance 86000): answers from behind it carry an NPDU with SNET after the hop count",
		Devices: map[string]string{"10.10.14.130:47808|local:": "WebLink router"},
		Differences: []Difference{
			{Frames: []int{55, 60}, Classification: "peer defect",
				Detail: "the NPDU's control octet announces only DNET, but the router puts the hop count after DADR and SNET/SLEN/SADR and a second hop count after it; read as the control octet says, the APDU starts 00 56 (a ConfirmedRequest with reserved max-APDU code 6), which Wireshark shows and go-bacnet refuses. The octets that follow (60 xx 09) are a Reject unrecognized-service"},
		},
	},
	{
		ID: "lithonia-reinitialize", File: "reinit-device.cap", PerKind: 3,
		Summary: "ReinitializeDevice with the undefined state 99 at a Lithonia device: it answers Reject undefined-enumeration",
		Differences: []Difference{
			{Frames: []int{201, 207, 223}, Classification: "expected refusal",
				Detail: "ReinitializeDevice with reinitializedStateOfDevice 99, not defined by clause 16.4.1.1.1; Wireshark decodes the value, go-bacnet refuses it as an undefined enumeration. The device answers Reject undefined-enumeration (frames 203, 209, 225), as go-bacnet's server does"},
		},
	},
	{
		ID: "atomic-read-file-segmented", File: "read-file.cap", PerKind: 2,
		Summary:     "AtomicReadFile at two devices (192.168.0.50 and .51): answers in up to five segments with window 16",
		Limitations: []string{"the file holds no I-Am or Device object read; epics-2.cap of the same lab address range identifies 192.168.0.50 as a Lithonia SYSC MLX 2x63w, which this case does not claim"},
	},
	{
		ID: "bvll-management-results", File: "BBMD_Results.pcap", PerKind: 4,
		Summary: "BVLL management requests (Write/Read-BDT, Read-FDT, Register-Foreign-Device) to a node answering BVLC-Result",
	},
	{
		ID: "contemporary-controls-basrt", File: "bvlc-bac4-rp.cap", PerKind: 2,
		Summary: "Contemporary Controls BASRT-B 1.24 router on UDP port 47812",
		Devices: map[string]string{"192.168.0.45:47812|local:": "BASRT-B router"},
	},
	{
		ID: "routing-table", File: "router.cap", PerKind: 2,
		Summary: "Who-Is-Router-To-Network, I-Am-Router-To-Network and Initialize-Routing-Table(-Ack)",
	},
	{
		ID: "bvll-foreign-device", File: "bvlc-fdreg-readprop-47809.cap", PerKind: 2,
		Summary: "Foreign device registration, Distribute-Broadcast-To-Network and Forwarded-NPDU on UDP 47809 between BACnet Stack demo applications (the BBMD and the foreign device), with I-Am from Alerton and WattStopper devices",
	},
	{
		ID: "cov-plugfest-2011", File: "cov-testing-1.pcap", PerKind: 2,
		Summary: "SubscribeCOV and COV notifications at a 2011 plugfest",
	},
}
