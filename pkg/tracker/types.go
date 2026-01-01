// Package tracker implements the core tracker data structures
package tracker

// Note represents a single note entry in a pattern
type Note struct {
	Pitch      int8   // 0-95 (C-0 to B-7), -1 = empty, -2 = note off
	Instrument uint8  // 0 = no change, 1-255 = instrument number
	Volume     int8   // 0-64, -1 = no change
	Effect     Effect // Effect command
}

// Effect represents a tracker effect command
type Effect struct {
	Type  uint8 // Effect type
	Param uint8 // Effect parameter
}

// Effect types
const (
	FxNone       uint8 = 0x00 // No effect
	FxArpeggio   uint8 = 0x00 // 0xy - Arpeggio (when param != 0)
	FxSlideUp    uint8 = 0x01 // 1xx - Slide up
	FxSlideDown  uint8 = 0x02 // 2xx - Slide down
	FxPortamento uint8 = 0x03 // 3xx - Portamento to note
	FxVibrato    uint8 = 0x04 // 4xy - Vibrato
	FxVolSlide   uint8 = 0x0A // Axy - Volume slide
	FxJump       uint8 = 0x0B // Bxx - Jump to position
	FxVolume     uint8 = 0x0C // Cxx - Set volume
	FxBreak      uint8 = 0x0D // Dxx - Pattern break
	FxEcho       uint8 = 0x0E // Exy - Echo channel x with delay y
	FxSpeed      uint8 = 0x0F // Fxx - Set speed (1-31) or tempo (32-255)
	FxOrnament   uint8 = 0x10 // Gxx - Set ornament
	FxDelay      uint8 = 0x11 // Hxx - Note delay
	FxRetrigger  uint8 = 0x12 // Ixy - Retrigger note
	FxCut        uint8 = 0x13 // Jxx - Cut note after xx ticks
	FxDuty       uint8 = 0x14 // Kxx - Set duty cycle (00-FF, 80=50%)
)

// Generator types for instruments
type Generator uint8

const (
	GenTriangle Generator = iota
	GenSawtooth
	GenSquare
	GenSawBig    // 11-bit sawtooth like bytebeat
	GenNoise
	GenSample    // Sample-based
	GenBytebeat  // Custom bytebeat formula
)

// Instrument defines a sound source
type Instrument struct {
	Name      string
	Generator Generator
	Sample    []int16   // For GenSample
	Formula   string    // For GenBytebeat
	Envelope  Envelope
	Ornament  uint8     // Default ornament (0 = none)
	Detune    int8      // Fine detune (-64 to +63)
	Volume    uint8     // Default volume (0-64)
	Duty      uint8     // Duty cycle for pulse wave (0-255, 128=50%)
}

// Envelope defines ADSR-like volume envelope
type Envelope struct {
	Attack  uint8 // Attack time (0-255)
	Decay   uint8 // Decay time
	Sustain uint8 // Sustain level (0-64)
	Release uint8 // Release time
	Loop    bool  // Loop sustain
}

// Ornament defines semitone offset pattern (ZX Spectrum style)
type Ornament struct {
	Name   string
	Loop   int8    // Loop point (-1 = no loop)
	Values []int8  // Semitone offsets per tick
}

// Pattern holds one pattern of notes
type Pattern struct {
	Rows     int      // Number of rows (typically 64)
	Channels int      // Number of channels
	Notes    [][]Note // [row][channel]
}

// NewPattern creates a new empty pattern
func NewPattern(rows, channels int) *Pattern {
	p := &Pattern{
		Rows:     rows,
		Channels: channels,
		Notes:    make([][]Note, rows),
	}
	for i := range p.Notes {
		p.Notes[i] = make([]Note, channels)
		for j := range p.Notes[i] {
			p.Notes[i][j] = Note{Pitch: -1, Volume: -1}
		}
	}
	return p
}

// ChannelConfig defines per-channel settings
type ChannelConfig struct {
	Name       string
	Generator  Generator // Default generator for channel
	Volume     uint8     // Channel volume (0-64)
	Pan        int8      // Pan (-64 left to +64 right)
	Muted      bool
	Solo       bool
	// Echo settings
	EchoSource int8  // -1 = none, or channel index (0-based)
	EchoDelay  uint8 // Delay in rows
	EchoVolume int8  // Volume offset (negative = quieter)
}

