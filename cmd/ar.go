// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/dcp"
	"github.com/otfabric/go-profinet/model"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"

	clihelp "github.com/otfabric/profinetctl/internal/cli"
	"github.com/otfabric/profinetctl/internal/errs"
	"github.com/otfabric/profinetctl/internal/format"
)

// arOptions configure the AR profinetctl establishes as IO-Controller.
type arOptions struct {
	station         string
	gsdml, dap      string
	modelFile       string
	plugs           []string
	advanced        bool
	sendClock       uint16
	reductionRatio  uint16
	watchdog        uint16
	dataHold        uint16
	assignIP        string
	permanentIP     bool
	outputs         []string
	params          []string
	connectTimeout  time.Duration
	rpcPort         int
	controllerName  string
	activityTimeout time.Duration
}

// The default watchdog and data hold factors, in cycles: 10, go-profinet's
// default. PLCs use 3, which a general-purpose OS does not hold reliably
// (go-profinet's docs/CYCLIC.md). The tests, under the race detector, raise
// them further.
var defaultWatchdog, defaultDataHold uint16 = 10, 10

func addAROptions(c *cobra.Command, o *arOptions) {
	f := c.Flags()
	f.StringVarP(&o.station, "station", "s", "", "Station name of the device (required)")
	f.StringVar(&o.gsdml, "gsdml", "", "GSDML file of the device: the expected configuration")
	f.StringVar(&o.dap, "dap", "", "Device access point ID in the GSDML file (default: the first)")
	f.StringVar(&o.modelFile, "model", "", "Expected configuration as a go-profinet model in JSON (instead of --gsdml)")
	f.StringArrayVar(&o.plugs, "plug", nil, "Plug the module with this ident number from the GSDML catalog: SLOT=IDENT (repeatable)")
	f.BoolVar(&o.advanced, "advanced", false, "Advanced startup with RT_CLASS_2 (instead of legacy startup with RT_CLASS_1)")
	f.Uint16Var(&o.sendClock, "send-clock", 32, "SendClockFactor in units of 31.25 µs (32 = 1 ms)")
	f.Uint16Var(&o.reductionRatio, "reduction-ratio", 4, "Reduction ratio: the cycle is send clock × reduction ratio")
	f.Uint16Var(&o.watchdog, "watchdog-factor", defaultWatchdog, "Watchdog factor in cycles")
	f.Uint16Var(&o.dataHold, "data-hold-factor", defaultDataHold, "Data hold factor in cycles")
	f.StringVar(&o.assignIP, "assign-ip", "", "Assign this address/prefix to the device with DCP before connecting")
	f.BoolVar(&o.permanentIP, "permanent-ip", false, "With --assign-ip: store the address permanently")
	f.StringArrayVar(&o.outputs, "output", nil, "Set output data: SLOT:SUBSLOT:HEX (repeatable); outputs not given are zero with IOPS bad")
	f.StringArrayVar(&o.params, "param", nil, "Write a parameter record before PrmEnd: SLOT:SUBSLOT:INDEX:HEX (repeatable)")
	f.DurationVar(&o.connectTimeout, "connect-timeout", 20*time.Second, "How long to wait for the AR to reach Running")
	f.IntVar(&o.rpcPort, "rpc-port", rpc.Port, "UDP port of the controller's RPC server (devices send ApplicationReady to it)")
	f.StringVar(&o.controllerName, "controller-name", "profinetctl", "Station name of the controller in the AR")
	f.DurationVar(&o.activityTimeout, "activity-timeout", 10*time.Second, "CM initiator activity timeout")
}

// parsedAR is the validated configuration of an AR.
type parsedAR struct {
	model   *model.Device
	cfg     controller.DeviceConfig
	outputs []outputValue
}

type outputValue struct {
	slot, subslot uint16
	data          []byte
}

func splitAddr(s string, n int) ([]string, error) {
	f := strings.SplitN(s, ":", n)
	if len(f) != n {
		return nil, fmt.Errorf("%q: want %d fields separated by ':'", s, n)
	}
	return f, nil
}

func (o *arOptions) parse() (*parsedAR, error) {
	if o.station == "" {
		return nil, errs.New(errs.KindUsage, errs.CodeInvalidInput, "no device: give --station (the controller finds the device by its station name)", nil)
	}
	if err := dcp.ValidateStationName(o.station); err != nil {
		return nil, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--station %q: %v", o.station, err), err)
	}
	m, err := loadModel(o.gsdml, o.dap, o.modelFile)
	if err != nil {
		return nil, err
	}
	if err := plug(m, o.plugs); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, errs.InvalidInput(errs.CodeInvalidModel, err.Error(), err)
	}
	p := &parsedAR{model: m, cfg: controller.DeviceConfig{
		StationName: o.station, Model: m, Advanced: o.advanced, SendClockFactor: o.sendClock, ReductionRatio: o.reductionRatio,
		WatchdogFactor: o.watchdog, DataHoldFactor: o.dataHold, ActivityTimeout: o.activityTimeout, ReconnectMax: 2 * time.Second,
	}}
	bad := func(flag string, err error) error {
		return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--%s %v", flag, err), err)
	}
	if o.assignIP != "" {
		s, err := suiteOf(o.assignIP, "")
		if err != nil {
			return nil, bad("assign-ip", err)
		}
		p.cfg.IP, p.cfg.AssignIP, p.cfg.PermanentIP = s, true, o.permanentIP
	}
	for _, out := range o.outputs {
		f, err := splitAddr(out, 3)
		if err != nil {
			return nil, bad("output", err)
		}
		slot, e1 := parseUint16(f[0])
		sub, e2 := parseUint16(f[1])
		data, e3 := parseHex(f[2])
		if err := errors.Join(e1, e2, e3); err != nil {
			return nil, bad("output", fmt.Errorf("%q: %w", out, err))
		}
		sm, ok := m.Submodule(slot, sub)
		if !ok || sm.OutputLen == 0 {
			return nil, bad("output", fmt.Errorf("%q: slot %d subslot %d has no outputs", out, slot, sub))
		}
		if len(data) != int(sm.OutputLen) {
			return nil, bad("output", fmt.Errorf("%q: %d octets, the submodule has %d", out, len(data), sm.OutputLen))
		}
		p.outputs = append(p.outputs, outputValue{slot, sub, data})
	}
	for _, par := range o.params {
		f, err := splitAddr(par, 4)
		if err != nil {
			return nil, bad("param", err)
		}
		slot, e1 := parseUint16(f[0])
		sub, e2 := parseUint16(f[1])
		idx, e3 := parseUint16(f[2])
		data, e4 := parseHex(f[3])
		if err := errors.Join(e1, e2, e3, e4); err != nil {
			return nil, bad("param", fmt.Errorf("%q: %w", par, err))
		}
		p.cfg.Parameters = append(p.cfg.Parameters, controller.Parameter{Slot: slot, Subslot: sub, Index: pnio.Index(idx), Data: data})
	}
	return p, nil
}

