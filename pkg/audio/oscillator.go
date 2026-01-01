// Package audio implements the audio synthesis engine
package audio

import (
	"math"

	"github.com/anthropics/abytetracker/pkg/tracker"
)

// Oscillator generates waveforms
type Oscillator struct {
	Type       tracker.Generator
	Phase      float64
	Frequency  float64
	SampleRate float64
	Duty       float64 // Duty cycle 0.0-1.0 (default 0.5 for square)
}

// NewOscillator creates a new oscillator
func NewOscillator(genType tracker.Generator, sampleRate float64) *Oscillator {
	return &Oscillator{
		Type:       genType,
		SampleRate: sampleRate,
		Duty:       0.5, // Default 50% duty
	}
}

// SetDuty sets the duty cycle (0.0 to 1.0)
func (o *Oscillator) SetDuty(duty float64) {
	if duty < 0.0 {
		duty = 0.0
	}
	if duty > 1.0 {
		duty = 1.0
	}
	o.Duty = duty
}

// SetFrequency sets the oscillator frequency
func (o *Oscillator) SetFrequency(freq float64) {
	o.Frequency = freq
}

// NoteToFreq converts MIDI note number to frequency
func NoteToFreq(note int8) float64 {
	// A4 = note 57 (9 + 4*12) = 440 Hz
	return 440.0 * math.Pow(2.0, float64(note-57)/12.0)
}

// Sample generates the next sample value (-1.0 to 1.0)
func (o *Oscillator) Sample() float64 {
	if o.Frequency <= 0 {
		return 0
	}

	// Advance phase
	phaseInc := o.Frequency / o.SampleRate
	o.Phase += phaseInc
	if o.Phase >= 1.0 {
		o.Phase -= 1.0
	}

	// Generate waveform
	switch o.Type {
	case tracker.GenTriangle:
		return o.triangle()
	case tracker.GenSawtooth:
		return o.sawtooth()
	case tracker.GenSquare:
		return o.square()
	case tracker.GenSawBig:
		return o.sawBig()
	case tracker.GenNoise:
		return o.noise()
	default:
		return 0
	}
}

// Triangle wave: /\/\/\
func (o *Oscillator) triangle() float64 {
	p := o.Phase
	if p < 0.5 {
		return 4.0*p - 1.0
	}
	return 3.0 - 4.0*p
}

// Sawtooth wave: /|/|/|
func (o *Oscillator) sawtooth() float64 {
	return 2.0*o.Phase - 1.0
}

// Square wave: _|-|_|-|
func (o *Oscillator) square() float64 {
	if o.Phase < o.Duty {
		return 1.0
	}
	return -1.0
}

// SawBig: 11-bit sawtooth like bytebeat swb
func (o *Oscillator) sawBig() float64 {
	// Mimics swb = x & 2047 in bytebeat
	val := int(o.Phase * 2048) & 2047
	return float64(val)/1024.0 - 1.0
}

// Noise: pseudo-random noise
func (o *Oscillator) noise() float64 {
	// LCG-based noise that depends on phase for determinism
	seed := uint32(o.Phase * 1000000)
	seed = seed*1103515245 + 12345
	return float64(int32(seed))/float64(math.MaxInt32)
}

// Reset resets the oscillator phase
func (o *Oscillator) Reset() {
	o.Phase = 0
}

// ChannelState holds the current state of a channel during playback
type ChannelState struct {
	Active      bool
	Oscillator  *Oscillator
	Instrument  int
	Note        int8
	BaseNote    int8    // Note before ornament/effects
	Volume      float64 // 0.0 to 1.0
	TargetVol   float64 // For envelope
	Frequency   float64

	// Envelope state
	EnvPhase    int     // 0=attack, 1=decay, 2=sustain, 3=release
	EnvPos      float64 // Position in current phase

	// Ornament state
	Ornament    int
	OrnPos      int
	OrnTick     int

	// Effect state
	PortaTarget float64 // Target frequency for portamento
	PortaSpeed  float64
	VibDepth    float64
	VibSpeed    float64
	VibPos      float64
	SlideSpeed  float64

	// Echo state
	EchoSource  int8
	EchoDelay   int
	EchoVolMod  float64
}

// NewChannelState creates a new channel state
func NewChannelState(sampleRate float64) *ChannelState {
	return &ChannelState{
		Oscillator: NewOscillator(tracker.GenTriangle, sampleRate),
		Volume:     0,
		EchoSource: -1,
	}
}

