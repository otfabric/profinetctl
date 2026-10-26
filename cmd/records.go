// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/otfabric/go-profinet/controller"
	"github.com/otfabric/go-profinet/pnio"
	"github.com/otfabric/go-profinet/rpc"

	clihelp "github.com/otfabric/profinetctl/internal/cli"
	"github.com/otfabric/profinetctl/internal/errs"
)

// recordResult is the data of a record read.
type recordResult struct {
	MAC     string `json:"mac"`
	Station string `json:"station"`
	API     uint32 `json:"api"`
	Slot    uint16 `json:"slot"`
	Subslot uint16 `json:"subslot"`
	Index   uint16 `json:"index"`
	Length  int    `json:"length"`
	Data    string `json:"data"`
	// Blocks lists the types of the blocks the data holds, when it is a
	// sequence of blocks.
	Blocks []string `json:"blocks,omitempty"`
}

func (r recordResult) MarshalTextOutput() (string, error) {
	s := fmt.Sprintf("%s slot %d subslot 0x%04X index 0x%04X: %d octets\n%s\n", orDash(r.Station), r.Slot, r.Subslot, r.Index, r.Length, hexDump(r.Data))
	if len(r.Blocks) > 0 {
		s += "blocks: " + strings.Join(r.Blocks, ", ") + "\n"
	}
	return s, nil
}

func (r recordResult) TableHeaders() []string {
	return []string{"SLOT", "SUBSLOT", "INDEX", "LENGTH", "DATA"}
}

func (r recordResult) TableRows() [][]string {
	return [][]string{{fmt.Sprint(r.Slot), fmt.Sprintf("0x%04X", r.Subslot), fmt.Sprintf("0x%04X", r.Index), fmt.Sprint(r.Length), orDash(r.Data)}}
}

// hexDump formats hex data in lines of 16 octets with offsets.
func hexDump(h string) string {
	b, _ := hex.DecodeString(h)
	if len(b) == 0 {
		return "(empty)"
	}
	var out strings.Builder
	for off := 0; off < len(b); off += 16 {
		end := min(off+16, len(b))
		fmt.Fprintf(&out, "%04x  % x", off, b[off:end])
		if end < len(b) {
			out.WriteString("\n")
		}
	}
	return out.String()
}

// blockTypes returns the block types of data that is a sequence of blocks.
func blockTypes(b []byte) []string {
	blocks, err := pnio.SplitBlocks(b)
	if err != nil {
		return nil
	}
	var out []string
	for _, blk := range blocks {
		out = append(out, blk.Header.Type.String())
	}
	return out
}

