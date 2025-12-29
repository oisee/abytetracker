package audio

import (
	"sync"

	"github.com/anthropics/abytetracker/pkg/tracker"
)

// Player manages song playback
type Player struct {
	Song       *tracker.Song
	Channels   []*ChannelState
	SampleRate int

	// Playback state
	Playing      bool
	Position     int // Current position in order
	Pattern      int // Current pattern
	Row          int // Current row
	Tick         int // Current tick within row

	// Timing
	TickSamples  int // Samples per tick
	TickCounter  int // Sample counter for current tick

	// Echo buffers (per channel)
	EchoBuffers [][]float64
	EchoPos     []int

	mu sync.Mutex
}

// NewPlayer creates a new player for a song
func NewPlayer(song *tracker.Song) *Player {
	p := &Player{
		Song:       song,
		SampleRate: song.SampleRate,
		Channels:   make([]*ChannelState, song.Channels),
	}

	for i := range p.Channels {
		p.Channels[i] = NewChannelState(float64(song.SampleRate))
		if i < len(song.ChanConfig) {
			p.Channels[i].Oscillator.Type = song.ChanConfig[i].Generator
			p.Channels[i].EchoSource = song.ChanConfig[i].EchoSource
			p.Channels[i].EchoDelay = int(song.ChanConfig[i].EchoDelay)
			p.Channels[i].EchoVolMod = float64(song.ChanConfig[i].EchoVolume) / 64.0
		}
	}

	// Initialize echo buffers (1 second max delay)
	p.EchoBuffers = make([][]float64, song.Channels)
	p.EchoPos = make([]int, song.Channels)
	for i := range p.EchoBuffers {
		p.EchoBuffers[i] = make([]float64, song.SampleRate)
	}

	p.UpdateTiming()
	return p
}

// UpdateTiming recalculates timing based on speed/tempo
func (p *Player) UpdateTiming() {
	// Classic tracker timing:
	// Ticks per second = Tempo * 2 / 5
	// Samples per tick = SampleRate / (Tempo * 2 / 5)
	ticksPerSecond := float64(p.Song.Tempo) * 2.0 / 5.0
	p.TickSamples = int(float64(p.SampleRate) / ticksPerSecond)
}

// Play starts playback
func (p *Player) Play() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Playing = true
}

// Stop stops playback
func (p *Player) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Playing = false
	// Silence all channels
	for _, ch := range p.Channels {
		ch.Active = false
		ch.Volume = 0
	}
}

// SetPosition sets the playback position
func (p *Player) SetPosition(pos, row int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Position = pos
	if pos < len(p.Song.Order) {
		p.Pattern = int(p.Song.Order[pos])
	}
	p.Row = row
	p.Tick = 0
	p.TickCounter = 0
}

// ProcessRow processes the current row, triggering notes and effects
func (p *Player) ProcessRow() {
	if p.Pattern >= len(p.Song.Patterns) {
		return
	}
	pat := p.Song.Patterns[p.Pattern]
	if p.Row >= pat.Rows {
		return
	}

	for ch := 0; ch < p.Song.Channels && ch < len(pat.Notes[p.Row]); ch++ {
		note := pat.Notes[p.Row][ch]
		cs := p.Channels[ch]

		// Handle note
		if note.Pitch == -2 {
			// Note off
			cs.NoteOff()
		} else if note.Pitch >= 0 {
			// Note on
			var inst *tracker.Instrument
			instNum := int(note.Instrument) - 1
			if instNum >= 0 && instNum < len(p.Song.Instruments) {
				inst = &p.Song.Instruments[instNum]
				cs.Instrument = instNum
			} else if cs.Instrument >= 0 && cs.Instrument < len(p.Song.Instruments) {
				inst = &p.Song.Instruments[cs.Instrument]
			}
			cs.TriggerNote(note.Pitch, inst, note.Volume)
		}

		// Handle effect
		p.processEffect(ch, note.Effect)
	}
}

// processEffect handles effect commands
func (p *Player) processEffect(ch int, fx tracker.Effect) {
	if fx.Type == 0 && fx.Param == 0 {
		return
	}

	cs := p.Channels[ch]

	switch fx.Type {
	case tracker.FxArpeggio:
		if fx.Param != 0 {
			// Store arpeggio values for tick-based processing
			// x = semitones for tick 1, y = semitones for tick 2
			_ = int8((fx.Param >> 4) & 0x0F) // x
			_ = int8(fx.Param & 0x0F)        // y
			cs.BaseNote = cs.Note
		}

	case tracker.FxSlideUp:
		cs.SlideSpeed = float64(fx.Param) * 4

	case tracker.FxSlideDown:
		cs.SlideSpeed = -float64(fx.Param) * 4

	case tracker.FxPortamento:
		cs.PortaSpeed = float64(fx.Param) * 4

	case tracker.FxVibrato:
		cs.VibSpeed = float64((fx.Param >> 4) & 0x0F)
		cs.VibDepth = float64(fx.Param & 0x0F)

	case tracker.FxVolume:
		if fx.Param <= 64 {
			cs.TargetVol = float64(fx.Param) / 64.0
			cs.Volume = cs.TargetVol
		}

	case tracker.FxSpeed:
		if fx.Param < 32 {
			p.Song.Speed = fx.Param
		} else {
			p.Song.Tempo = fx.Param
			p.UpdateTiming()
		}

	case tracker.FxOrnament:
		if int(fx.Param) < len(p.Song.Ornaments) {
			cs.Ornament = int(fx.Param)
			cs.OrnPos = 0
			cs.OrnTick = 0
		}

	case tracker.FxEcho:
		// Exy: echo channel x with delay y rows
		srcCh := int8((fx.Param >> 4) & 0x0F)
		delay := int(fx.Param & 0x0F)
		cs.EchoSource = srcCh
		cs.EchoDelay = delay
	}
}

