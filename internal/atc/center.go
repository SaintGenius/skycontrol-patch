package atc

import (
	"fmt"
	"strings"

	"github.com/skycontrol/skycontrol/internal/airfield"
	"github.com/skycontrol/skycontrol/internal/radio"
)

// Center is 305.0 AM. Spoken name is always "Sky Control" — never "Center".
const (
	CenterHz       = 305_000_000
	CenterCallsign = "Sky Control"
)

func CenterFrequency() radio.Frequency {
	return radio.Frequency{Hz: CenterHz, Modulation: "AM"}
}

func IsCenterFreq(f radio.Frequency) bool {
	if f.Hz < 1_000_000 {
		return false
	}
	d := f.Hz - CenterHz
	if d < 0 {
		d = -d
	}
	return d < 500_000 // ±0.5 MHz
}

func (t *Tower) sayCenter(text string) {
	fmt.Println("  as Sky Control on 305.000")
	t.sayTX(radio.Transmission{
		Callsign:  CenterCallsign,
		Text:      SpeakForRadio(text),
		Spoken:    SpeakForRadio(text),
		Frequency: CenterFrequency(),
		Coalition: 0,
	}, text)
}

func (t *Tower) handleCenterCall(call radio.ReceivedCall) bool {
	text := strings.ToLower(strings.TrimSpace(call.Transcript))
	// Field is signing them off to us — Ground/Tower must speak first.
	if wantsCenter(text) && containsAny(text, "switch to", "switching to", "contacting", "contact ",
		"change to", "going over", "hand off", "handoff") {
		return false
	}
	if !IsCenterFreq(call.Frequency) && !wantsCenter(text) {
		return false
	}
	intent := DetectIntent(text)

	t.mu.Lock()
	st := t.identifyCaller(call)
	if st == nil {
		st = t.findByPilot(call.Pilot)
	}
	t.seedOwnerLocked(st)

	nearby, home := t.centerNearbyLocked(st)
	named := t.namedField(call.Transcript, nearby, home)
	dest := named
	if dest == nil {
		dest = home
	}
	if dest != nil && st != nil && named != nil && (st.Owner == nil || named.Name != st.Owner.Name) {
		st.Dest = dest
	}
	pilot := "Aircraft"
	if st != nil && st.Callsign != "" {
		pilot = st.Callsign
	} else if st != nil && st.Pilot != "" {
		pilot = SpeakCallsign(st.Pilot)
	} else if cs := heardCallsign(call); cs != "" {
		pilot = cs
	}
	onGround := st == nil || sittingOnField(st)
	if dest == nil && (intent == IntentATIS || intent == IntentRadioCheck) {
		dest = t.fallbackFieldLocked()
	}
	t.mu.Unlock()

	if intent == IntentRadioCheck {
		t.mu.Lock()
		t.centerReleases = 0
		t.mu.Unlock()
		role := RoleTower
		if onGround || containsAny(text, "ground") {
			role = RoleGround
		}
		if containsAny(text, "tower") {
			role = RoleTower
		}
		if dest != nil {
			t.sayCenter(t.centerRadioCheck(pilot, dest, role == RoleGround))
		} else {
			t.sayCenter(fmt.Sprintf("%s, %s, loud and clear.", pilot, CenterCallsign))
		}
		return true
	}

	contactTalk := containsAny(text, "switch to", "switching to", "contacting", "contact ",
		"change to", "changing to", "going over", "hand off", "handoff", "going to",
		"headed to", "heading to", "departing for", "enroute", "en route")
	infoTalk := containsAny(text, "how far", "distance", "bearing", "where's", "where is",
		"what is", "what's", "frequency of")

	clearance := intent == IntentTaxi || intent == IntentTakeoff || intent == IntentLanding ||
		intent == IntentInbound || intent == IntentStartup || intent == IntentParking ||
		intent == IntentHoldShort || intent == IntentTouchAndGo || intent == IntentGoAround ||
		intent == IntentContact || intent == IntentCheckIn

	if intent == IntentATIS && dest != nil {
		t.sayCenter(t.centerATISLine(pilot, dest))
		return true
	}

	if dest != nil && !infoTalk && (clearance || contactTalk || named != nil) {
		role := RoleGround
		if !onGround || intent == IntentTakeoff || intent == IntentLanding || intent == IntentInbound ||
			intent == IntentTouchAndGo || intent == IntentGoAround || intent == IntentCheckIn {
			role = RoleTower
		}
		if intent == IntentTaxi || intent == IntentStartup || intent == IntentParking || intent == IntentHoldShort {
			role = RoleGround
		}
		if containsAny(text, "ground") {
			role = RoleGround
		}
		if containsAny(text, "tower") {
			role = RoleTower
		}
		t.sayCenter(t.centerContactLine(pilot, dest, role))
		return true
	}

	// Info / distance / random — let ChatGPT speak, but still as Sky Control.
	return false
}

