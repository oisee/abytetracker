// Package tui implements the terminal user interface
package tui

import (
	"fmt"
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
	Octave      int  // Current input octave

	// Playback display
	PlayPos     int
	PlayPat     int
	PlayRow     int
	Playing     bool

	// Status message
	StatusMsg   string
}

// NewModel creates a new TUI model
func NewModel(song *tracker.Song) Model {
	player := audio.NewPlayer(song)
	return Model{
		Song:   song,
		Player: player,
		Octave: 4,
		Width:  120,
		Height: 30,
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
		// Update playback position
		pos, pat, row, _, playing := m.Player.GetPlaybackInfo()
		m.PlayPos = pos
		m.PlayPat = pat
		m.PlayRow = row
		m.Playing = playing

		// Follow playback
		if playing && pat == m.currentPatternIndex() {
			m.CursorRow = row
			m.ensureRowVisible()
		}
		return m, tickCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) currentPatternIndex() int {
	if m.PlayPos < len(m.Song.Order) {
		return int(m.Song.Order[m.PlayPos])
	}
	return 0
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
		return m, tea.Quit

	case "f1":
		m.ShowHelp = !m.ShowHelp

	// Playback controls
	case " ":
		if m.Playing {
			m.Player.Stop()
		} else {
			m.Player.SetPosition(0, m.CursorRow)
			m.Player.Play()
		}

	case "f5":
		// Play from current position
		m.Player.SetPosition(0, m.CursorRow)
		m.Player.Play()

	case "f8":
		m.Player.Stop()

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

	// Octave
	case "*":
		if m.Octave < 8 {
			m.Octave++
		}

	case "/":
		if m.Octave > 0 {
			m.Octave--
		}

	// Note input
	case "delete", "backspace":
		m.clearCell()

	case ".":
		m.noteOff()

	default:
		// Check for note input
		if note := keyToNote(msg.String(), m.Octave); note >= 0 {
			m.enterNote(note)
		}
	}

	return m, nil
}

func (m *Model) currentPattern() *tracker.Pattern {
	patIdx := 0
	if len(m.Song.Order) > 0 {
		patIdx = int(m.Song.Order[0]) // TODO: use current position
	}
	if patIdx < len(m.Song.Patterns) {
		return m.Song.Patterns[patIdx]
	}
	return nil
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

	// Channel headers
	b.WriteString(m.channelHeaderView())
	b.WriteString("\n")

	// Pattern grid
	b.WriteString(m.patternView())

	// Footer
	b.WriteString(m.footerView())

	return b.String()
}

func (m Model) headerView() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("14")).
		Render("ABYTETRACKER")

	playing := "STOPPED"
	if m.Playing {
		playing = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Render("PLAYING")
	}

	info := fmt.Sprintf(" │ Pos:%02d/%02d Pat:%02d Row:%02d │ Spd:%d BPM:%d │ Oct:%d │ %s",
		m.PlayPos, len(m.Song.Order), m.PlayPat, m.PlayRow,
		m.Song.Speed, m.Song.Tempo, m.Octave, playing)

	return title + info
}

func (m Model) channelHeaderView() string {
	var parts []string
	parts = append(parts, "    │")

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

		header := fmt.Sprintf(" %-6s:%s │", name, gen)
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

	// Instrument
	instStr := "--"
	if note.Instrument > 0 {
		instStr = fmt.Sprintf("%02X", note.Instrument)
	}
	instStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	if note.Instrument == 0 {
		instStyle = instStyle.Foreground(lipgloss.Color("8"))
	}
	if isCursor && m.CursorCol == ColInstrument {
		instStyle = instStyle.Background(lipgloss.Color("6"))
	}

	// Effect (simplified)
	fxStr := "..."
	if note.Effect.Type != 0 || note.Effect.Param != 0 {
		fxStr = fmt.Sprintf("%X%02X", note.Effect.Type, note.Effect.Param)
	}
	fxStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	if note.Effect.Type == 0 && note.Effect.Param == 0 {
		fxStyle = fxStyle.Foreground(lipgloss.Color("8"))
	}
	if isCursor && (m.CursorCol == ColEffect || m.CursorCol == ColEffectParam) {
		fxStyle = fxStyle.Background(lipgloss.Color("6"))
	}

	return " " + noteStyle.Render(noteStr) + " " + instStyle.Render(instStr) + " " + fxStyle.Render(fxStr)
}

func (m Model) footerView() string {
	keys := " [Space]Play [F5]From Row [F8]Stop [Tab]Ch [*/]Oct [F1]Help [Q]Quit"
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(keys)
}

func (m Model) helpView() string {
	help := `
╔══════════════════════════════════════════════════════════════════╗
║                    ABYTETRACKER HELP                             ║
╠══════════════════════════════════════════════════════════════════╣
║ NAVIGATION                                                       ║
║   ↑↓←→      Move cursor                                          ║
║   Tab       Next channel                                         ║
║   PgUp/Dn   Move 16 rows                                         ║
║   Home/End  First/last row                                       ║
║                                                                  ║
║ NOTE INPUT (piano keyboard)                                      ║
║   Z S X D C V G B H N J M  - Lower octave (C to B)              ║
║   Q 2 W 3 E R 5 T 6 Y 7 U  - Upper octave                       ║
║   * /       Octave up/down                                       ║
║   .         Note off                                             ║
║   Del       Clear cell                                           ║
║                                                                  ║
║ PLAYBACK                                                         ║
║   Space     Play/Stop                                            ║
║   F5        Play from current row                                ║
║   F8        Stop                                                 ║
║                                                                  ║
║ EFFECTS (in effect column)                                       ║
║   0xy  Arpeggio         Axy  Volume slide                       ║
║   1xx  Slide up         Cxx  Set volume                         ║
║   2xx  Slide down       Fxx  Set speed/tempo                    ║
║   4xy  Vibrato          Gxx  Set ornament                       ║
║   Exy  Echo ch x, delay y                                       ║
║                                                                  ║
║                              [F1] Close help                     ║
╚══════════════════════════════════════════════════════════════════╝
`
	return lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(help)
}
