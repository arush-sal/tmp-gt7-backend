// Throwaway: decode a VictoryDash telemetry.json export (one driven GT7 lap) to a
// full-field CSV, reusing the backend's packet decoder. VictoryDash already gives
// decrypted 368-byte format-C GT7 packets (magic "0S7G" at offset 0) as arrays of
// ints in "rawUdpPackets", so Decrypt is skipped entirely: each packet's bytes go
// straight to protocol.Decode.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/arush-sal/garage/backend/collector/protocol"
)

// telemetry mirrors the VictoryDash telemetry.json shape we care about.
type telemetry struct {
	RawUDPPackets [][]int `json:"rawUdpPackets"`
	LapTimeMs     int     `json:"lapTimeMs"`
}

// nulTrim returns the ASCII string up to the first NUL (for CarCategory/SurfaceType).
func nulTrim(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: vd2csv <telemetry.json> <out.csv>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	must(err)

	var t telemetry
	must(json.Unmarshal(raw, &t))

	out, err := os.Create(os.Args[2])
	must(err)
	defer func() { must(out.Close()) }()
	w := bufio.NewWriter(out)
	defer func() { must(w.Flush()) }()
	_, err = fmt.Fprintln(w, "package_id,fmt,current_lap,in_race,time_on_track_ms,pos_x,pos_y,pos_z,"+
		"vel_x,vel_y,vel_z,rot_pitch,rot_yaw,rot_roll,angvel_x,angvel_y,angvel_z,"+
		"car_speed,throttle,brake,gear,slip_fl,slip_fr,slip_rl,slip_rr,"+
		// format-C fields (always populated: VictoryDash packets are all 368-byte format C)
		"wheel_steer_l,wheel_steer_r,wheel_base,steer_ang_vel,wheel_rotation,"+
		"sway,heave,surge,throttle_filtered,brake_filtered,surface_type,car_category,current_lap_time_ms")
	must(err)

	var kept, lastPID uint32
	var seen bool
	nC := 0
	for _, p := range t.RawUDPPackets {
		if len(p) != 368 {
			continue // not a format-C packet
		}
		pkt := make([]byte, len(p))
		for i, v := range p {
			pkt[i] = byte(v)
		}
		d, err := protocol.Decode(pkt)
		if err != nil {
			continue
		}
		if seen && d.PackageID == lastPID {
			continue // dedup repeats
		}
		lastPID, seen = d.PackageID, true
		nC++
		_, err = fmt.Fprintf(w, "%d,%d,%d,%t,%d,%.4f,%.4f,%.4f,%.4f,%.4f,%.4f,%.5f,%.5f,%.5f,%.5f,%.5f,%.5f,%.3f,%.2f,%.2f,%d,%.4f,%.4f,%.4f,%.4f,"+
			"%.5f,%.5f,%.4f,%.5f,%.5f,%.4f,%.4f,%.4f,%d,%d,%s,%s,%d\n",
			d.PackageID, 368, d.CurrentLap, d.InRace, d.TimeOnTrack.Milliseconds(),
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
	fmt.Fprintf(os.Stderr, "read %d packets, decoded %d telemetry frames (%d format-C) -> %s\n", len(t.RawUDPPackets), kept, nC, os.Args[2])
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