// Song represents a complete tracker song
type Song struct {
	Title       string
	Author      string
	Speed       uint8           // Ticks per row (1-31)
	Tempo       uint8           // BPM (32-255)
	SampleRate  int             // Audio sample rate
	Channels    int             // Number of channels

	Instruments []Instrument
	Ornaments   []Ornament
	Patterns    []*Pattern
	Order       []uint8         // Pattern order list
	ChanConfig  []ChannelConfig // Per-channel config
}

// NewSong creates a new song with defaults
func NewSong(channels int) *Song {
	if channels < 1 {
		channels = 4
	}

	s := &Song{
		Title:      "Untitled",
		Author:     "",
		Speed:      6,
		Tempo:      125,
		SampleRate: 44100,
		Channels:   channels,
		ChanConfig: make([]ChannelConfig, channels),
		Patterns:   []*Pattern{NewPattern(64, channels)},
		Order:      []uint8{0},
	}

	// Default channel config
	gens := []Generator{GenTriangle, GenSawtooth, GenSquare, GenSquare, GenNoise, GenNoise}
	names := []string{"Lead", "Bass", "Chord1", "Chord2", "Perc1", "Perc2"}
	for i := range s.ChanConfig {
		gen := GenTriangle
		name := "CH" + string(rune('1'+i))
		if i < len(gens) {
			gen = gens[i]
			name = names[i]
		}
		s.ChanConfig[i] = ChannelConfig{
			Name:       name,
			Generator:  gen,
			Volume:     64,
			Pan:        0,
			EchoSource: -1,
		}
	}

	// Default instruments
	s.Instruments = []Instrument{
		{Name: "Lead", Generator: GenTriangle, Volume: 64, Envelope: Envelope{Attack: 0, Decay: 20, Sustain: 48, Release: 30}},
		{Name: "Bass", Generator: GenSawtooth, Volume: 56, Envelope: Envelope{Attack: 0, Decay: 40, Sustain: 40, Release: 20}},
		{Name: "Pad", Generator: GenSquare, Volume: 40, Envelope: Envelope{Attack: 30, Decay: 10, Sustain: 50, Release: 50}},
		{Name: "Kick", Generator: GenNoise, Volume: 64, Envelope: Envelope{Attack: 0, Decay: 15, Sustain: 0, Release: 10}},
		{Name: "Snare", Generator: GenNoise, Volume: 56, Envelope: Envelope{Attack: 0, Decay: 25, Sustain: 0, Release: 15}},
		{Name: "HiHat", Generator: GenNoise, Volume: 32, Envelope: Envelope{Attack: 0, Decay: 8, Sustain: 0, Release: 5}},
	}

	// Default ornaments (ZX Spectrum style)
	s.Ornaments = []Ornament{
		{Name: "Arp Maj", Loop: 0, Values: []int8{0, 4, 7}},          // Major chord arpeggio
		{Name: "Arp Min", Loop: 0, Values: []int8{0, 3, 7}},          // Minor chord arpeggio
		{Name: "Arp 7th", Loop: 0, Values: []int8{0, 4, 7, 10}},      // Dominant 7th
		{Name: "Arp Oct", Loop: 0, Values: []int8{0, 12, 0, 12}},     // Octave
		{Name: "Vib ±1", Loop: 0, Values: []int8{0, 1, 0, -1}},       // Subtle vibrato
		{Name: "Vib ±2", Loop: 0, Values: []int8{0, 1, 2, 1, 0, -1, -2, -1}}, // Wider vibrato
		{Name: "SlideUp", Loop: -1, Values: []int8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}},
		{Name: "SlideDn", Loop: -1, Values: []int8{0, -1, -2, -3, -4, -5, -6, -7, -8, -9, -10, -11, -12}},
	}

	return s
}

// NoteToString converts a pitch to note name
func NoteToString(pitch int8) string {
	if pitch < 0 {
		if pitch == -2 {
			return "OFF"
		}
		return "---"
	}
	notes := []string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}
	octave := pitch / 12
	note := pitch % 12
	return notes[note] + string(rune('0'+octave))
}

// StringToNote converts note name to pitch
func StringToNote(s string) int8 {
	if len(s) < 3 || s == "---" {
		return -1
	}
	if s == "OFF" {
		return -2
	}

	notes := map[string]int8{
		"C-": 0, "C#": 1, "D-": 2, "D#": 3, "E-": 4, "F-": 5,
		"F#": 6, "G-": 7, "G#": 8, "A-": 9, "A#": 10, "B-": 11,
	}

	note, ok := notes[s[:2]]
	if !ok {
		return -1
	}
	octave := int8(s[2] - '0')
	return octave*12 + note
}