// ProcessTick processes effects that happen every tick
func (p *Player) ProcessTick() {
	for ch := 0; ch < p.Song.Channels; ch++ {
		cs := p.Channels[ch]
		if !cs.Active {
			continue
		}

		// Apply ornament
		if cs.Ornament > 0 && cs.Ornament <= len(p.Song.Ornaments) {
			orn := &p.Song.Ornaments[cs.Ornament-1]
			cs.ProcessOrnament(orn)
		}

		// Apply vibrato
		if cs.VibDepth > 0 {
			cs.VibPos += cs.VibSpeed * 0.1
			vibOffset := cs.VibDepth * 0.5 * (1.0 + 0.5*vibOffset(cs.VibPos))
			freq := NoteToFreq(cs.BaseNote) * (1.0 + vibOffset/100.0)
			cs.Oscillator.SetFrequency(freq)
		}

		// Apply slide
		if cs.SlideSpeed != 0 {
			cs.Frequency += cs.SlideSpeed
			if cs.Frequency < 20 {
				cs.Frequency = 20
			}
			if cs.Frequency > 20000 {
				cs.Frequency = 20000
			}
			cs.Oscillator.SetFrequency(cs.Frequency)
		}

		// Apply portamento
		if cs.PortaSpeed > 0 && cs.PortaTarget > 0 {
			if cs.Frequency < cs.PortaTarget {
				cs.Frequency += cs.PortaSpeed
				if cs.Frequency > cs.PortaTarget {
					cs.Frequency = cs.PortaTarget
				}
			} else if cs.Frequency > cs.PortaTarget {
				cs.Frequency -= cs.PortaSpeed
				if cs.Frequency < cs.PortaTarget {
					cs.Frequency = cs.PortaTarget
				}
			}
			cs.Oscillator.SetFrequency(cs.Frequency)
		}

		// Process envelope
		var env *tracker.Envelope
		if cs.Instrument >= 0 && cs.Instrument < len(p.Song.Instruments) {
			env = &p.Song.Instruments[cs.Instrument].Envelope
		}
		cs.ProcessEnvelope(env, p.TickSamples)
	}
}

func vibOffset(pos float64) float64 {
	// Simple sine vibrato
	return float64(int(pos*256)&255-128) / 128.0
}

// GenerateSamples generates audio samples into the buffer
func (p *Player) GenerateSamples(buffer []float64) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range buffer {
		if p.Playing {
			// Check if we need to advance
			p.TickCounter++
			if p.TickCounter >= p.TickSamples {
				p.TickCounter = 0
				p.Tick++

				if p.Tick == 0 {
					// First tick of row - process row
					p.ProcessRow()
				}

				// Process tick effects
				p.ProcessTick()

				if p.Tick >= int(p.Song.Speed) {
					// Advance to next row
					p.Tick = 0
					p.Row++

					if p.Pattern < len(p.Song.Patterns) && p.Row >= p.Song.Patterns[p.Pattern].Rows {
						// Advance to next position
						p.Row = 0
						p.Position++
						if p.Position >= len(p.Song.Order) {
							p.Position = 0 // Loop
						}
						p.Pattern = int(p.Song.Order[p.Position])
					}
				}
			}
		}

		// Generate samples from all channels
		var sample float64
		for ch := 0; ch < len(p.Channels); ch++ {
			cs := p.Channels[ch]
			chSample := cs.GenerateSample()

			// Apply channel volume
			if ch < len(p.Song.ChanConfig) && !p.Song.ChanConfig[ch].Muted {
				chSample *= float64(p.Song.ChanConfig[ch].Volume) / 64.0
			}

			// Store in echo buffer
			p.EchoBuffers[ch][p.EchoPos[ch]] = chSample
			p.EchoPos[ch] = (p.EchoPos[ch] + 1) % len(p.EchoBuffers[ch])

			// Handle echo channel
			if cs.EchoSource >= 0 && int(cs.EchoSource) < len(p.Channels) && cs.EchoDelay > 0 {
				// Calculate delay in samples
				delaySamples := cs.EchoDelay * p.TickSamples * int(p.Song.Speed)
				srcCh := int(cs.EchoSource)
				echoIdx := (p.EchoPos[srcCh] - delaySamples + len(p.EchoBuffers[srcCh])) % len(p.EchoBuffers[srcCh])
				echoSample := p.EchoBuffers[srcCh][echoIdx] * (1.0 + cs.EchoVolMod)
				chSample += echoSample
			}

			sample += chSample
		}

		// Simple limiter
		if sample > 1.0 {
			sample = 1.0
		}
		if sample < -1.0 {
			sample = -1.0
		}

		buffer[i] = sample
	}
}

// GetPlaybackInfo returns current playback position
func (p *Player) GetPlaybackInfo() (pos, pat, row, tick int, playing bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Position, p.Pattern, p.Row, p.Tick, p.Playing
}
