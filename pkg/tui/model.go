// Package tui implements the terminal user interface
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anthropics/abytetracker/pkg/audio"
	"github.com/anthropics/abytetracker/pkg/tracker"
)

// EditMode represents what we're editing
type EditMode int

const (
	ModePattern EditMode = iota
	ModeInstrument
	ModeOrnament
	ModeOrder
)

// Column within a cell
type Column int

const (
	ColNote Column = iota
	ColInstrument
	ColVolume
	ColEffect
	ColEffectParam
)

// Model is the main TUI model
type Model struct {
	Song   *tracker.Song
	Player *audio.Player
	Audio  *audio.RealtimeOutput

	// View state
	Width       int
	Height      int
	Mode        EditMode
	ShowHelp    bool

	// Pattern editor state
	CursorRow   int
	CursorCh    int
	CursorCol   Column
	ViewRow     int  // Top visible row
	EditPos     int  // Current position in order list
	Octave      int  // Current input octave

	// Editor cursors for other modes
	OrderCursor int  // Selected position in order editor
	InstCursor  int  // Selected instrument
	OrnCursor   int  // Selected ornament

	// Playback display
	PlayPos     int
	PlayPat     int
	PlayRow     int
	Playing     bool

	// Status message
	StatusMsg   string

	// File info
	Filename    string
}

// NewModel creates a new TUI model
func NewModel(song *tracker.Song, filename string) Model {
	player := audio.NewPlayer(song)

	// Initialize real-time audio
	rtAudio, err := audio.NewRealtimeOutput(player)
	if err != nil {
		// Audio init failed - continue without sound
		rtAudio = nil
	}

	return Model{
		Song:     song,
		Player:   player,
		Audio:    rtAudio,
		Filename: filename,
		Octave:   4,
		Width:    120,
		Height:   30,
	}
}

// Init implements tea.Model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tickCmd(),
	)
}

// tickMsg is sent periodically for playback updates
type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(16_666_666, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

// Update implements tea.Model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tickMsg:
		// If no real-time audio, use simulation
		if m.Audio == nil {
			m.Player.AdvanceTime()
		}

		// Update playback position
		pos, pat, row, _, playing := m.Player.GetPlaybackInfo()
		m.PlayPos = pos
		m.PlayPat = pat
		m.PlayRow = row
		m.Playing = playing

		// Follow playback - move cursor and switch patterns
		if playing {
			m.EditPos = pos
			m.CursorRow = row
			m.ensureRowVisible()
		}
		return m, tickCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}