// plug plugs modules of the catalog into slots.
func plug(m *model.Device, plugs []string) error {
	for _, pl := range plugs {
		slotS, identS, ok := strings.Cut(pl, "=")
		slot, err := parseUint16(slotS)
		if !ok || err != nil || slot == 0 {
			return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--plug %q: want SLOT=IDENT with a slot from 1", pl), err)
		}
		ident, err := strconv.ParseUint(identS, 0, 32)
		if err != nil {
			return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--plug %q: %v", pl, err), err)
		}
		var mod *model.Module
		for _, mt := range m.ModuleTypes() {
			if mt.IdentNumber == uint32(ident) {
				mt := mt
				mod = &mt
				break
			}
		}
		if mod == nil {
			return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--plug %q: no module type with ident number 0x%08X (profinetctl gsdml modules lists them)", pl, ident), nil)
		}
		replaced := false
		for i := range m.Slots {
			if m.Slots[i].Number == slot {
				m.Slots[i].Module, replaced = *mod, true
			}
		}
		if !replaced {
			m.Slots = append(m.Slots, model.Slot{Number: slot, Module: *mod})
		}
	}
	sort.Slice(m.Slots, func(i, j int) bool { return m.Slots[i].Number < m.Slots[j].Number })
	return nil
}

// arEvent is one record of the event stream of an AR.
type arEvent struct {
	Time  string `json:"time"`
	Event string `json:"event"`
	// state
	From  string `json:"from,omitempty"`
	To    string `json:"to,omitempty"`
	Error string `json:"error,omitempty"`
	// alarm
	Alarm *alarmView `json:"alarm,omitempty"`
}

type alarmView struct {
	Type     string `json:"type"`
	Priority string `json:"priority"`
	API      uint32 `json:"api"`
	Slot     uint16 `json:"slot"`
	Subslot  uint16 `json:"subslot"`
	Module   uint32 `json:"module_ident_number"`
	Sub      uint32 `json:"submodule_ident_number"`
	Sequence uint16 `json:"sequence"`
	USI      uint16 `json:"usi,omitempty"`
	Data     string `json:"data,omitempty"`
	// Diagnosis decodes the items of a diagnosis alarm.
	Diagnosis []diagView `json:"diagnosis,omitempty"`
}

func alarmViewOf(n pnio.AlarmNotification) *alarmView {
	v := &alarmView{Type: n.Type.String(), Priority: "low", API: n.API, Slot: n.Slot, Subslot: n.Subslot, Module: n.ModuleIdentNumber,
		Sub: n.SubmoduleIdentNumber, Sequence: n.Sequence(), USI: n.USI, Data: hex.EncodeToString(n.Data)}
	if n.High {
		v.Priority = "high"
	}
	if n.HasItem && n.USI >= pnio.USIChannelDiagnosis {
		if items, err := pnio.DecodeDiagnosisItems(n.USI, n.Data); err == nil {
			v.Diagnosis = diagViews([]pnio.DiagnosisData{{API: n.API, Slot: n.Slot, Subslot: n.Subslot, USI: n.USI, Items: items}})
		}
	}
	return v
}

func (e arEvent) MarshalTextOutput() (string, error) {
	switch e.Event {
	case "state":
		s := fmt.Sprintf("%s state %s → %s", e.Time, e.From, e.To)
		if e.Error != "" {
			s += ": " + e.Error
		}
		return s + "\n", nil
	case "alarm":
		a := e.Alarm
		s := fmt.Sprintf("%s alarm %s (%s) slot %d subslot 0x%04X seq %d", e.Time, a.Type, a.Priority, a.Slot, a.Subslot, a.Sequence)
		for _, d := range a.Diagnosis {
			s += fmt.Sprintf("; channel 0x%04X %s %s", d.Channel, d.Error, d.Severity)
		}
		if len(a.Diagnosis) == 0 && a.Data != "" {
			s += " data " + a.Data
		}
		return s + "\n", nil
	default:
		return fmt.Sprintf("%s %s %s\n", e.Time, e.Event, e.Error), nil
	}
}

func (e arEvent) TableHeaders() []string {
	return []string{"TIME", "EVENT", "DETAIL"}
}