func newRecordCmd(a *app) *cobra.Command {
	g := groupCmd("record", "Read and write records of a device (DCE/RPC)")
	var rt target
	var rr recordAddr
	var maxLen uint32
	read := &cobra.Command{
		Use:   "read --index INDEX",
		Short: "Read a record (ReadImplicit: no AR needed)",
		Long: `Read the record INDEX of a submodule with ReadImplicit, which needs no
application relationship. --index takes the record index in decimal or hex
(0xAFF0 is I&M0, 0xF80C the diagnosis of the device, 0x802A the port data).`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := rt.validate(); err != nil {
				return err
			}
			rec, err := rr.record()
			if err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.openRPC(ctx, &rt)
				if err != nil {
					return nil, err
				}
				defer s.Close()
				cctx, cancel := s.ctx(ctx)
				defer cancel()
				b, err := s.rc.Read(cctx, rpc.NilUUID, rec, maxLen)
				if err != nil {
					return nil, classify("read "+rec.String(), err)
				}
				return recordResult{MAC: s.id.MAC.String(), Station: s.id.NameOfStation, API: rec.API, Slot: rec.Slot, Subslot: rec.Subslot,
					Index: uint16(rec.Index), Length: len(b), Data: hex.EncodeToString(b), Blocks: blockTypes(b)}, nil
			})
		},
	}
	addRPCTargetFlags(read, &rt)
	addRecordFlags(read, &rr, 0, 1, "Record index (decimal or 0x hex); required")
	read.Flags().Uint32Var(&maxLen, "max-length", 4096, "Largest record accepted")
	clihelp.MustMarkFlagRequired(read, "index")

	var wt target
	var wr recordAddr
	var wv writeVia
	var data, dataFile string
	write := &cobra.Command{
		Use:   "write --index INDEX --data HEX",
		Short: "Write a record (in a device access AR, or an IOCAR with --gsdml/--model)",
		Long: `Write the record INDEX of a submodule. profinetctl establishes a device access AR
(a supervisor AR without IO data) for the write and releases it afterwards.
Devices that do not support device access (p-net) reject it: with --gsdml or
--model the write goes through an IO-Controller AR (IOCAR) with that expected
configuration instead, which needs --station. Writing a record changes the
configuration of the device.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := wt.validate(); err != nil {
				return err
			}
			rec, err := wr.record()
			if err != nil {
				return err
			}
			b, err := dataArg(data, dataFile)
			if err != nil {
				return err
			}
			if err := wv.validate(&wt); err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				w, err := a.openWriter(ctx, &wt, &wv)
				if err != nil {
					return nil, err
				}
				defer w.close()
				if err := w.write(ctx, rec, b); err != nil {
					return nil, classify("write "+rec.String(), err)
				}
				mac, station := w.device()
				return changeResult{MAC: mac, Station: station, Operation: "record write",
					Value: fmt.Sprintf("%s: %d octets", rec, len(b))}, nil
			})
		},
	}
	addRPCTargetFlags(write, &wt)
	addRecordFlags(write, &wr, 0, 1, "Record index (decimal or 0x hex); required")
	write.Flags().StringVar(&data, "data", "", "Record data in hex")
	write.Flags().StringVar(&dataFile, "data-file", "", "Read the record data from this file (binary)")
	addWriteViaFlags(write, &wv)
	clihelp.MustMarkFlagRequired(write, "index")
	g.AddCommand(read, write)
	return g
}

func dataArg(data, file string) ([]byte, error) {
	switch {
	case data != "" && file != "":
		return nil, errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination, "--data and --data-file both give the data: give one", nil)
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("--data-file: %v", err), err)
		}
		return b, nil
	case data != "":
		b, err := parseHex(data)
		if err != nil {
			return nil, errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--data: %v", err), err)
		}
		return b, nil
	default:
		return nil, errs.New(errs.KindUsage, errs.CodeInvalidInput, "no data: give --data HEX or --data-file FILE", nil)
	}
}

// writeVia selects the AR of a write: a device access AR, or with a device
// description an IOCAR.
type writeVia struct {
	gsdml, dap, model string
}

func addWriteViaFlags(c *cobra.Command, w *writeVia) {
	c.Flags().StringVar(&w.gsdml, "gsdml", "", "Write in an IOCAR with the expected configuration of this GSDML file (for devices without device access)")
	c.Flags().StringVar(&w.dap, "dap", "", "With --gsdml: the device access point ID")
	c.Flags().StringVar(&w.model, "model", "", "Write in an IOCAR with the expected configuration of this model")
}

func (w *writeVia) iocar() bool { return w.gsdml != "" || w.model != "" }

func (w *writeVia) validate(t *target) error {
	if w.iocar() && t.station == "" {
		return errs.New(errs.KindUsage, errs.CodeInvalidFlagCombination, "a write in an IOCAR (--gsdml, --model) finds the device by --station", nil)
	}
	return nil
}

// recordWriter reads and writes records within an AR.
type recordWriter struct {
	write  func(ctx context.Context, rec controller.Record, data []byte) error
	read   func(ctx context.Context, rec controller.Record) ([]byte, error)
	device func() (mac, station string)
	close  func()
}

// openWriter establishes the AR of a write.
func (a *app) openWriter(ctx context.Context, t *target, w *writeVia) (*recordWriter, error) {
	bound := func(ctx context.Context) (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, a.timeout)
	}
	if w.iocar() {
		o := &arOptions{station: t.station, gsdml: w.gsdml, dap: w.dap, modelFile: w.model, sendClock: 32, reductionRatio: 8,
			watchdog: defaultWatchdog, dataHold: defaultDataHold, connectTimeout: 20 * time.Second, rpcPort: rpc.Port,
			controllerName: "profinetctl", activityTimeout: 10 * time.Second}
		p, err := o.parse()
		if err != nil {
			return nil, err
		}
		s, err := a.establish(ctx, o, p)
		if err != nil {
			return nil, err
		}
		return &recordWriter{
			write: func(ctx context.Context, rec controller.Record, data []byte) error {
				cctx, cancel := bound(ctx)
				defer cancel()
				return s.dev.WriteRecord(cctx, rec, data)
			},
			read: func(ctx context.Context, rec controller.Record) ([]byte, error) {
				cctx, cancel := bound(ctx)
				defer cancel()
				return s.dev.ReadRecord(cctx, rec, 0)
			},
			device: func() (string, string) { return s.dev.MAC().String(), s.dev.Name() },
			close:  s.close,
		}, nil
	}
	s, err := a.openRPC(ctx, t)
	if err != nil {
		return nil, err
	}
	ar, err := s.supervisor(ctx)
	if err != nil {
		s.Close()
		var se *pnio.StatusError
		if errors.As(err, &se) {
			err = errs.New(errs.KindRefused, errs.CodePNIOError, err.Error()+": the device refuses device access ARs; give --gsdml or --model to write in an IOCAR", err)
		}
		return nil, err
	}
	return &recordWriter{
		write: func(ctx context.Context, rec controller.Record, data []byte) error {
			cctx, cancel := bound(ctx)
			defer cancel()
			return ar.Write(cctx, rec, data)
		},
		read: func(ctx context.Context, rec controller.Record) ([]byte, error) {
			cctx, cancel := bound(ctx)
			defer cancel()
			return ar.Read(cctx, rec, 0)
		},
		device: func() (string, string) { return s.id.MAC.String(), s.id.NameOfStation },
		close: func() {
			releaseAR(ctx, s, ar)
			s.Close()
		},
	}, nil
}

func releaseAR(ctx context.Context, s *rpcSession, ar *controller.SupervisorAR) {
	cctx, cancel := s.ctx(context.WithoutCancel(ctx))
	defer cancel()
	if err := ar.Release(cctx); err != nil {
		_, _ = fmt.Fprintf(s.a.stderr, "warning: release of the device access AR: %v (the device closes it after its activity timeout)\n", err)
	}
}

// imEntry is one I&M record.
type imEntry struct {
	Record string `json:"record"`
	Error  string `json:"error,omitempty"`
	IM0    *im0V  `json:"im0,omitempty"`
	IM1    *im1V  `json:"im1,omitempty"`
	IM2    *im2V  `json:"im2,omitempty"`
	IM3    *im3V  `json:"im3,omitempty"`
	IM4    *im4V  `json:"im4,omitempty"`
}

type im0V struct {
	VendorID         uint16   `json:"vendor_id"`
	OrderID          string   `json:"order_id"`
	SerialNumber     string   `json:"serial_number"`
	HardwareRevision uint16   `json:"hardware_revision"`
	SoftwareRevision string   `json:"software_revision"`
	RevisionCounter  uint16   `json:"revision_counter"`
	ProfileID        uint16   `json:"profile_id"`
	ProfileType      uint16   `json:"profile_specific_type"`
	Version          string   `json:"im_version"`
	Supported        []string `json:"supported"`
}

type im1V struct {
	TagFunction string `json:"tag_function"`
	TagLocation string `json:"tag_location"`
}

type im2V struct {
	Date string `json:"installation_date"`
}

type im3V struct {
	Descriptor string `json:"descriptor"`
}

type im4V struct {
	Signature string `json:"signature"`
}

func im0View(m pnio.IM0) *im0V {
	v := &im0V{VendorID: m.VendorID, OrderID: m.OrderID, SerialNumber: m.SerialNumber, HardwareRevision: m.HardwareRevision,
		SoftwareRevision: m.SoftwareRevision.String(), RevisionCounter: m.RevisionCounter, ProfileID: m.ProfileID, ProfileType: m.ProfileType,
		Version: fmt.Sprintf("%d.%d", m.VersionMajor, m.VersionMinor), Supported: []string{}}
	for i, bit := range []uint16{pnio.IMSupportIM1, pnio.IMSupportIM2, pnio.IMSupportIM3, pnio.IMSupportIM4} {
		if m.Supported&bit != 0 {
			v.Supported = append(v.Supported, fmt.Sprintf("I&M%d", i+1))
		}
	}
	return v
}

// imResult is the I&M records of a submodule.
type imResult struct {
	MAC     string    `json:"mac"`
	Station string    `json:"station"`
	Slot    uint16    `json:"slot"`
	Subslot uint16    `json:"subslot"`
	Records []imEntry `json:"records"`
}

// Partial reports records that could not be read.
func (r imResult) Partial() bool {
	for _, e := range r.Records {
		if e.Error != "" {
			return true
		}
	}
	return false
}

func (r imResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) slot %d subslot 0x%04X\n", orDash(r.Station), r.MAC, r.Slot, r.Subslot)
	for _, e := range r.Records {
		switch {
		case e.Error != "":
			fmt.Fprintf(&b, "%s: %s\n", e.Record, e.Error)
		case e.IM0 != nil:
			m := e.IM0
			fmt.Fprintf(&b, "I&M0: vendor 0x%04X, order %q, serial %q, hardware %d, software %s, revision counter %d, profile 0x%04X/0x%04X, I&M %s, supports %s\n",
				m.VendorID, m.OrderID, m.SerialNumber, m.HardwareRevision, m.SoftwareRevision, m.RevisionCounter, m.ProfileID, m.ProfileType, m.Version, orDash(strings.Join(m.Supported, ", ")))
		case e.IM1 != nil:
			fmt.Fprintf(&b, "I&M1: function %q, location %q\n", e.IM1.TagFunction, e.IM1.TagLocation)
		case e.IM2 != nil:
			fmt.Fprintf(&b, "I&M2: installation date %q\n", e.IM2.Date)
		case e.IM3 != nil:
			fmt.Fprintf(&b, "I&M3: descriptor %q\n", e.IM3.Descriptor)
		case e.IM4 != nil:
			fmt.Fprintf(&b, "I&M4: signature %s\n", e.IM4.Signature)
		}
	}
	return b.String(), nil
}

func (r imResult) TableHeaders() []string { return []string{"RECORD", "VALUE"} }

func (r imResult) TableRows() [][]string {
	var rows [][]string
	for _, e := range r.Records {
		v := e.Error
		switch {
		case e.IM0 != nil:
			v = fmt.Sprintf("order %q serial %q hw %d sw %s", e.IM0.OrderID, e.IM0.SerialNumber, e.IM0.HardwareRevision, e.IM0.SoftwareRevision)
		case e.IM1 != nil:
			v = fmt.Sprintf("function %q location %q", e.IM1.TagFunction, e.IM1.TagLocation)
		case e.IM2 != nil:
			v = e.IM2.Date
		case e.IM3 != nil:
			v = e.IM3.Descriptor
		case e.IM4 != nil:
			v = e.IM4.Signature
		}
		rows = append(rows, []string{e.Record, v})
	}
	return rows
}

// readIM reads one I&M record and decodes it.
func readIM(ctx context.Context, s *rpcSession, slot, subslot uint16, n int) imEntry {
	e := imEntry{Record: fmt.Sprintf("I&M%d", n)}
	cctx, cancel := s.ctx(ctx)
	defer cancel()
	b, err := s.rc.Read(cctx, rpc.NilUUID, controller.Record{Slot: slot, Subslot: subslot, Index: pnio.IndexIM0 + pnio.Index(n)}, 0)
	if err != nil {
		e.Error = classify("", err).Error()
		return e
	}
	blk, _, err := pnio.ReadBlock(b)
	if err == nil {
		switch n {
		case 0:
			var m pnio.IM0
			if m, err = pnio.DecodeIM0(blk); err == nil {
				e.IM0 = im0View(m)
			}
		case 1:
			var m pnio.IM1
			if m, err = pnio.DecodeIM1(blk); err == nil {
				e.IM1 = &im1V{TagFunction: m.TagFunction, TagLocation: m.TagLocation}
			}
		case 2:
			var m pnio.IM2
			if m, err = pnio.DecodeIM2(blk); err == nil {
				e.IM2 = &im2V{Date: m.Date}
			}
		case 3:
			var m pnio.IM3
			if m, err = pnio.DecodeIM3(blk); err == nil {
				e.IM3 = &im3V{Descriptor: m.Descriptor}
			}
		default:
			var m pnio.IM4
			if m, err = pnio.DecodeIM4(blk); err == nil {
				e.IM4 = &im4V{Signature: hex.EncodeToString(m.Signature[:])}
			}
		}
	}
	if err != nil {
		e.Error = classify("decode", err).Error()
	}
	return e
}

func newIMCmd(a *app) *cobra.Command {
	g := groupCmd("im", "Read and write the identification and maintenance records (I&M0-4)")
	var t target
	var slot, subslot uint16
	var which []int
	read := &cobra.Command{
		Use:   "read",
		Short: "Read I&M0 to I&M4 (ReadImplicit); exit code 7 when some cannot be read",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			for _, n := range which {
				if n < 0 || n > 4 {
					return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--record %d: I&M0 to I&M4 exist", n), nil)
				}
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.openRPC(ctx, &t)
				if err != nil {
					return nil, err
				}
				defer s.Close()
				r := imResult{MAC: s.id.MAC.String(), Station: s.id.NameOfStation, Slot: slot, Subslot: subslot}
				list := which
				if len(list) == 0 {
					// I&M0 says which of the others the device supports.
					first := readIM(ctx, s, slot, subslot, 0)
					r.Records = append(r.Records, first)
					if first.IM0 == nil {
						return nil, errs.New(errs.KindRefused, errs.CodePNIOError, "I&M0: "+first.Error, nil)
					}
					for i := 1; i <= 4; i++ {
						for _, sup := range first.IM0.Supported {
							if sup == fmt.Sprintf("I&M%d", i) {
								list = append(list, i)
							}
						}
					}
				}
				for _, n := range list {
					if n == 0 && len(which) == 0 {
						continue
					}
					r.Records = append(r.Records, readIM(ctx, s, slot, subslot, n))
				}
				return r, nil
			})
		},
	}
	addRPCTargetFlags(read, &t)
	read.Flags().Uint16Var(&slot, "slot", 0, "Slot of the submodule")
	read.Flags().Uint16Var(&subslot, "subslot", 1, "Subslot of the submodule (the DAP: slot 0, subslot 1)")
	read.Flags().IntSliceVar(&which, "record", nil, "Read only these records (0-4; default: I&M0 and those it lists as supported)")

	var wt target
	var wv writeVia
	var function, location, date, descriptor, signature string
	write := &cobra.Command{
		Use:   "write",
		Short: "Write I&M1 (tag), I&M2 (date), I&M3 (descriptor) or I&M4 (signature) of the DAP",
		Long: `Write the I&M records given on the command line to the DAP (slot 0, subslot 1)
in a device access AR. --function and --location write I&M1 together (the one
not given keeps its current value), --date I&M2 ("YYYY-MM-DD HH:MM"), --descriptor
I&M3 and --signature (54 octets in hex) I&M4.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := wt.validate(); err != nil {
				return err
			}
			f := c.Flags()
			im1 := f.Changed("function") || f.Changed("location")
			if !im1 && !f.Changed("date") && !f.Changed("descriptor") && !f.Changed("signature") {
				return errs.New(errs.KindUsage, errs.CodeInvalidInput, "nothing to write: give --function, --location, --date, --descriptor or --signature", nil)
			}
			for _, l := range []struct {
				flag, v string
				max     int
			}{{"function", function, pnio.IMTagFunctionLen}, {"location", location, pnio.IMTagLocationLen}, {"descriptor", descriptor, pnio.IMDescriptorLen}} {
				if len(l.v) > l.max {
					return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--%s: at most %d characters", l.flag, l.max), nil)
				}
			}
			if f.Changed("date") && !pnio.ValidIMDate(date) {
				return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--date %q: not \"YYYY-MM-DD\" or \"YYYY-MM-DD HH:MM\"", date), nil)
			}
			var sig pnio.IM4
			if f.Changed("signature") {
				b, err := parseHex(signature)
				if err != nil || len(b) > len(sig.Signature) {
					return errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("--signature: at most %d octets in hex", len(sig.Signature)), err)
				}
				copy(sig.Signature[:], b)
			}
			if err := wv.validate(&wt); err != nil {
				return err
			}
			return a.result(c, func(ctx context.Context) (any, error) {
				w, err := a.openWriter(ctx, &wt, &wv)
				if err != nil {
					return nil, err
				}
				defer w.close()
				var items []controller.WriteItem
				var written []string
				dap := func(n int) controller.Record {
					return controller.Record{Slot: 0, Subslot: 1, Index: pnio.IndexIM0 + pnio.Index(n)}
				}
				if im1 {
					m := pnio.IM1{}
					if b, err := w.read(ctx, dap(1)); err == nil {
						if blk, _, err := pnio.ReadBlock(b); err == nil {
							if cur, err := pnio.DecodeIM1(blk); err == nil {
								m = cur
							}
						}
					}
					if f.Changed("function") {
						m.TagFunction = function
					}
					if f.Changed("location") {
						m.TagLocation = location
					}
					items = append(items, controller.WriteItem{Record: dap(1), Data: m.Append(nil)})
					written = append(written, "I&M1")
				}
				if f.Changed("date") {
					m := pnio.IM2{Date: date}
					items = append(items, controller.WriteItem{Record: dap(2), Data: m.Append(nil)})
					written = append(written, "I&M2")
				}
				if f.Changed("descriptor") {
					m := pnio.IM3{Descriptor: descriptor}
					items = append(items, controller.WriteItem{Record: dap(3), Data: m.Append(nil)})
					written = append(written, "I&M3")
				}
				if f.Changed("signature") {
					items = append(items, controller.WriteItem{Record: dap(4), Data: sig.Append(nil)})
					written = append(written, "I&M4")
				}
				for i, it := range items {
					if err := w.write(ctx, it.Record, it.Data); err != nil {
						return nil, classify("write "+written[i], err)
					}
				}
				mac, station := w.device()
				return changeResult{MAC: mac, Station: station, Operation: "im write", Value: strings.Join(written, ", ")}, nil
			})
		},
	}
	addRPCTargetFlags(write, &wt)
	write.Flags().StringVar(&function, "function", "", "I&M1 tag function (32 characters)")
	write.Flags().StringVar(&location, "location", "", "I&M1 tag location (22 characters)")
	write.Flags().StringVar(&date, "date", "", "I&M2 installation date")
	write.Flags().StringVar(&descriptor, "descriptor", "", "I&M3 descriptor (54 characters)")
	write.Flags().StringVar(&signature, "signature", "", "I&M4 signature in hex")
	addWriteViaFlags(write, &wv)
	g.AddCommand(read, write)
	return g
}