func (m *Model) ensureRowVisible() {
	visibleRows := m.Height - 12
	if visibleRows < 8 {
		visibleRows = 8
	}

	if m.CursorRow < m.ViewRow {
		m.ViewRow = m.CursorRow
	}
	if m.CursorRow >= m.ViewRow+visibleRows {
		m.ViewRow = m.CursorRow - visibleRows + 1
	}
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.Player.Stop()
		if m.Audio != nil {
			m.Audio.Close()
		}
		return m, tea.Quit

	case "f1", "h":
		m.ShowHelp = !m.ShowHelp

	case "f2":
		if m.Mode == ModeOrder {
			m.Mode = ModePattern
		} else {
			m.Mode = ModeOrder
		}

	case "f3":
		if m.Mode == ModeInstrument {
			m.Mode = ModePattern
		} else {
			m.Mode = ModeInstrument
		}

	case "f4":
		if m.Mode == ModeOrnament {
			m.Mode = ModePattern
		} else {
			m.Mode = ModeOrnament
		}

	// Playback controls
	case " ":
		if m.Playing {
			m.Player.Stop()
		} else {
			m.Player.SetPosition(m.EditPos, m.CursorRow)
			m.Player.Play()
		}

	case "f5":
		// Play from current position
		m.Player.SetPosition(m.EditPos, m.CursorRow)
		m.Player.Play()

	case "f8":
		m.Player.Stop()

	case "f9":
		// Export to WAV
		m.exportWAV()

	// Navigation
	case "up":
		if m.CursorRow > 0 {
			m.CursorRow--
			m.ensureRowVisible()
		}

	case "down":
		pat := m.currentPattern()
		if pat != nil && m.CursorRow < pat.Rows-1 {
			m.CursorRow++
			m.ensureRowVisible()
		}

	case "left":
		if m.CursorCol > ColNote {
			m.CursorCol--
		} else if m.CursorCh > 0 {
			m.CursorCh--
			m.CursorCol = ColEffectParam
		}

	case "right":
		if m.CursorCol < ColEffectParam {
			m.CursorCol++
		} else if m.CursorCh < m.Song.Channels-1 {
			m.CursorCh++
			m.CursorCol = ColNote
		}

	case "tab":
		m.CursorCh = (m.CursorCh + 1) % m.Song.Channels
		m.CursorCol = ColNote

	case "shift+tab":
		m.CursorCh--
		if m.CursorCh < 0 {
			m.CursorCh = m.Song.Channels - 1
		}
		m.CursorCol = ColNote

	case "pgup":
		m.CursorRow -= 16
		if m.CursorRow < 0 {
			m.CursorRow = 0
		}
		m.ensureRowVisible()

	case "pgdown":
		pat := m.currentPattern()
		if pat != nil {
			m.CursorRow += 16
			if m.CursorRow >= pat.Rows {
				m.CursorRow = pat.Rows - 1
			}
			m.ensureRowVisible()
		}

	case "home":
		m.CursorRow = 0
		m.ensureRowVisible()

	case "end":
		pat := m.currentPattern()
		if pat != nil {
			m.CursorRow = pat.Rows - 1
			m.ensureRowVisible()
		}

	// Order position navigation
	case "+", "=":
		if m.EditPos < len(m.Song.Order)-1 {
			m.EditPos++
			m.CursorRow = 0
			m.ViewRow = 0
		}

	case "-", "_":
		if m.EditPos > 0 {
			m.EditPos--
			m.CursorRow = 0
			m.ViewRow = 0
		}

	// Octave
	case "*":
		if m.Octave < 8 {
			m.Octave++
		}

	case "/":
		if m.Octave > 0 {
			m.Octave--
		}

	default:
		// Mode-specific handling
		switch m.Mode {
		case ModeOrder:
			return m.handleOrderKey(msg)
		case ModeInstrument:
			return m.handleInstrumentKey(msg)
		case ModeOrnament:
			return m.handleOrnamentKey(msg)
		default:
			// Pattern mode
			return m.handlePatternKey(msg)
		}
	}

	return m, nil
}

func (m Model) handlePatternKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "delete", "backspace":
		m.clearCell()
	case ".":
		m.noteOff()
	default:
		if note := keyToNote(msg.String(), m.Octave); note >= 0 {
			m.enterNote(note)
		}
	}
	return m, nil
}

func (m Model) handleOrderKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.OrderCursor > 0 {
			m.OrderCursor--
		}
	case "down":
		if m.OrderCursor < len(m.Song.Order)-1 {
			m.OrderCursor++
		}
	case "+", "=":
		// Add pattern after current position
		newOrder := make([]uint8, len(m.Song.Order)+1)
		copy(newOrder[:m.OrderCursor+1], m.Song.Order[:m.OrderCursor+1])
		newOrder[m.OrderCursor+1] = m.Song.Order[m.OrderCursor]
		copy(newOrder[m.OrderCursor+2:], m.Song.Order[m.OrderCursor+1:])
		m.Song.Order = newOrder
		m.OrderCursor++
	case "-", "_":
		// Remove current position (keep at least 1)
		if len(m.Song.Order) > 1 {
			newOrder := make([]uint8, len(m.Song.Order)-1)
			copy(newOrder[:m.OrderCursor], m.Song.Order[:m.OrderCursor])
			copy(newOrder[m.OrderCursor:], m.Song.Order[m.OrderCursor+1:])
			m.Song.Order = newOrder
			if m.OrderCursor >= len(m.Song.Order) {
				m.OrderCursor = len(m.Song.Order) - 1
			}
		}
	case "enter":
		// Go to this position in pattern mode
		m.EditPos = m.OrderCursor
		m.CursorRow = 0
		m.ViewRow = 0
		m.Mode = ModePattern
	case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Set pattern number
		digit := uint8(msg.String()[0] - '0')
		current := m.Song.Order[m.OrderCursor]
		newVal := (current%10)*10 + digit
		if int(newVal) < len(m.Song.Patterns) {
			m.Song.Order[m.OrderCursor] = newVal
		}
	case "n":
		// Create new pattern and assign it
		newPat := tracker.NewPattern(64, m.Song.Channels)
		m.Song.Patterns = append(m.Song.Patterns, newPat)
		m.Song.Order[m.OrderCursor] = uint8(len(m.Song.Patterns) - 1)
	}
	return m, nil
}

