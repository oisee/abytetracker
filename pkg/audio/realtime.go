package audio

import (
	"encoding/binary"

	"github.com/ebitengine/oto/v3"
)

// RealtimeOutput manages real-time audio playback
type RealtimeOutput struct {
	player     *Player
	otoCtx     *oto.Context
	otoPlayer  *oto.Player
	buffer     []float64
	running    bool
}

// NewRealtimeOutput creates a new real-time audio output
func NewRealtimeOutput(player *Player) (*RealtimeOutput, error) {
	op := &oto.NewContextOptions{
		SampleRate:   player.SampleRate,
		ChannelCount: 1, // Mono
		Format:       oto.FormatSignedInt16LE,
	}

	otoCtx, ready, err := oto.NewContext(op)
	if err != nil {
		return nil, err
	}
	<-ready

	rt := &RealtimeOutput{
		player:  player,
		otoCtx:  otoCtx,
		buffer:  make([]float64, 512),
		running: true,
	}

	// Create audio stream
	rt.otoPlayer = otoCtx.NewPlayer(&audioStream{rt: rt})
	rt.otoPlayer.SetBufferSize(player.SampleRate / 10) // 100ms buffer
	rt.otoPlayer.Play()

	return rt, nil
}

// Close stops the audio output
func (rt *RealtimeOutput) Close() {
	rt.running = false
	if rt.otoPlayer != nil {
		rt.otoPlayer.Close()
	}
}

// audioStream implements io.Reader for oto
type audioStream struct {
	rt *RealtimeOutput
}

func (s *audioStream) Read(buf []byte) (int, error) {
	if !s.rt.running {
		// Fill with silence
		for i := range buf {
			buf[i] = 0
		}
		return len(buf), nil
	}

	// Generate samples
	samples := len(buf) / 2 // 16-bit = 2 bytes per sample
	if samples > len(s.rt.buffer) {
		s.rt.buffer = make([]float64, samples)
	}

	s.rt.player.GenerateSamples(s.rt.buffer[:samples])

	// Convert to 16-bit PCM
	for i := 0; i < samples; i++ {
		sample := s.rt.buffer[i]
		// Clamp
		if sample > 1.0 {
			sample = 1.0
		}
		if sample < -1.0 {
			sample = -1.0
		}
		s16 := int16(sample * 32767)
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s16))
	}

	return samples * 2, nil
}