func (e arEvent) TableRows() [][]string {
	t, _ := e.MarshalTextOutput()
	detail := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(t, e.Time), " "+e.Event))
	return [][]string{{e.Time, e.Event, detail}}
}

// arSession is an established AR of profinetctl's controller.
type arSession struct {
	*session
	ctrl   *controller.Controller
	dev    *controller.Device
	parsed *parsedAR
	events chan arEvent
}

func stamp() string { return time.Now().Format("15:04:05.000") }

// establish starts the controller, adds the device and waits for Running.
func (a *app) establish(ctx context.Context, o *arOptions, p *parsedAR) (*arSession, error) {
	s, err := a.open()
	if err != nil {
		return nil, err
	}
	// Find the device first, so that a missing one or a duplicate name
	// fails at once rather than after --connect-timeout.
	if _, err := s.resolve(ctx, &target{station: o.station, wait: time.Second}); err != nil {
		s.Close()
		return nil, err
	}
	// The controller has its own DCP requester: the session's would see
	// its responses too.
	_ = s.dcp.Close()
	ctrl, err := controller.New(controller.Config{
		Mux: s.link.Mux, ListenPacket: s.link.ListenPacket, RPCPort: o.rpcPort, StationName: o.controllerName,
		VendorID: toolVendorID, DeviceID: toolDeviceID, Instance: 1,
		DCP: dcp.ClientOptions{Timeout: a.timeout / 4, Retries: 3}, Logger: a.logger(),
	})
	if err != nil {
		s.link.Close()
		return nil, classify("controller", err)
	}
	as := &arSession{session: s, ctrl: ctrl, parsed: p, events: make(chan arEvent, 256)}
	push := func(e arEvent) {
		select {
		case as.events <- e:
		default:
		}
	}
	cfg := p.cfg
	cfg.ReconnectMin = 200 * time.Millisecond
	cfg.RPCTimeout = a.timeout
	cfg.OnState = func(from, to controller.State, err error) {
		e := arEvent{Time: stamp(), Event: "state", From: from.String(), To: to.String()}
		if err != nil {
			e.Error = classify("", err).Error()
		}
		push(e)
	}
	cfg.OnAlarm = func(ev controller.AlarmEvent) {
		push(arEvent{Time: stamp(), Event: "alarm", Alarm: alarmViewOf(ev.Notification)})
	}
	dev, err := ctrl.AddDevice(cfg)
	if err != nil {
		as.close()
		return nil, classify("add device", err)
	}
	as.dev = dev
	for _, out := range p.outputs {
		if err := dev.SetOutput(out.slot, out.subslot, out.data, pnio.IOxSGood); err != nil {
			as.close()
			return nil, classify("output", err)
		}
	}
	wctx, cancel := context.WithTimeout(ctx, o.connectTimeout)
	defer cancel()
	if err := dev.WaitState(wctx, controller.StateRunning); err != nil {
		st, last := dev.State()
		as.close()
		if errors.Is(err, context.Canceled) {
			return nil, classify("connect", err)
		}
		msg := fmt.Sprintf("the AR with %q did not reach Running within %s (state %s)", o.station, o.connectTimeout, st)
		if last != nil {
			cl := classify("", last)
			e, _ := errs.As(cl)
			return nil, errs.New(e.Kind, e.Code, msg+": "+last.Error(), last)
		}
		return nil, errs.New(errs.KindTimeout, errs.CodeConnectFailed, msg, err)
	}
	if diffs := differences(dev.ModuleDiff()); len(diffs) > 0 {
		msg := "the real configuration differs from the expected one: " + strings.Join(diffs, "; ")
		_, _ = fmt.Fprintf(a.stderr, "warning: %s\n", msg)
		push(arEvent{Time: stamp(), Event: "module-diff", Error: msg})
	}
	return as, nil
}

// differences describes the modules and submodules a ModuleDiffBlock reports
// as wrong, substituted or missing. A diff that only reports the pending
// ApplicationReady yields none.
func differences(diff *pnio.ModuleDiffBlock) []string {
	if diff == nil {
		return nil
	}
	var out []string
	for _, api := range diff.APIs {
		for _, m := range api.Modules {
			switch m.State {
			case pnio.ModuleStateNoModule:
				out = append(out, fmt.Sprintf("slot %d: no module", m.Slot))
				continue
			case pnio.ModuleStateWrong:
				out = append(out, fmt.Sprintf("slot %d: wrong module 0x%08X", m.Slot, m.IdentNumber))
				continue
			case pnio.ModuleStateSubstitute:
				out = append(out, fmt.Sprintf("slot %d: substitute module 0x%08X", m.Slot, m.IdentNumber))
			}
			for _, sm := range m.Submodules {
				if sm.State&pnio.SubmStateFormat == 0 {
					continue
				}
				switch sm.State & (3 << 11) {
				case pnio.SubmStateIdentWrong:
					out = append(out, fmt.Sprintf("slot %d subslot 0x%04X: wrong submodule 0x%08X", m.Slot, sm.Subslot, sm.IdentNumber))
				case pnio.SubmStateIdentNoSubmodule:
					out = append(out, fmt.Sprintf("slot %d subslot 0x%04X: no submodule", m.Slot, sm.Subslot))
				case pnio.SubmStateIdentSubstitute:
					out = append(out, fmt.Sprintf("slot %d subslot 0x%04X: substitute submodule 0x%08X", m.Slot, sm.Subslot, sm.IdentNumber))
				}
			}
		}
	}
	return out
}

// close releases the AR and stops the controller.
func (s *arSession) close() {
	if s.ctrl != nil {
		_ = s.ctrl.Close()
	}
	s.link.Close()
}