// diagView is one diagnosis entry.
type diagView struct {
	API        uint32 `json:"api"`
	Slot       uint16 `json:"slot"`
	Subslot    uint16 `json:"subslot"`
	Channel    uint16 `json:"channel"`
	Direction  string `json:"direction"`
	Severity   string `json:"severity"`
	ErrorType  uint16 `json:"error_type"`
	Error      string `json:"error"`
	ExtError   uint16 `json:"ext_error_type,omitempty"`
	ExtValue   uint32 `json:"ext_add_value,omitempty"`
	Qualifier  uint32 `json:"qualifier,omitempty"`
	USI        uint16 `json:"usi"`
	VendorData string `json:"vendor_data,omitempty"`
}

var channelErrors = map[uint16]string{
	pnio.ChannelErrShortCircuit: "short circuit", pnio.ChannelErrUndervoltage: "undervoltage", pnio.ChannelErrOvervoltage: "overvoltage",
	pnio.ChannelErrOverload: "overload", pnio.ChannelErrOvertemp: "overtemperature", pnio.ChannelErrLineBreak: "line break",
	pnio.ChannelErrUpperLimit: "upper limit exceeded", pnio.ChannelErrLowerLimit: "lower limit exceeded", pnio.ChannelErrError: "error",
	pnio.ChannelErrDataTransmiss: "data transmission impossible", pnio.ChannelErrRemoteMismat: "remote mismatch",
}