func (m Model) handleInstrumentKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.InstCursor > 0 {
			m.InstCursor--
		}
	case "down":
		if m.InstCursor < len(m.Song.Instruments)-1 {
			m.InstCursor++
		}
	case "g":
		// Cycle generator
		inst := &m.Song.Instruments[m.InstCursor]
		inst.Generator = (inst.Generator + 1) % 5
	case "+", "=":
		// Increase volume
		inst := &m.Song.Instruments[m.InstCursor]
		if inst.Volume < 64 {
			inst.Volume++
		}
	case "-", "_":
		// Decrease volume
		inst := &m.Song.Instruments[m.InstCursor]
		if inst.Volume > 0 {
			inst.Volume--
		}
	}
	return m, nil
}

func (m Model) handleOrnamentKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.OrnCursor > 0 {
			m.OrnCursor--
		}
	case "down":
		if m.OrnCursor < len(m.Song.Ornaments)-1 {
			m.OrnCursor++
		}
	case "+", "=":
		// Add value to ornament
		orn := &m.Song.Ornaments[m.OrnCursor]
		orn.Values = append(orn.Values, 0)
	case "-", "_":
		// Remove last value
		orn := &m.Song.Ornaments[m.OrnCursor]
		if len(orn.Values) > 1 {
			orn.Values = orn.Values[:len(orn.Values)-1]
		}
	}
	return m, nil
}

func (m *Model) currentPattern() *tracker.Pattern {
	patIdx := 0
	if m.EditPos < len(m.Song.Order) {
		patIdx = int(m.Song.Order[m.EditPos])
	}
	if patIdx < len(m.Song.Patterns) {
		return m.Song.Patterns[patIdx]
	}
	return nil
}

func (m *Model) currentPatternNum() int {
	if m.EditPos < len(m.Song.Order) {
		return int(m.Song.Order[m.EditPos])
	}
	return 0
}

func (m *Model) enterNote(note int8) {
	pat := m.currentPattern()
	if pat == nil {
		return
	}

	if m.CursorRow < pat.Rows && m.CursorCh < pat.Channels {
		pat.Notes[m.CursorRow][m.CursorCh].Pitch = note
		if pat.Notes[m.CursorRow][m.CursorCh].Instrument == 0 {
			pat.Notes[m.CursorRow][m.CursorCh].Instrument = 1 // Default instrument
		}
		// Move down
		if m.CursorRow < pat.Rows-1 {
			m.CursorRow++
			m.ensureRowVisible()
		}
	}
}

func (m *Model) noteOff() {
	pat := m.currentPattern()
	if pat == nil {
		return
	}

	if m.CursorRow < pat.Rows && m.CursorCh < pat.Channels {
		pat.Notes[m.CursorRow][m.CursorCh].Pitch = -2 // Note off
		if m.CursorRow < pat.Rows-1 {
			m.CursorRow++
			m.ensureRowVisible()
		}
	}
}

func (m *Model) clearCell() {
	pat := m.currentPattern()
	if pat == nil {
		return
	}

	if m.CursorRow < pat.Rows && m.CursorCh < pat.Channels {
		switch m.CursorCol {
		case ColNote:
			pat.Notes[m.CursorRow][m.CursorCh].Pitch = -1
			pat.Notes[m.CursorRow][m.CursorCh].Instrument = 0
		case ColInstrument:
			pat.Notes[m.CursorRow][m.CursorCh].Instrument = 0
		case ColVolume:
			pat.Notes[m.CursorRow][m.CursorCh].Volume = -1
		case ColEffect, ColEffectParam:
			pat.Notes[m.CursorRow][m.CursorCh].Effect = tracker.Effect{}
		}
	}
}