// arInfoResult is what Connect negotiated.
type arInfoResult struct {
	Station    string            `json:"station"`
	MAC        string            `json:"mac"`
	IP         string            `json:"ip"`
	AR         arView            `json:"ar"`
	ModuleDiff []diffModuleView  `json:"module_diff,omitempty"`
	Diffs      []string          `json:"differences,omitempty"`
	info       controller.ARInfo `json:"-"`
}

type arView struct {
	UUID           string   `json:"ar_uuid"`
	SessionKey     uint16   `json:"session_key"`
	Type           string   `json:"ar_type"`
	RTClass        uint8    `json:"rt_class"`
	Advanced       bool     `json:"advanced"`
	DeviceAddr     string   `json:"device_address"`
	LocalAlarmRef  uint16   `json:"local_alarm_ref"`
	RemoteAlarmRef uint16   `json:"remote_alarm_ref"`
	Input          iocrView `json:"input"`
	Output         iocrView `json:"output"`
}

type iocrView struct {
	FrameID         uint16  `json:"frame_id"`
	DataLength      uint16  `json:"data_length"`
	SendClockFactor uint16  `json:"send_clock_factor"`
	ReductionRatio  uint16  `json:"reduction_ratio"`
	WatchdogFactor  uint16  `json:"watchdog_factor"`
	DataHoldFactor  uint16  `json:"data_hold_factor"`
	CycleMS         float64 `json:"cycle_ms"`
}

type diffModuleView struct {
	Slot        uint16              `json:"slot"`
	IdentNumber uint32              `json:"ident_number"`
	State       uint16              `json:"state"`
	Submodules  []diffSubmoduleView `json:"submodules"`
}

type diffSubmoduleView struct {
	Subslot     uint16 `json:"subslot"`
	IdentNumber uint32 `json:"ident_number"`
	State       uint16 `json:"state"`
}

func iocrViewOf(c controller.IOCRInfo) iocrView {
	return iocrView{FrameID: c.FrameID, DataLength: c.DataLength, SendClockFactor: c.SendClockFactor, ReductionRatio: c.ReductionRatio,
		WatchdogFactor: c.WatchdogFactor, DataHoldFactor: c.DataHoldFactor, CycleMS: float64(c.Cycle) / float64(time.Millisecond)}
}

func diffView(d *pnio.ModuleDiffBlock) []diffModuleView {
	if d == nil {
		return nil
	}
	out := []diffModuleView{}
	for _, api := range d.APIs {
		for _, m := range api.Modules {
			v := diffModuleView{Slot: m.Slot, IdentNumber: m.IdentNumber, State: m.State, Submodules: []diffSubmoduleView{}}
			for _, sm := range m.Submodules {
				v.Submodules = append(v.Submodules, diffSubmoduleView{Subslot: sm.Subslot, IdentNumber: sm.IdentNumber, State: sm.State})
			}
			out = append(out, v)
		}
	}
	return out
}

func (r arInfoResult) MarshalTextOutput() (string, error) {
	a := r.info
	var b strings.Builder
	fmt.Fprintf(&b, "Device:      %s (%s, %s)\n", r.Station, r.MAC, r.IP)
	fmt.Fprintf(&b, "AR:          %s, type %s, session key %d\n", a.UUID, a.Type, a.SessionKey)
	mode := "legacy"
	if a.Advanced {
		mode = "advanced"
	}
	fmt.Fprintf(&b, "Startup:     %s, RT class %d\n", mode, a.RTClass)
	fmt.Fprintf(&b, "Alarm refs:  local %d, remote %d\n", a.LocalAlarmRef, a.RemoteAlarmRef)
	for _, cr := range []controller.IOCRInfo{a.Input, a.Output} {
		fmt.Fprintf(&b, "%-12s FrameID 0x%04X, %d octets, cycle %s (send clock %d × reduction %d), watchdog %d, data hold %d\n",
			cr.Type.String()+" CR:", cr.FrameID, cr.DataLength, cr.Cycle, cr.SendClockFactor, cr.ReductionRatio, cr.WatchdogFactor, cr.DataHoldFactor)
	}
	if len(r.Diffs) > 0 {
		b.WriteString("ModuleDiff:  the real configuration differs from the expected one:\n")
		for _, d := range r.Diffs {
			fmt.Fprintf(&b, "  %s\n", d)
		}
	}
	return b.String(), nil
}

func (r arInfoResult) TableHeaders() []string {
	return []string{"IOCR", "FRAMEID", "LENGTH", "CYCLE", "WATCHDOG", "DATA HOLD"}
}

func (r arInfoResult) TableRows() [][]string {
	var rows [][]string
	for _, cr := range []controller.IOCRInfo{r.info.Input, r.info.Output} {
		rows = append(rows, []string{cr.Type.String(), fmt.Sprintf("0x%04X", cr.FrameID), fmt.Sprint(cr.DataLength), cr.Cycle.String(), fmt.Sprint(cr.WatchdogFactor), fmt.Sprint(cr.DataHoldFactor)})
	}
	return rows
}

func (s *arSession) info() arInfoResult {
	info, _ := s.dev.AR()
	diff := s.dev.ModuleDiff()
	return arInfoResult{Station: s.dev.Name(), MAC: s.dev.MAC().String(), IP: addrOr(s.dev.IP().Addr), info: info,
		AR: arView{UUID: info.UUID.String(), SessionKey: info.SessionKey, Type: info.Type.String(), RTClass: uint8(info.RTClass), Advanced: info.Advanced,
			DeviceAddr: info.DeviceAddr, LocalAlarmRef: info.LocalAlarmRef, RemoteAlarmRef: info.RemoteAlarmRef,
			Input: iocrViewOf(info.Input), Output: iocrViewOf(info.Output)},
		ModuleDiff: diffView(diff), Diffs: differences(diff)}
}