func (t *Tower) centerNearbyLocked(st *AircraftState) ([]airfield.Nearby, *airfield.Airfield) {
	var nearby []airfield.Nearby
	var home *airfield.Airfield
	if st == nil {
		return nearby, home
	}
	home = st.Owner
	if home == nil {
		home = st.Nearest
	}
	mapName := ""
	if home != nil {
		mapName = home.Map
	} else if st.Nearest != nil {
		mapName = st.Nearest.Map
	}
	if t.airfields != nil && mapName != "" {
		nearby = t.airfields.NearbyOnMap(mapName, st.Latitude, st.Longitude, 20)
	}
	push := func(af *airfield.Airfield) {
		if af == nil {
			return
		}
		for _, n := range nearby {
			if strings.EqualFold(n.Name, af.Name) {
				return
			}
		}
		nearby = append(nearby, airfield.Nearby{Name: af.Name, TowerFreq: af.PrimaryTowerFreq()})
	}
	push(st.Owner)
	push(st.Dest)
	push(st.Nearest)
	return nearby, home
}

func (t *Tower) fallbackFieldLocked() *airfield.Airfield {
	st := t.primaryLocked()
	if st == nil {
		return nil
	}
	if st.Owner != nil {
		return st.Owner
	}
	return st.Nearest
}

func (t *Tower) centerContactLine(pilot string, af *airfield.Airfield, role Role) string {
	if pilot == "" {
		pilot = "Aircraft"
	}
	if af == nil {
		return fmt.Sprintf("%s, %s, say again facility.", pilot, CenterCallsign)
	}
	cs := af.Callsign(airfield.Role(role))
	raw := af.PrimaryTowerFreq()
	if role == RoleGround {
		if g := af.PrimaryGroundFreq(); g != "" {
			raw = g
		}
	}
	freq := SpeakFrequency(raw)
	if freq == "" {
		freq = "this frequency"
	}
	fmt.Printf("  center: contact %s on %s\n", cs, raw)
	return fmt.Sprintf("%s, %s, contact %s on %s.", pilot, CenterCallsign, cs, freq)
}

func (t *Tower) centerRadioCheck(pilot string, af *airfield.Airfield, onGround bool) string {
	if pilot == "" {
		pilot = "Aircraft"
	}
	if af == nil {
		return fmt.Sprintf("%s, %s, loud and clear.", pilot, CenterCallsign)
	}
	role := RoleTower
	if onGround && len(af.Frequencies.Ground) > 0 {
		role = RoleGround
	}
	cs := af.Callsign(airfield.Role(role))
	raw := af.PrimaryTowerFreq()
	if role == RoleGround {
		if g := af.PrimaryGroundFreq(); g != "" {
			raw = g
		}
	}
	freq := SpeakFrequency(raw)
	if freq == "" {
		return fmt.Sprintf("%s, %s, loud and clear.", pilot, CenterCallsign)
	}
	return fmt.Sprintf("%s, %s, loud and clear. Contact %s on %s.", pilot, CenterCallsign, cs, freq)
}

func (t *Tower) centerATISLine(pilot string, af *airfield.Airfield) string {
	if pilot == "" {
		pilot = "Aircraft"
	}
	if af == nil || len(af.Frequencies.ATIS) == 0 {
		return fmt.Sprintf("%s, %s, no ATIS this field.", pilot, CenterCallsign)
	}
	freq := SpeakFrequency(af.Frequencies.ATIS[0])
	return fmt.Sprintf("%s, %s, %s Information on %s, comm two.", pilot, CenterCallsign, af.Name, freq)
}

// sittingOnField: ramp / taxi / hold short even when Tacview skips OnGround (DCS hot start).
func sittingOnField(st *AircraftState) bool {
	if st == nil {
		return false
	}
	if st.OnGround {
		return true
	}
	switch st.Phase {
	case "parked", "taxi", "runway":
		return true
	}
	gs := st.SpeedMS * 1.94384
	if gs >= 40 || st.DistanceNM > 1.5 {
		return false
	}
	agl := 0.0
	if st.Nearest != nil {
		agl = st.aglFt(st.Nearest)
	}
	if st.AGLFt > 0 && (agl == 0 || st.AGLFt < agl) {
		agl = st.AGLFt
	}
	return agl < 250
}