func (m *Model) exportWAV() {
	// Determine output filename based on source file
	baseName := "output"
	if m.Filename != "" {
		baseName = strings.TrimSuffix(filepath.Base(m.Filename), filepath.Ext(m.Filename))
	} else if m.Song.Title != "" {
		baseName = m.Song.Title
	}

	// Create _export directory if needed
	exportDir := "_export"
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		m.StatusMsg = "Export failed: " + err.Error()
		return
	}

	outputPath := filepath.Join(exportDir, baseName+".wav")

	// Calculate duration based on song length
	// Each row = speed ticks, each tick = tempo-based duration
	totalRows := 0
	for _, patIdx := range m.Song.Order {
		if int(patIdx) < len(m.Song.Patterns) {
			totalRows += m.Song.Patterns[patIdx].Rows
		}
	}
	ticksPerSecond := float64(m.Song.Tempo) * 2.0 / 5.0
	secondsPerRow := float64(m.Song.Speed) / ticksPerSecond
	duration := float64(totalRows) * secondsPerRow
	if duration < 1 {
		duration = 10 // Default 10 seconds
	}

	m.StatusMsg = fmt.Sprintf("Exporting %.1fs to %s...", duration, outputPath)

	// Create file
	f, err := os.Create(outputPath)
	if err != nil {
		m.StatusMsg = "Export failed: " + err.Error()
		return
	}
	defer f.Close()

	// Export
	err = audio.ExportWAV(m.Player, f, duration)
	if err != nil {
		m.StatusMsg = "Export failed: " + err.Error()
		return
	}

	m.StatusMsg = fmt.Sprintf("Exported to %s (%.1fs)", outputPath, duration)
}

// keyToNote converts keyboard key to MIDI note
func keyToNote(key string, octave int) int8 {
	// Piano-style keyboard layout:
	// Lower row: Z S X D C V G B H N J M (white + black keys)
	// Upper row: Q 2 W 3 E R 5 T 6 Y 7 U
	notes := map[string]int{
		// Lower octave
		"z": 0, "s": 1, "x": 2, "d": 3, "c": 4, "v": 5,
		"g": 6, "b": 7, "h": 8, "n": 9, "j": 10, "m": 11,
		// Upper octave
		"q": 12, "2": 13, "w": 14, "3": 15, "e": 16, "r": 17,
		"5": 18, "t": 19, "6": 20, "y": 21, "7": 22, "u": 23,
		"i": 24, "9": 25, "o": 26, "0": 27, "p": 28,
	}

	if n, ok := notes[key]; ok {
		return int8(octave*12 + n)
	}
	return -1
}

// View implements tea.Model
func (m Model) View() string {
	if m.ShowHelp {
		return m.helpView()
	}

	var b strings.Builder

	// Header
	b.WriteString(m.headerView())
	b.WriteString("\n")

	switch m.Mode {
	case ModeOrder:
		b.WriteString(m.orderView())
	case ModeInstrument:
		b.WriteString(m.instrumentView())
	case ModeOrnament:
		b.WriteString(m.ornamentView())
	default:
		// Pattern mode
		b.WriteString(m.channelHeaderView())
		b.WriteString("\n")
		b.WriteString(m.patternView())
	}

	// Footer
	b.WriteString(m.footerView())

	return b.String()
}

func (m Model) headerView() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")).
		Render("ABYTETRACKER")

	status := "STOPPED"
	if m.Playing {
		status = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Render(fmt.Sprintf("PLAYING %02d:%02d", m.PlayPos, m.PlayRow))
	}

	info := fmt.Sprintf(" │ Pos:%02d/%02d Pat:%02d Row:%02d │ Spd:%d BPM:%d │ Oct:%d │ %s",
		m.EditPos, len(m.Song.Order), m.currentPatternNum(), m.CursorRow,
		m.Song.Speed, m.Song.Tempo, m.Octave, status)

	return title + info
}

func (m Model) channelHeaderView() string {
	var parts []string
	parts = append(parts, "   │")

	for ch := 0; ch < m.Song.Channels; ch++ {
		name := fmt.Sprintf("CH%d", ch+1)
		if ch < len(m.Song.ChanConfig) {
			name = m.Song.ChanConfig[ch].Name
		}

		gen := "tri"
		if ch < len(m.Song.ChanConfig) {
			switch m.Song.ChanConfig[ch].Generator {
			case tracker.GenSawtooth:
				gen = "saw"
			case tracker.GenSquare:
				gen = "squ"
			case tracker.GenNoise:
				gen = "noi"
			}
		}

		style := lipgloss.NewStyle()
		if ch == m.CursorCh {
			style = style.Foreground(lipgloss.Color("11")).Bold(true)
		} else {
			style = style.Foreground(lipgloss.Color("8"))
		}

		header := fmt.Sprintf(" %-6s:%s│", name, gen)
		parts = append(parts, style.Render(header))
	}

	return strings.Join(parts, "")
}