// iocrResult is the layout of the two IOCRs.
type iocrResult struct {
	AR      string      `json:"ar_uuid"`
	Entries []iocrEntry `json:"entries"`
}

type iocrEntry struct {
	IOCR    string `json:"iocr"`
	FrameID uint16 `json:"frame_id"`
	Kind    string `json:"kind"`
	Slot    uint16 `json:"slot"`
	Subslot uint16 `json:"subslot"`
	Offset  uint16 `json:"offset"`
	Length  uint16 `json:"length"`
}

func (r iocrResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	cur := ""
	for _, e := range r.Entries {
		if e.IOCR != cur {
			cur = e.IOCR
			fmt.Fprintf(&b, "%s CR, FrameID 0x%04X:\n", e.IOCR, e.FrameID)
		}
		if e.Kind == "data" {
			fmt.Fprintf(&b, "  offset %4d  data %3d octets + IOPS  slot %d subslot 0x%04X\n", e.Offset, e.Length, e.Slot, e.Subslot)
		} else {
			fmt.Fprintf(&b, "  offset %4d  IOCS                    slot %d subslot 0x%04X\n", e.Offset, e.Slot, e.Subslot)
		}
	}
	return b.String(), nil
}

func (r iocrResult) TableHeaders() []string {
	return []string{"IOCR", "FRAMEID", "KIND", "SLOT", "SUBSLOT", "OFFSET", "LENGTH"}
}

func (r iocrResult) TableRows() [][]string {
	var rows [][]string
	for _, e := range r.Entries {
		rows = append(rows, []string{e.IOCR, fmt.Sprintf("0x%04X", e.FrameID), e.Kind, fmt.Sprint(e.Slot), fmt.Sprintf("0x%04X", e.Subslot), fmt.Sprint(e.Offset), fmt.Sprint(e.Length)})
	}
	return rows
}

func iocrOf(info controller.ARInfo) iocrResult {
	r := iocrResult{AR: info.UUID.String(), Entries: []iocrEntry{}}
	for _, cr := range []controller.IOCRInfo{info.Input, info.Output} {
		for _, o := range cr.Objects {
			r.Entries = append(r.Entries, iocrEntry{IOCR: cr.Type.String(), FrameID: cr.FrameID, Kind: "data", Slot: o.Slot, Subslot: o.Subslot, Offset: o.Offset, Length: o.Length})
		}
		for _, c := range cr.IOCS {
			r.Entries = append(r.Entries, iocrEntry{IOCR: cr.Type.String(), FrameID: cr.FrameID, Kind: "iocs", Slot: c.Slot, Subslot: c.Subslot, Offset: c.Offset, Length: 1})
		}
	}
	return r
}

// oneShot establishes an AR, collects a result from it and releases it.
func (a *app) oneShot(c *cobra.Command, o *arOptions, collect func(ctx context.Context, s *arSession) (any, error)) error {
	p, err := o.parse()
	if err != nil {
		return err
	}
	return a.result(c, func(ctx context.Context) (any, error) {
		s, err := a.establish(ctx, o, p)
		if err != nil {
			return nil, err
		}
		defer s.close()
		return collect(ctx, s)
	})
}

func newARCmd(a *app) *cobra.Command {
	var o arOptions
	c := &cobra.Command{
		Use:   "ar",
		Short: "Establish an AR, show what Connect negotiated (AR, IOCRs, ModuleDiff), release it",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.oneShot(c, &o, func(_ context.Context, s *arSession) (any, error) { return s.info(), nil })
		},
	}
	addAROptions(c, &o)
	return c
}

func newIOCRCmd(a *app) *cobra.Command {
	var o arOptions
	c := &cobra.Command{
		Use:   "iocr",
		Short: "Establish an AR and show the layout of its IOCRs (data objects, IOPS, IOCS), then release it",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.oneShot(c, &o, func(_ context.Context, s *arSession) (any, error) {
				info, _ := s.dev.AR()
				return iocrOf(info), nil
			})
		},
	}
	addAROptions(c, &o)
	return c
}

// runFor waits until d elapses (0: until ctx is done) and reports whether
// ctx ended the wait.
func runFor(ctx context.Context, d time.Duration) bool {
	var t <-chan time.Time
	if d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		t = timer.C
	}
	select {
	case <-ctx.Done():
		return true
	case <-t:
		return false
	}
}

// streamAR establishes an AR and streams its events (and what tick adds,
// every interval) until duration elapses or the user interrupts; an
// interrupt ends the stream normally.
func (a *app) streamAR(c *cobra.Command, o *arOptions, duration, interval time.Duration, tick func(s *arSession, out *format.StreamWriter) error, final func(s *arSession) any, alarmsOnly bool) error {
	p, err := o.parse()
	if err != nil {
		return err
	}
	return a.stream(c, func(ctx context.Context, out *format.StreamWriter) error {
		s, err := a.establish(ctx, o, p)
		if err != nil {
			return err
		}
		defer s.close()
		var mu sync.Mutex
		write := func(v any) error {
			mu.Lock()
			defer mu.Unlock()
			if err := out.Write(v); err != nil {
				return errs.Output(errs.CodeOutputWriteFailed, err)
			}
			return nil
		}
		// The events up to Running are part of the stream.
		var ticker <-chan time.Time
		if tick != nil && interval > 0 {
			t := time.NewTicker(interval)
			defer t.Stop()
			ticker = t.C
		}
		var deadline <-chan time.Time
		if duration > 0 {
			t := time.NewTimer(duration)
			defer t.Stop()
			deadline = t.C
		}
		for {
			select {
			case e := <-s.events:
				if alarmsOnly && e.Event != "alarm" && (e.Event != "state" || e.To != "Offline") {
					continue
				}
				if err := write(e); err != nil {
					return err
				}
			case <-ticker:
				if err := tick(s, out); err != nil {
					return err
				}
			case <-deadline:
				if final != nil {
					return write(final(s))
				}
				return nil
			case <-ctx.Done():
				if final != nil {
					return write(final(s))
				}
				return nil
			}
		}
	})
}

