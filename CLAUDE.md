# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Project Overview

ABYTETRACKER is a TUI (Terminal User Interface) music tracker written in Go, inspired by classic trackers like ProTracker, FastTracker, and Impulse Tracker. It features ZX Spectrum-style ornaments and bytebeat-inspired oscillators.

## Key Commands

```bash
# Build
go build -o abytetracker ./cmd/tracker

# Run
./abytetracker                      # New 6-channel song
./abytetracker songs/bossabeat.abt  # Load existing song
./abytetracker -channels 8          # New 8-channel song

# Cross-compile for Mac
GOOS=darwin GOARCH=arm64 go build -o abytetracker-mac ./cmd/tracker
```

## Architecture

```
cmd/tracker/main.go       # Entry point, CLI args, file loading
pkg/
├── tracker/types.go      # Core types: Song, Pattern, Note, Instrument, Ornament
├── audio/
│   ├── oscillator.go     # Waveform generators (tri, saw, squ, swb, noi)
│   ├── player.go         # Playback engine, effects processing
│   └── output.go         # WAV export
├── format/abt.go         # .abt file format load/save
└── tui/model.go          # Bubbletea TUI interface
songs/
└── bossabeat.abt         # Demo song (converted from bytebeat)
```

## File Format (.abt)

Plain text format with sections:
- `[song]` - metadata (title, author, tempo, speed, rate, channels)
- `[instruments]` - ID | Name | Gen | ADSR | Orn | Vol
- `[ornaments]` - ID | Name | Loop | Values (semitones)
- `[channels]` - CH | Name | Gen | Vol | Pan | Echo
- `[order]` - pattern playback order
- `[pattern N]` - note data: `ROW | NOTE INS VOL FX | ...`

Cell format: `C-4 01 40 A04` (note, instrument, volume, effect)

## Keyboard Controls

- **Navigation**: ↑↓←→, Tab, PgUp/Dn, Home/End
- **Note input**: Z-M (lower octave), Q-P (upper octave)
- **Octave**: * / to change
- **Playback**: Space (play/stop), F5 (from row), F8 (stop)
- **Export**: F9 (export to output.wav)
- **Help**: F1

## Current Status

### Working:
- TUI pattern editor with vertical scrolling
- File load/save (.abt format)
- WAV export (F9)
- Oscillators: triangle, sawtooth, square, sawbig (11-bit), noise
- Instruments with ADSR envelopes
- Ornaments (ZX Spectrum style arpeggio tables)
- Effects: arpeggio, slide, portamento, vibrato, volume, speed, ornament, echo
- Echo channel routing

### TODO:
- Real-time audio playback (needs oto integration)
- Sample-based instruments
- More effects (retrigger, cut, delay)
- Pattern copy/paste
- Song save (currently only load works in TUI)
- Undo/redo

## Related Projects

This tracker was created alongside an ABAP bytebeat engine (ZCL_BYTEBEAT in SAP) that implements similar synthesis. The Bossabeat song was originally a JavaScript bytebeat composition by Kouzerumatsukite.

## Dependencies

- github.com/charmbracelet/bubbletea - TUI framework
- github.com/charmbracelet/lipgloss - Styling
- (optional) github.com/ebitengine/oto/v3 - Real-time audio (not yet integrated)