// TriggerNote starts a new note on the channel
func (cs *ChannelState) TriggerNote(note int8, inst *tracker.Instrument, volume int8) {
	cs.Active = true
	cs.Note = note
	cs.BaseNote = note
	cs.Frequency = NoteToFreq(note)
	cs.Oscillator.SetFrequency(cs.Frequency)
	cs.Oscillator.Reset()

	if inst != nil {
		cs.Oscillator.Type = inst.Generator
		cs.Ornament = int(inst.Ornament)
		// Set duty cycle from instrument (128 = 50%)
		if inst.Duty > 0 {
			cs.Oscillator.SetDuty(float64(inst.Duty) / 255.0)
		} else {
			cs.Oscillator.SetDuty(0.5) // Default 50%
		}
		if volume < 0 {
			cs.TargetVol = float64(inst.Volume) / 64.0
		}
	}

	if volume >= 0 {
		cs.TargetVol = float64(volume) / 64.0
	}

	// Reset envelope
	cs.EnvPhase = 0
	cs.EnvPos = 0
	cs.OrnPos = 0
	cs.OrnTick = 0
}

// NoteOff releases the note
func (cs *ChannelState) NoteOff() {
	cs.EnvPhase = 3 // Release
	cs.EnvPos = 0
}

// ProcessEnvelope updates the envelope and returns current volume multiplier
func (cs *ChannelState) ProcessEnvelope(env *tracker.Envelope, tickSamples int) float64 {
	if env == nil {
		cs.Volume = cs.TargetVol
		return cs.Volume
	}

	// Simple ADSR with tick-based timing
	switch cs.EnvPhase {
	case 0: // Attack
		if env.Attack == 0 {
			cs.Volume = cs.TargetVol
			cs.EnvPhase = 1
		} else {
			cs.EnvPos += 1.0 / float64(env.Attack)
			cs.Volume = cs.TargetVol * cs.EnvPos
			if cs.EnvPos >= 1.0 {
				cs.Volume = cs.TargetVol
				cs.EnvPhase = 1
				cs.EnvPos = 0
			}
		}
	case 1: // Decay
		if env.Decay == 0 {
			cs.EnvPhase = 2
		} else {
			sustainLevel := float64(env.Sustain) / 64.0 * cs.TargetVol
			cs.EnvPos += 1.0 / float64(env.Decay)
			cs.Volume = cs.TargetVol - (cs.TargetVol-sustainLevel)*cs.EnvPos
			if cs.EnvPos >= 1.0 {
				cs.Volume = sustainLevel
				cs.EnvPhase = 2
				cs.EnvPos = 0
			}
		}
	case 2: // Sustain
		cs.Volume = float64(env.Sustain) / 64.0 * cs.TargetVol
	case 3: // Release
		if env.Release == 0 || cs.Volume <= 0.001 {
			cs.Volume = 0
			cs.Active = false
		} else {
			cs.EnvPos += 1.0 / float64(env.Release)
			cs.Volume *= 1.0 - cs.EnvPos*0.1
			if cs.Volume <= 0.001 {
				cs.Volume = 0
				cs.Active = false
			}
		}
	}

	return cs.Volume
}

// ProcessOrnament applies ornament (semitone offset)
func (cs *ChannelState) ProcessOrnament(orn *tracker.Ornament) {
	if orn == nil || len(orn.Values) == 0 {
		return
	}

	// Get current ornament value
	offset := orn.Values[cs.OrnPos]

	// Apply semitone offset to frequency
	cs.Note = cs.BaseNote + offset
	cs.Frequency = NoteToFreq(cs.Note)
	cs.Oscillator.SetFrequency(cs.Frequency)

	// Advance ornament position
	cs.OrnTick++
	if cs.OrnTick >= 1 { // Advance every tick
		cs.OrnTick = 0
		cs.OrnPos++
		if cs.OrnPos >= len(orn.Values) {
			if orn.Loop >= 0 {
				cs.OrnPos = int(orn.Loop)
			} else {
				cs.OrnPos = len(orn.Values) - 1
			}
		}
	}
}

// GenerateSample generates the next audio sample for this channel
func (cs *ChannelState) GenerateSample() float64 {
	if !cs.Active || cs.Volume <= 0 {
		return 0
	}
	return cs.Oscillator.Sample() * cs.Volume
}