// summary is the final record of a connect.
type summary struct {
	Time   string       `json:"time"`
	Event  string       `json:"event"`
	State  string       `json:"state"`
	Error  string       `json:"error,omitempty"`
	Stats  statsView    `json:"stats"`
	Inputs []inputValue `json:"inputs"`
}

func (s summary) MarshalTextOutput() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s summary: state %s, %d connect(s), %d abort(s), %d alarm(s), %d frames sent, %d received, %d watchdog expiries\n",
		s.Time, s.State, s.Stats.Connects, s.Stats.Aborts, s.Stats.Alarms, s.Stats.Sent, s.Stats.Received, s.Stats.Expired)
	for _, in := range s.Inputs {
		fmt.Fprintf(&b, "  input slot %d subslot 0x%04X: %s IOPS %s\n", in.Slot, in.Subslot, orDash(in.Data), in.IOPS)
	}
	return b.String(), nil
}

func (s summary) TableHeaders() []string {
	return []string{"TIME", "EVENT", "DETAIL"}
}

func (s summary) TableRows() [][]string {
	return [][]string{{s.Time, s.Event, fmt.Sprintf("state %s, %d sent, %d received, %d expired", s.State, s.Stats.Sent, s.Stats.Received, s.Stats.Expired)}}
}

type statsView struct {
	Connects     uint64 `json:"connects"`
	Aborts       uint64 `json:"aborts"`
	Alarms       uint64 `json:"alarms"`
	Sent         uint64 `json:"frames_sent"`
	SendErrors   uint64 `json:"send_errors"`
	Received     uint64 `json:"frames_received"`
	Stale        uint64 `json:"frames_stale"`
	Malformed    uint64 `json:"frames_malformed"`
	WrongSource  uint64 `json:"frames_wrong_source"`
	Expired      uint64 `json:"watchdog_expiries"`
	Cycles       uint64 `json:"cycles"`
	Missed       uint64 `json:"missed_cycles"`
	MaxLateness  string `json:"max_lateness"`
	MeanLateness string `json:"mean_lateness"`
	MinInterval  string `json:"min_interval"`
	MaxInterval  string `json:"max_interval"`
}

func statsOf(st controller.DeviceStats) statsView {
	return statsView{Connects: st.Connects, Aborts: st.Aborts, Alarms: st.Alarms, Sent: st.Provider.Sent, SendErrors: st.Provider.SendErrors,
		Received: st.Consumer.Received, Stale: st.Consumer.Stale, Malformed: st.Consumer.Malformed, WrongSource: st.Consumer.WrongSource,
		Expired: st.Consumer.Expired, Cycles: st.Cycle.Cycles, Missed: st.Cycle.Missed,
		MaxLateness: st.Cycle.MaxLateness.String(), MeanLateness: st.Cycle.MeanLateness.String(),
		MinInterval: st.Cycle.MinInterval.String(), MaxInterval: st.Cycle.MaxInterval.String()}
}

// inputValue is the input data of a submodule.
type inputValue struct {
	Slot    uint16 `json:"slot"`
	Subslot uint16 `json:"subslot"`
	Data    string `json:"data"`
	IOPS    string `json:"iops"`
}

// outputStatus is the consumer status the device reports for the outputs of a submodule.
type outputStatus struct {
	Slot    uint16 `json:"slot"`
	Subslot uint16 `json:"subslot"`
	IOCS    string `json:"iocs"`
}

func ioxs(v uint8, ok bool) string {
	switch {
	case !ok:
		return "-"
	case v&pnio.IOxSGood != 0:
		return "good"
	default:
		return fmt.Sprintf("bad (0x%02X)", v)
	}
}

func (s *arSession) inputs() []inputValue {
	out := []inputValue{}
	s.parsed.model.Each(func(p model.PluggedSubmodule) {
		sm := p.Submodule
		if sm.InputLen == 0 && sm.OutputLen > 0 {
			return
		}
		data, iops, ok := s.dev.Input(p.Slot, sm.Subslot)
		out = append(out, inputValue{Slot: p.Slot, Subslot: sm.Subslot, Data: hex.EncodeToString(data), IOPS: ioxs(iops, ok)})
	})
	return out
}

func (s *arSession) summary() any {
	st, last := s.dev.State()
	r := summary{Time: stamp(), Event: "summary", State: st.String(), Stats: statsOf(s.dev.Stats()), Inputs: s.inputs()}
	if last != nil {
		r.Error = classify("", last).Error()
	}
	return r
}