func errorName(t uint16) string {
	if n, ok := channelErrors[t]; ok {
		return n
	}
	switch {
	case t >= 0x0100 && t <= 0x7FFF:
		return "manufacturer specific"
	default:
		return "reserved"
	}
}

func severity(p uint16) string {
	switch p & 0x0600 {
	case pnio.ChanMaintRequired:
		return "maintenance required"
	case pnio.ChanMaintDemanded:
		return "maintenance demanded"
	case pnio.ChanMaintQualified:
		return "qualified"
	default:
		return "fault"
	}
}

func direction(p uint16) string {
	switch p & (3 << 13) {
	case pnio.ChanDirInput:
		return "input"
	case pnio.ChanDirOutput:
		return "output"
	case pnio.ChanDirInOut:
		return "input/output"
	default:
		return "manufacturer"
	}
}

func diagViews(data []pnio.DiagnosisData) []diagView {
	out := []diagView{}
	for _, d := range data {
		if len(d.Items) == 0 {
			out = append(out, diagView{API: d.API, Slot: d.Slot, Subslot: d.Subslot, Channel: d.ChannelNumber, Direction: direction(d.ChannelProperties),
				Severity: severity(d.ChannelProperties), Error: "manufacturer specific", USI: d.USI, VendorData: hex.EncodeToString(d.Data)})
			continue
		}
		for _, it := range d.Items {
			out = append(out, diagView{API: d.API, Slot: d.Slot, Subslot: d.Subslot, Channel: it.ChannelNumber, Direction: direction(it.ChannelProperties),
				Severity: severity(it.ChannelProperties), ErrorType: it.ErrorType, Error: errorName(it.ErrorType),
				ExtError: it.ExtErrorType, ExtValue: it.ExtAddValue, Qualifier: it.Qualifier, USI: d.USI})
		}
	}
	return out
}

