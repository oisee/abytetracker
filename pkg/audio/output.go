package audio

import (
	"encoding/binary"
	"io"
	"sync"
)

// Output manages audio output
type Output struct {
	Player     *Player
	SampleRate int
	BufferSize int

	mu      sync.Mutex
	running bool
}

// NewOutput creates a new audio output
func NewOutput(player *Player) *Output {
	return &Output{
		Player:     player,
		SampleRate: player.SampleRate,
		BufferSize: 4096,
	}
}

// AudioReader implements io.Reader for audio output
type AudioReader struct {
	output *Output
	buffer []float64
	pos    int
}

// NewAudioReader creates an io.Reader that generates audio
func (o *Output) NewAudioReader() *AudioReader {
	return &AudioReader{
		output: o,
		buffer: make([]float64, o.BufferSize),
	}
}

// Read implements io.Reader - generates audio samples
func (ar *AudioReader) Read(p []byte) (n int, err error) {
	// Generate samples into buffer if needed
	if ar.pos >= len(ar.buffer) {
		ar.output.Player.GenerateSamples(ar.buffer)
		ar.pos = 0
	}

	// Convert float samples to 16-bit PCM
	for n = 0; n+2 <= len(p) && ar.pos < len(ar.buffer); n += 2 {
		sample := ar.buffer[ar.pos]
		ar.pos++

		// Clamp
		if sample > 1.0 {
			sample = 1.0
		}
		if sample < -1.0 {
			sample = -1.0
		}

		// Convert to 16-bit signed
		s16 := int16(sample * 32767)
		binary.LittleEndian.PutUint16(p[n:], uint16(s16))
	}

	return n, nil
}

// WAVWriter writes audio to WAV format
type WAVWriter struct {
	writer      io.Writer
	sampleRate  int
	channels    int
	dataWritten int
}

// NewWAVWriter creates a WAV writer
func NewWAVWriter(w io.Writer, sampleRate, channels int) *WAVWriter {
	return &WAVWriter{
		writer:     w,
		sampleRate: sampleRate,
		channels:   channels,
	}
}

// WriteHeader writes the WAV header
func (w *WAVWriter) WriteHeader(dataSize int) error {
	// RIFF header
	w.writer.Write([]byte("RIFF"))
	binary.Write(w.writer, binary.LittleEndian, uint32(dataSize+36))
	w.writer.Write([]byte("WAVE"))

	// fmt chunk
	w.writer.Write([]byte("fmt "))
	binary.Write(w.writer, binary.LittleEndian, uint32(16))          // Chunk size
	binary.Write(w.writer, binary.LittleEndian, uint16(1))           // PCM format
	binary.Write(w.writer, binary.LittleEndian, uint16(w.channels))  // Channels
	binary.Write(w.writer, binary.LittleEndian, uint32(w.sampleRate)) // Sample rate
	byteRate := w.sampleRate * w.channels * 2
	binary.Write(w.writer, binary.LittleEndian, uint32(byteRate))    // Byte rate
	blockAlign := w.channels * 2
	binary.Write(w.writer, binary.LittleEndian, uint16(blockAlign))  // Block align
	binary.Write(w.writer, binary.LittleEndian, uint16(16))          // Bits per sample

	// data chunk header
	w.writer.Write([]byte("data"))
	binary.Write(w.writer, binary.LittleEndian, uint32(dataSize))

	return nil
}

// WriteSamples writes float samples as 16-bit PCM
func (w *WAVWriter) WriteSamples(samples []float64) error {
	for _, s := range samples {
		if s > 1.0 {
			s = 1.0
		}
		if s < -1.0 {
			s = -1.0
		}
		s16 := int16(s * 32767)
		if err := binary.Write(w.writer, binary.LittleEndian, s16); err != nil {
			return err
		}
		w.dataWritten += 2
	}
	return nil
}

// Export exports the song to WAV
func ExportWAV(player *Player, writer io.Writer, durationSeconds float64) error {
	sampleRate := player.SampleRate
	totalSamples := int(durationSeconds * float64(sampleRate))
	dataSize := totalSamples * 2 // 16-bit mono

	wavWriter := NewWAVWriter(writer, sampleRate, 1)
	if err := wavWriter.WriteHeader(dataSize); err != nil {
		return err
	}

	// Reset player
	player.SetPosition(0, 0)
	player.Play()

	// Generate in chunks
	chunkSize := 4096
	buffer := make([]float64, chunkSize)
	for written := 0; written < totalSamples; {
		remaining := totalSamples - written
		if remaining < chunkSize {
			buffer = buffer[:remaining]
		}
		player.GenerateSamples(buffer)
		if err := wavWriter.WriteSamples(buffer); err != nil {
			return err
		}
		written += len(buffer)
	}

	player.Stop()
	return nil
}