func newConnectCmd(a *app) *cobra.Command {
	var o arOptions
	var duration time.Duration
	c := &cobra.Command{
		Use:   "connect",
		Short: "Establish an AR as IO-Controller and run it, streaming state changes and alarms",
		Long: `Connect to the device as IO-Controller with the expected configuration of its
GSDML file (--gsdml, --dap, --plug) or of a model (--model): DCP Identify by
station name, Connect, parameterization (--param), PrmEnd, ApplicationReady and
cyclic data exchange. Outputs are set with --output; outputs not given are sent
as zeros with a bad IOPS, so that the device keeps its outputs safe.

The AR runs until --duration elapses (0: until interrupted with Ctrl-C), then is
released. State changes and alarms are streamed (JSON Lines with --format json),
followed by a summary. The controller reconnects after a failure.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.streamAR(c, &o, duration, 0, nil, func(s *arSession) any { return s.summary() }, false)
		},
	}
	addAROptions(c, &o)
	c.Flags().DurationVar(&duration, "duration", 0, "How long to run the AR (0: until interrupted)")
	return c
}

func newAlarmsCmd(a *app) *cobra.Command {
	var o arOptions
	var duration time.Duration
	c := &cobra.Command{
		Use:   "alarms",
		Short: "Establish an AR and stream the alarms of the device (acknowledged as they arrive)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.streamAR(c, &o, duration, 0, nil, nil, true)
		},
	}
	addAROptions(c, &o)
	c.Flags().DurationVar(&duration, "duration", 0, "How long to listen (0: until interrupted)")
	return c
}

// disconnectResult reports a released AR.
type disconnectResult struct {
	Station string `json:"station"`
	AR      string `json:"ar_uuid"`
}

func (r disconnectResult) MarshalTextOutput() (string, error) {
	return fmt.Sprintf("released AR %s of %s\n", r.AR, r.Station), nil
}
func (r disconnectResult) TableHeaders() []string { return []string{"STATION", "AR"} }
func (r disconnectResult) TableRows() [][]string  { return [][]string{{r.Station, r.AR}} }

func newDisconnectCmd(a *app) *cobra.Command {
	var t target
	var arUUID string
	var key uint16
	c := &cobra.Command{
		Use:   "disconnect --ar UUID --session-key N",
		Short: "Release an AR left behind by another controller (DCE/RPC Release)",
		Long: `Send a Release for the AR with the given AR UUID and session key, for example an
AR that a crashed controller left behind. profinetctl connect releases its own
ARs when it ends. Devices that tie an AR to the RPC activity of its Connect
(p-net) do not accept a Release from another activity: they close the AR after
its CM activity timeout instead.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			u, err := rpc.ParseUUID(arUUID)
			if err != nil || u.IsNil() {
				return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--ar %q is not an AR UUID", arUUID), err)
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.openRPC(ctx, &t)
				if err != nil {
					return nil, err
				}
				defer s.Close()
				cctx, cancel := s.ctx(ctx)
				defer cancel()
				if err := s.rc.Control(cctx, pnio.ControlBlock{Type: pnio.BlockReleaseReq, ARUUID: u, SessionKey: key, Command: pnio.CmdRelease}); err != nil {
					return nil, classify("Release", err)
				}
				return disconnectResult{Station: s.id.NameOfStation, AR: u.String()}, nil
			})
		},
	}
	addRPCTargetFlags(c, &t)
	c.Flags().StringVar(&arUUID, "ar", "", "AR UUID (required)")
	c.Flags().Uint16Var(&key, "session-key", 0, "Session key of the AR")
	clihelp.MustMarkFlagRequired(c, "ar")
	return c
}

// ioStatusResult is a snapshot of the cyclic data.
type ioStatusResult struct {
	Station string         `json:"station"`
	State   string         `json:"state"`
	Inputs  []inputValue   `json:"inputs"`
	Outputs []outputStatus `json:"outputs"`
}

func (r ioStatusResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s\n", r.Station, r.State)
	for _, in := range r.Inputs {
		fmt.Fprintf(&b, "input  slot %d subslot 0x%04X: %s IOPS %s\n", in.Slot, in.Subslot, orDash(in.Data), in.IOPS)
	}
	for _, o := range r.Outputs {
		fmt.Fprintf(&b, "output slot %d subslot 0x%04X: IOCS %s\n", o.Slot, o.Subslot, o.IOCS)
	}
	return b.String(), nil
}

func (r ioStatusResult) TableHeaders() []string {
	return []string{"DIRECTION", "SLOT", "SUBSLOT", "DATA", "STATUS"}
}

func (r ioStatusResult) TableRows() [][]string {
	var rows [][]string
	for _, in := range r.Inputs {
		rows = append(rows, []string{"input", fmt.Sprint(in.Slot), fmt.Sprintf("0x%04X", in.Subslot), orDash(in.Data), "IOPS " + in.IOPS})
	}
	for _, o := range r.Outputs {
		rows = append(rows, []string{"output", fmt.Sprint(o.Slot), fmt.Sprintf("0x%04X", o.Subslot), "-", "IOCS " + o.IOCS})
	}
	return rows
}

func (s *arSession) ioStatus() ioStatusResult {
	st, _ := s.dev.State()
	r := ioStatusResult{Station: s.dev.Name(), State: st.String(), Inputs: s.inputs(), Outputs: []outputStatus{}}
	s.parsed.model.Each(func(p model.PluggedSubmodule) {
		if p.Submodule.OutputLen == 0 {
			return
		}
		cs, ok := s.dev.ConsumerStatus(p.Slot, p.Submodule.Subslot)
		r.Outputs = append(r.Outputs, outputStatus{Slot: p.Slot, Subslot: p.Submodule.Subslot, IOCS: ioxs(cs, ok)})
	})
	return r
}

// sample is one record of io monitor.
type sample struct {
	Time   string       `json:"time"`
	Event  string       `json:"event"`
	Inputs []inputValue `json:"inputs"`
}