type diagnosticsResult struct {
	MAC     string     `json:"mac"`
	Station string     `json:"station"`
	Entries []diagView `json:"entries"`
}

func (r diagnosticsResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	if len(r.Entries) == 0 {
		fmt.Fprintf(&b, "%s: no diagnosis\n", orDash(r.Station))
	}
	for _, e := range r.Entries {
		fmt.Fprintf(&b, "slot %d subslot 0x%04X channel 0x%04X %s: %s (0x%04X), %s", e.Slot, e.Subslot, e.Channel, e.Direction, e.Error, e.ErrorType, e.Severity)
		if e.ExtError != 0 {
			fmt.Fprintf(&b, ", extended 0x%04X value 0x%08X", e.ExtError, e.ExtValue)
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func (r diagnosticsResult) TableHeaders() []string {
	return []string{"SLOT", "SUBSLOT", "CHANNEL", "DIRECTION", "ERROR", "TYPE", "SEVERITY"}
}

func (r diagnosticsResult) TableRows() [][]string {
	var rows [][]string
	for _, e := range r.Entries {
		rows = append(rows, []string{fmt.Sprint(e.Slot), fmt.Sprintf("0x%04X", e.Subslot), fmt.Sprintf("0x%04X", e.Channel), e.Direction, e.Error, fmt.Sprintf("0x%04X", e.ErrorType), e.Severity})
	}
	return rows
}

func newDiagnosticsCmd(a *app) *cobra.Command {
	var t target
	var slot, subslot uint16
	c := &cobra.Command{
		Use:     "diagnostics",
		Aliases: []string{"diag"},
		Short:   "Read the diagnosis of a device or of one submodule",
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := t.validate(); err != nil {
				return err
			}
			sub := c.Flags().Changed("slot") || c.Flags().Changed("subslot")
			return a.result(c, func(ctx context.Context) (any, error) {
				s, err := a.openRPC(ctx, &t)
				if err != nil {
					return nil, err
				}
				defer s.Close()
				cctx, cancel := s.ctx(ctx)
				defer cancel()
				d, err := s.rc.ReadDiagnosis(cctx, rpc.NilUUID, slot, subslot, sub)
				if err != nil {
					return nil, classify("read diagnosis", err)
				}
				return diagnosticsResult{MAC: s.id.MAC.String(), Station: s.id.NameOfStation, Entries: diagViews(d)}, nil
			})
		},
	}
	addRPCTargetFlags(c, &t)
	c.Flags().Uint16Var(&slot, "slot", 0, "Only this slot (with --subslot)")
	c.Flags().Uint16Var(&subslot, "subslot", 1, "Only this subslot")
	return c
}

// errorOf is the message of an error for a field of a result.
func errorOf(err error) string {
	if err == nil {
		return ""
	}
	var se *pnio.StatusError
	if errors.As(err, &se) {
		return se.Error()
	}
	return classify("", err).Error()
}
