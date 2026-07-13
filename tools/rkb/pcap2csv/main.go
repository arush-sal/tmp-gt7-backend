// Throwaway: decode a GT7 .pcapng capture to a full-field CSV, reusing the backend's
// Salsa20 decrypt + packet decode. Skips link-layer parsing entirely: for each captured
// packet it tries to decrypt the trailing {368,344,296} bytes (format C / ~ / A) and keeps
// it iff the magic verifies. Format-C fields (steering/sway/surge/surface/...) populate only
// when the source is a 368-byte packet; they stay zero/empty on format-A captures.
package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"strings"

	"github.com/arush-sal/garage/backend/collector/protocol"
)

// candidate GT7 packet lengths, largest first: C(368), tilde(344), A(296).
var pktSizes = []int{368, 344, 296}

// nulTrim returns the ASCII string up to the first NUL (for CarCategory).
func nulTrim(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: pcap2csv <in.pcapng> <out.csv>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	must(err)
	pkts := parsePcapng(raw)

	out, err := os.Create(os.Args[2])
	must(err)
	defer func() { must(out.Close()) }()
	w := bufio.NewWriter(out)
	defer func() { must(w.Flush()) }()
	_, err = fmt.Fprintln(w, "package_id,fmt,current_lap,in_race,time_on_track_ms,pos_x,pos_y,pos_z,"+
		"vel_x,vel_y,vel_z,rot_pitch,rot_yaw,rot_roll,angvel_x,angvel_y,angvel_z,"+
		"car_speed,throttle,brake,gear,slip_fl,slip_fr,slip_rl,slip_rr,"+
		// format-C fields (zero/empty on format-A sources)
		"wheel_steer_l,wheel_steer_r,wheel_base,steer_ang_vel,wheel_rotation,"+
		"sway,heave,surge,throttle_filtered,brake_filtered,surface_type,car_category,current_lap_time_ms")
	must(err)

	var kept, lastPID uint32
	var seen bool
	nC := 0
	for _, p := range pkts {
		var dec []byte
		var fmtSz int
		for _, n := range pktSizes {
			if len(p) < n {
				continue
			}
			if d, err := protocol.Decrypt(p[len(p)-n:]); err == nil {
				dec, fmtSz = d, n
				break // first magic-valid length wins
			}
		}
		if dec == nil {
			continue // not a telemetry packet (heartbeat, wrong len, bad magic)
		}
		d, err := protocol.Decode(dec)
		if err != nil {
			continue
		}
		if seen && d.PackageID == lastPID {
			continue // dedup repeats
		}
		lastPID, seen = d.PackageID, true
		if fmtSz == 368 {
			nC++
		}
		_, err = fmt.Fprintf(w, "%d,%d,%d,%t,%d,%.4f,%.4f,%.4f,%.4f,%.4f,%.4f,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%.3f,%.2f,%.2f,%d,%.4f,%.4f,%.4f,%.4f,"+
			"%.5f,%.5f,%.4f,%.5f,%.5f,%.4f,%.4f,%.4f,%d,%d,%s,%s,%d\n",
			d.PackageID, fmtSz, d.CurrentLap, d.InRace, d.TimeOnTrack.Milliseconds(),
			d.PositionX, d.PositionY, d.PositionZ,
			d.VelocityX, d.VelocityY, d.VelocityZ,
			d.RotationPitch, d.RotationYaw, d.RotationRoll,
			d.AngularVelocityX, d.AngularVelocityY, d.AngularVelocityZ,
			d.CarSpeed, d.Throttle, d.Brake, d.CurrentGear,
			d.TyreSlipRatioFL, d.TyreSlipRatioFR, d.TyreSlipRatioRL, d.TyreSlipRatioRR,
			d.WheelSteeringAngle[0], d.WheelSteeringAngle[1], d.WheelBase, d.SteeringAngularVelocity, d.WheelRotation,
			d.Sway, d.Heave, d.Surge, d.ThrottleFiltered, d.BrakeFiltered,
			nulTrim(d.SurfaceType[:]), nulTrim(d.CarCategory[:]), d.CurrentLapTimeMs)
		must(err)
		kept++
	}
	fmt.Fprintf(os.Stderr, "parsed %d captured packets, decoded %d telemetry frames (%d format-C) -> %s\n", len(pkts), kept, nC, os.Args[2])
}

// parsePcapng walks pcapng blocks and returns the packet-data payload of every
// Enhanced (0x06) or Simple (0x03) Packet Block. Handles both byte orders via the SHB magic.
func parsePcapng(b []byte) [][]byte {
	var out [][]byte
	le := true // set from Section Header Block byte-order magic
	i := 0
	for i+8 <= len(b) {
		bo := order(le)
		btype := bo.Uint32(b[i:])
		tlen := int(bo.Uint32(b[i+4:]))
		if tlen < 12 || i+tlen > len(b) {
			break
		}
		body := b[i+8 : i+tlen-4]
		switch btype {
		case 0x0A0D0D0A: // Section Header Block: read byte-order magic
			if binary.LittleEndian.Uint32(body[0:]) == 0x1A2B3C4D {
				le = true
			} else {
				le = false
			}
		case 0x00000006: // Enhanced Packet Block: iface(4) tsHi(4) tsLo(4) capLen(4) origLen(4) data...
			o := order(le)
			capLen := int(o.Uint32(body[12:]))
			if 20+capLen <= len(body) {
				out = append(out, body[20:20+capLen])
			}
		case 0x00000003: // Simple Packet Block: origLen(4) data...
			o := order(le)
			origLen := int(o.Uint32(body[0:]))
			if 4+origLen <= len(body) {
				out = append(out, body[4:4+origLen])
			}
		}
		i += tlen
	}
	return out
}

func order(le bool) binary.ByteOrder {
	if le {
		return binary.LittleEndian
	}
	return binary.BigEndian
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