func (m Model) patternView() string {
	pat := m.currentPattern()
	if pat == nil {
		return "No pattern"
	}

	visibleRows := m.Height - 10
	if visibleRows < 8 {
		visibleRows = 8
	}

	var lines []string

	for row := m.ViewRow; row < m.ViewRow+visibleRows && row < pat.Rows; row++ {
		line := m.renderRow(pat, row)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderRow(pat *tracker.Pattern, row int) string {
	// Row number
	rowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if row%4 == 0 {
		rowStyle = rowStyle.Foreground(lipgloss.Color("14"))
	}
	if row == m.PlayRow && m.Playing {
		rowStyle = rowStyle.Background(lipgloss.Color("4"))
	}

	cursor := " "
	if row == m.CursorRow {
		cursor = ">"
	}

	line := fmt.Sprintf("%s%s│", cursor, rowStyle.Render(fmt.Sprintf("%02X", row)))

	for ch := 0; ch < pat.Channels; ch++ {
		note := pat.Notes[row][ch]
		cell := m.renderCell(note, row, ch)
		line += cell + "│"
	}

	return line
}

func (m Model) renderCell(note tracker.Note, row, ch int) string {
	isCursor := row == m.CursorRow && ch == m.CursorCh
	isEmpty := note.Pitch == -1 && note.Instrument == 0 && note.Effect.Type == 0 && note.Effect.Param == 0

	// Note
	noteStr := tracker.NoteToString(note.Pitch)
	noteStyle := lipgloss.NewStyle()
	if note.Pitch >= 0 {
		noteStyle = noteStyle.Foreground(lipgloss.Color("15"))
	} else if note.Pitch == -2 {
		noteStyle = noteStyle.Foreground(lipgloss.Color("9"))
	} else {
		noteStyle = noteStyle.Foreground(lipgloss.Color("8"))
	}
	if isCursor && m.CursorCol == ColNote {
		noteStyle = noteStyle.Background(lipgloss.Color("6"))
	}

	// Instrument - only show if note is present or instrument explicitly set
	instStr := "--"
	instStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if note.Instrument > 0 && (note.Pitch >= 0 || note.Pitch == -2 || !isEmpty) {
		instStr = fmt.Sprintf("%02X", note.Instrument)
		instStyle = instStyle.Foreground(lipgloss.Color("11"))
	}
	if isCursor && m.CursorCol == ColInstrument {
		instStyle = instStyle.Background(lipgloss.Color("6"))
	}

	// Effect
	fxStr := "..."
	fxStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if note.Effect.Type != 0 || note.Effect.Param != 0 {
		fxStr = fmt.Sprintf("%X%02X", note.Effect.Type, note.Effect.Param)
		fxStyle = fxStyle.Foreground(lipgloss.Color("13"))
	}
	if isCursor && (m.CursorCol == ColEffect || m.CursorCol == ColEffectParam) {
		fxStyle = fxStyle.Background(lipgloss.Color("6"))
	}

	return " " + noteStyle.Render(noteStr) + " " + instStyle.Render(instStr) + " " + fxStyle.Render(fxStr)
}

func (m Model) orderView() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render("ORDER LIST")
	b.WriteString(title + " (F2 to exit)\n\n")

	for i, patIdx := range m.Song.Order {
		cursor := "  "
		if i == m.OrderCursor {
			cursor = "> "
		}
		playing := "  "
		if i == m.PlayPos && m.Playing {
			playing = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("▶ ")
		}

		style := lipgloss.NewStyle()
		if i == m.OrderCursor {
			style = style.Bold(true).Foreground(lipgloss.Color("14"))
		}
		if i == m.EditPos {
			style = style.Background(lipgloss.Color("8"))
		}

		line := fmt.Sprintf("%s%s%02d: Pattern %02d", cursor, playing, i, patIdx)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n ↑↓ Navigate  +/= Add  - Remove  0-9 Set pattern  Enter Go to pattern\n")
	return b.String()
}

func (m Model) instrumentView() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render("INSTRUMENTS")
	b.WriteString(title + " (F3 to exit)\n\n")

	genNames := map[tracker.Generator]string{
		tracker.GenTriangle: "tri", tracker.GenSawtooth: "saw",
		tracker.GenSquare: "squ", tracker.GenSawBig: "swb", tracker.GenNoise: "noi",
	}

	for i, inst := range m.Song.Instruments {
		cursor := "  "
		if i == m.InstCursor {
			cursor = "> "
		}
		style := lipgloss.NewStyle()
		if i == m.InstCursor {
			style = style.Bold(true).Foreground(lipgloss.Color("14"))
		}

		gen := genNames[inst.Generator]
		env := fmt.Sprintf("A%02d D%02d S%02d R%02d", inst.Envelope.Attack, inst.Envelope.Decay, inst.Envelope.Sustain, inst.Envelope.Release)
		duty := ""
		if inst.Generator == tracker.GenSquare && inst.Duty > 0 {
			duty = fmt.Sprintf(" D%02X", inst.Duty)
		}
		line := fmt.Sprintf("%s%02d: %-8s %s Vol:%02d %s%s", cursor, i+1, inst.Name, gen, inst.Volume, env, duty)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n ↑↓ Select  ←→ Field  0-9 Edit  G Osc  Enter Edit name\n")
	return b.String()
}

func (m Model) ornamentView() string {
	var b strings.Builder
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render("ORNAMENTS")
	b.WriteString(title + " (F4 to exit)\n\n")

	for i, orn := range m.Song.Ornaments {
		cursor := "  "
		if i == m.OrnCursor {
			cursor = "> "
		}
		style := lipgloss.NewStyle()
		if i == m.OrnCursor {
			style = style.Bold(true).Foreground(lipgloss.Color("14"))
		}

		values := ""
		for j, v := range orn.Values {
			if j > 0 {
				values += " "
			}
			if v >= 0 {
				values += fmt.Sprintf("+%d", v)
			} else {
				values += fmt.Sprintf("%d", v)
			}
		}
		loop := ""
		if orn.Loop >= 0 {
			loop = fmt.Sprintf(" L%d", orn.Loop)
		}
		line := fmt.Sprintf("%s%02d: %-8s [%s]%s", cursor, i+1, orn.Name, values, loop)
		b.WriteString(style.Render(line) + "\n")
	}

	b.WriteString("\n ↑↓ Select  Enter Edit  + Add value  - Remove value\n")
	return b.String()
}

func (m Model) footerView() string {
	var keys string
	switch m.Mode {
	case ModeOrder:
		keys = " [F2]Pattern [F3]Inst [F4]Orn [Space]Play [H]Help [Q]Quit"
	case ModeInstrument:
		keys = " [F2]Order [F3]Pattern [F4]Orn [Space]Play [H]Help [Q]Quit"
	case ModeOrnament:
		keys = " [F2]Order [F3]Inst [F4]Pattern [Space]Play [H]Help [Q]Quit"
	default:
		keys = " [F2]Order [F3]Inst [F4]Orn [Space]Play [F9]Export [+/-]Pos [H]Help [Q]Quit"
	}
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(keys)
	if m.StatusMsg != "" {
		status := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("\n " + m.StatusMsg)
		footer += status
	}
	return footer
}

func (m Model) helpView() string {
	help := `
╔══════════════════════════════════════════════════════════════════╗
║                    ABYTETRACKER HELP                             ║
╠══════════════════════════════════════════════════════════════════╣
║ NAVIGATION                                                       ║
║   ↑↓←→      Move cursor          Tab       Next channel          ║
║   PgUp/Dn   Move 16 rows         +/-       Next/prev pattern     ║
║   Home/End  First/last row       */        Octave up/down        ║
║                                                                  ║
║ NOTE INPUT (piano keyboard)                                      ║
║   Z S X D C V G B H N J M  - Lower octave (C to B)              ║
║   Q 2 W 3 E R 5 T 6 Y 7 U  - Upper octave                       ║
║   .         Note off             Del       Clear cell            ║
║                                                                  ║
║ PLAYBACK & EXPORT                                                ║
║   Space     Play/Stop            F9        Export WAV            ║
║   F5        Play from row        F8        Stop                  ║
║                                                                  ║
║ OSCILLATORS (set in instrument, shown in channel header)         ║
║   tri  Triangle wave       saw  Sawtooth wave                    ║
║   squ  Square/pulse wave   swb  SawBig (11-bit bytebeat)        ║
║   noi  Noise               (use Kxx effect for duty cycle)       ║
║                                                                  ║
║ EFFECTS (in effect column: Txx where T=type, xx=param)           ║
║   0xy  Arpeggio            Cxx  Set volume (00-40)               ║
║   1xx  Slide up            Fxx  Set speed (<20) or tempo (≥20)   ║
║   2xx  Slide down          Gxx  Set ornament                     ║
║   4xy  Vibrato             Kxx  Set duty cycle (00-FF, 80=50%)   ║
║   Exy  Echo ch x delay y                                         ║
║                                                                  ║
║                              [H/F1] Close help                   ║
╚══════════════════════════════════════════════════════════════════╝
`
	return lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(help)
}
