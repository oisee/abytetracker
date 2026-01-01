package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anthropics/abytetracker/pkg/format"
	"github.com/anthropics/abytetracker/pkg/tracker"
	"github.com/anthropics/abytetracker/pkg/tui"
)

func main() {
	channels := flag.Int("channels", 6, "Number of channels (1-16)")
	flag.Parse()

	var song *tracker.Song
	var err error
	var filename string

	// Check if a file was provided
	if flag.NArg() > 0 {
		filename = flag.Arg(0)
		f, err := os.Open(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening file: %v\n", err)
			os.Exit(1)
		}
		song, err = format.Load(f)
		f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Loaded: %s by %s (%d channels)\n", song.Title, song.Author, song.Channels)
	} else {
		// Create a new song
		if *channels < 1 {
			*channels = 1
		}
		if *channels > 16 {
			*channels = 16
		}
		song = tracker.NewSong(*channels)
		song.Title = "New Song"
		// Add demo pattern for new songs
		pat := song.Patterns[0]
		pat.Notes[0][0] = tracker.Note{Pitch: 48, Instrument: 1}  // C-4
		pat.Notes[4][0] = tracker.Note{Pitch: 52, Instrument: 1}  // E-4
		pat.Notes[8][0] = tracker.Note{Pitch: 55, Instrument: 1}  // G-4
		pat.Notes[12][0] = tracker.Note{Pitch: 60, Instrument: 1} // C-5
		if song.Channels > 1 {
			pat.Notes[0][1] = tracker.Note{Pitch: 36, Instrument: 2}
			pat.Notes[8][1] = tracker.Note{Pitch: 43, Instrument: 2}
		}
		if song.Channels > 4 {
			for row := 0; row < 64; row += 16 {
				pat.Notes[row][4] = tracker.Note{Pitch: 36, Instrument: 4}
			}
		}
	}
	_ = err

	// Start TUI
	model := tui.NewModel(song, filename)
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