func (s sample) MarshalTextOutput() (string, error) {
	var parts []string
	for _, in := range s.Inputs {
		parts = append(parts, fmt.Sprintf("%d/0x%04X=%s", in.Slot, in.Subslot, orDash(in.Data)))
	}
	return s.Time + " " + strings.Join(parts, " ") + "\n", nil
}

func (s sample) TableHeaders() []string {
	h := []string{"TIME"}
	for _, in := range s.Inputs {
		h = append(h, fmt.Sprintf("%d/0x%04X", in.Slot, in.Subslot))
	}
	return h
}

func (s sample) TableRows() [][]string {
	row := []string{s.Time}
	for _, in := range s.Inputs {
		row = append(row, orDash(in.Data))
	}
	return [][]string{row}
}

// statisticsResult is the counters of a run.
type statisticsResult struct {
	Station  string    `json:"station"`
	State    string    `json:"state"`
	Duration string    `json:"duration"`
	Cycle    string    `json:"cycle"`
	Stats    statsView `json:"stats"`
	// FrameRate is the received frames per second.
	FrameRate float64 `json:"frames_per_second"`
}

func (r statisticsResult) MarshalTextOutput() (string, error) {
	s := r.Stats
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s for %s, cycle %s\n", r.Station, r.State, r.Duration, r.Cycle)
	fmt.Fprintf(&b, "provider:  %d frames sent, %d send errors\n", s.Sent, s.SendErrors)
	fmt.Fprintf(&b, "consumer:  %d frames received (%.1f/s), %d stale, %d malformed, %d from another source, %d watchdog expiries\n",
		s.Received, r.FrameRate, s.Stale, s.Malformed, s.WrongSource, s.Expired)
	fmt.Fprintf(&b, "scheduler: %d cycles, %d missed, lateness max %s mean %s, interval %s..%s\n", s.Cycles, s.Missed, s.MaxLateness, s.MeanLateness, s.MinInterval, s.MaxInterval)
	fmt.Fprintf(&b, "AR:        %d connect(s), %d abort(s), %d alarm(s)\n", s.Connects, s.Aborts, s.Alarms)
	return b.String(), nil
}

func (r statisticsResult) TableHeaders() []string {
	return []string{"COUNTER", "VALUE"}
}

func (r statisticsResult) TableRows() [][]string {
	s := r.Stats
	return [][]string{
		{"frames sent", fmt.Sprint(s.Sent)}, {"send errors", fmt.Sprint(s.SendErrors)}, {"frames received", fmt.Sprint(s.Received)},
		{"frames stale", fmt.Sprint(s.Stale)}, {"frames malformed", fmt.Sprint(s.Malformed)}, {"watchdog expiries", fmt.Sprint(s.Expired)},
		{"cycles", fmt.Sprint(s.Cycles)}, {"missed cycles", fmt.Sprint(s.Missed)}, {"max lateness", s.MaxLateness}, {"connects", fmt.Sprint(s.Connects)},
		{"aborts", fmt.Sprint(s.Aborts)}, {"alarms", fmt.Sprint(s.Alarms)},
	}
}

func newIOCmd(a *app) *cobra.Command {
	g := groupCmd("io", "Cyclic IO data of an AR")
	var so arOptions
	var settle time.Duration
	status := &cobra.Command{
		Use:   "status",
		Short: "Establish an AR, show the inputs and the status of the outputs, release it",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.oneShot(c, &so, func(ctx context.Context, s *arSession) (any, error) {
				if runFor(ctx, settle) {
					return nil, classify("io status", ctx.Err())
				}
				return s.ioStatus(), nil
			})
		},
	}
	addAROptions(status, &so)
	status.Flags().DurationVar(&settle, "settle", 500*time.Millisecond, "How long to exchange data before the snapshot")

	var mo arOptions
	var duration, interval time.Duration
	var changes bool
	monitor := &cobra.Command{
		Use:   "monitor",
		Short: "Establish an AR and stream the input data at an interval",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var last string
			tick := func(s *arSession, out *format.StreamWriter) error {
				smp := sample{Time: stamp(), Event: "sample", Inputs: s.inputs()}
				key := fmt.Sprint(smp.Inputs)
				if changes && key == last {
					return nil
				}
				last = key
				if err := out.Write(smp); err != nil {
					return errs.Output(errs.CodeOutputWriteFailed, err)
				}
				return nil
			}
			return a.streamAR(c, &mo, duration, interval, tick, nil, false)
		},
	}
	addAROptions(monitor, &mo)
	monitor.Flags().DurationVar(&duration, "duration", 0, "How long to monitor (0: until interrupted)")
	monitor.Flags().DurationVar(&interval, "interval", time.Second, "Interval between samples")
	monitor.Flags().BoolVar(&changes, "changes", false, "Only write a sample when the inputs changed")

	var to arOptions
	var statDuration time.Duration
	statistics := &cobra.Command{
		Use:   "statistics",
		Short: "Establish an AR, run it for --duration and report the cyclic counters",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return a.oneShot(c, &to, func(ctx context.Context, s *arSession) (any, error) {
				start := time.Now()
				runFor(ctx, statDuration)
				el := time.Since(start)
				st, _ := s.dev.State()
				info, _ := s.dev.AR()
				v := statsOf(s.dev.Stats())
				return statisticsResult{Station: s.dev.Name(), State: st.String(), Duration: el.Round(time.Millisecond).String(), Cycle: info.Input.Cycle.String(),
					Stats: v, FrameRate: float64(v.Received) / el.Seconds()}, nil
			})
		},
	}
	addAROptions(statistics, &to)
	statistics.Flags().DurationVar(&statDuration, "duration", 5*time.Second, "How long to run")
	g.AddCommand(status, monitor, statistics)
	return g
}
