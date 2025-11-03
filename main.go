package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Tab represents a view tab
type Tab int

const (
	TabPlayback Tab = iota
	TabRecording
	TabOutputDevices
	TabInputDevices
)

var tabNames = []string{"Playback", "Recording", "Output Devices", "Input Devices"}

// Model represents the application state
type model struct {
	pulseClient    *PulseClient
	currentTab     Tab
	selectedIndex  int
	searchQuery    string
	searchMode     bool
	showAllDevices bool // Toggle to show unavailable devices
	err            error
	width          int
	height         int

	// Cached data
	sinks         []Device
	sources       []Device
	sinkInputs    []Stream
	sourceOutputs []Stream
	lastUpdate    time.Time
}

type tickMsg time.Time
type updateDataMsg struct{}
type updatePeaksMsg struct{}

func tickEvery() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func peakUpdateEvery() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(t time.Time) tea.Msg {
		return updatePeaksMsg{}
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tickEvery(),
		peakUpdateEvery(),
		func() tea.Msg { return updateDataMsg{} },
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(
			tickEvery(),
			func() tea.Msg { return updateDataMsg{} },
		)

	case updateDataMsg:
		// Update data from PulseAudio
		m.updateData()
		return m, nil

	case updatePeaksMsg:
		// Update peak levels for input devices
		m.updatePeaks()
		return m, peakUpdateEvery()

	case tea.KeyMsg:
		if m.searchMode {
			return m.handleSearchKey(msg)
		}
		return m.handleNormalKey(msg)
	}

	return m, nil
}

func (m *model) updateData() {
	var err error

	switch m.currentTab {
	case TabPlayback:
		m.sinkInputs, err = m.pulseClient.ListSinkInputs()
		if err != nil {
			m.err = err
		}
	case TabRecording:
		m.sourceOutputs, err = m.pulseClient.ListSourceOutputs()
		if err != nil {
			m.err = err
		}
	case TabOutputDevices:
		m.sinks, err = m.pulseClient.ListSinks()
		if err != nil {
			m.err = err
		}
	case TabInputDevices:
		m.sources, err = m.pulseClient.ListSources()
		if err != nil {
			m.err = err
		}
		// Start peak monitors for input devices
		m.startPeakMonitors()
	}

	m.lastUpdate = time.Now()
}

func (m *model) updatePeaks() {
	// Only update peaks when on Input Devices tab
	if m.currentTab != TabInputDevices {
		return
	}

	// Update peak levels for all sources
	for i := range m.sources {
		if m.sources[i].Available {
			m.sources[i].PeakLevel = m.pulseClient.GetPeakLevel(m.sources[i].Index)
		}
	}
}

func (m *model) startPeakMonitors() {
	// Only monitor input devices when on that tab
	if m.currentTab != TabInputDevices {
		return
	}

	for _, source := range m.sources {
		if source.Available {
			// Start monitoring (it will skip if already running)
			m.pulseClient.StartPeakMonitor(source.Index, source.Name)
		}
	}
}

func (m *model) stopPeakMonitors() {
	// Stop all peak monitors
	m.pulseClient.StopAllPeakMonitors()
}

func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searchMode = false
		m.searchQuery = ""
		return m, nil

	case tea.KeyEnter:
		m.searchMode = false
		return m, nil

	case tea.KeyBackspace:
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
		}
		return m, nil

	case tea.KeyRunes:
		m.searchQuery += string(msg.Runes)
		return m, nil
	}

	return m, nil
}

func (m model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.stopPeakMonitors()
		return m, tea.Quit

	case "tab", "right":
		// Stop peak monitors when leaving Input Devices tab
		if m.currentTab == TabInputDevices {
			m.stopPeakMonitors()
		}
		m.currentTab = (m.currentTab + 1) % 4
		m.selectedIndex = 0
		m.searchQuery = ""
		return m, func() tea.Msg { return updateDataMsg{} }

	case "shift+tab", "left":
		// Stop peak monitors when leaving Input Devices tab
		if m.currentTab == TabInputDevices {
			m.stopPeakMonitors()
		}
		m.currentTab = (m.currentTab + 3) % 4
		m.selectedIndex = 0
		m.searchQuery = ""
		return m, func() tea.Msg { return updateDataMsg{} }

	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
		}
		return m, nil

	case "down", "j":
		maxIndex := m.getMaxIndex()
		if m.selectedIndex < maxIndex-1 {
			m.selectedIndex++
		}
		return m, nil

	case "/":
		m.searchMode = true
		return m, nil

	case "a":
		m.showAllDevices = !m.showAllDevices
		m.selectedIndex = 0 // Reset selection when toggling
		return m, nil

	case "=", "+":
		m.adjustVolume(5)
		return m, func() tea.Msg { return updateDataMsg{} }

	case "-", "_":
		m.adjustVolume(-5)
		return m, func() tea.Msg { return updateDataMsg{} }

	case "m":
		m.toggleMute()
		return m, func() tea.Msg { return updateDataMsg{} }

	case "d", "enter":
		m.setAsDefault()
		return m, func() tea.Msg { return updateDataMsg{} }

	case "r":
		return m, func() tea.Msg { return updateDataMsg{} }
	}

	return m, nil
}

func (m *model) getMaxIndex() int {
	items := m.getFilteredItems()

	switch items := items.(type) {
	case []Stream:
		return len(items)
	case []Device:
		return len(items)
	default:
		return 0
	}
}

func (m *model) getFilteredItems() interface{} {
	query := strings.ToLower(m.searchQuery)

	switch m.currentTab {
	case TabPlayback:
		if query == "" {
			return m.sinkInputs
		}
		filtered := []Stream{}
		for _, s := range m.sinkInputs {
			if strings.Contains(strings.ToLower(s.Name), query) ||
				strings.Contains(strings.ToLower(s.AppName), query) {
				filtered = append(filtered, s)
			}
		}
		return filtered

	case TabRecording:
		if query == "" {
			return m.sourceOutputs
		}
		filtered := []Stream{}
		for _, s := range m.sourceOutputs {
			if strings.Contains(strings.ToLower(s.Name), query) ||
				strings.Contains(strings.ToLower(s.AppName), query) {
				filtered = append(filtered, s)
			}
		}
		return filtered

	case TabOutputDevices:
		filtered := []Device{}
		for _, d := range m.sinks {
			// Filter by availability unless showAllDevices is true
			if !m.showAllDevices && !d.Available {
				continue
			}
			// Filter by search query
			if query != "" {
				if !strings.Contains(strings.ToLower(d.Description), query) &&
					!strings.Contains(strings.ToLower(d.Name), query) {
					continue
				}
			}
			filtered = append(filtered, d)
		}
		return filtered

	case TabInputDevices:
		filtered := []Device{}
		for _, d := range m.sources {
			// Filter by availability unless showAllDevices is true
			if !m.showAllDevices && !d.Available {
				continue
			}
			// Filter by search query
			if query != "" {
				if !strings.Contains(strings.ToLower(d.Description), query) &&
					!strings.Contains(strings.ToLower(d.Name), query) {
					continue
				}
			}
			filtered = append(filtered, d)
		}
		return filtered
	}

	return nil
}

func (m *model) adjustVolume(delta int) {
	items := m.getFilteredItems()

	switch m.currentTab {
	case TabPlayback:
		streams := items.([]Stream)
		if m.selectedIndex < len(streams) {
			stream := streams[m.selectedIndex]
			newVolume := stream.Volume + delta
			if newVolume < 0 {
				newVolume = 0
			}
			if newVolume > 150 {
				newVolume = 150
			}
			m.pulseClient.SetSinkInputVolume(stream.Index, newVolume)
		}

	case TabRecording:
		streams := items.([]Stream)
		if m.selectedIndex < len(streams) {
			stream := streams[m.selectedIndex]
			newVolume := stream.Volume + delta
			if newVolume < 0 {
				newVolume = 0
			}
			if newVolume > 150 {
				newVolume = 150
			}
			m.pulseClient.SetSourceOutputVolume(stream.Index, newVolume)
		}

	case TabOutputDevices:
		devices := items.([]Device)
		if m.selectedIndex < len(devices) {
			device := devices[m.selectedIndex]
			newVolume := device.Volume + delta
			if newVolume < 0 {
				newVolume = 0
			}
			if newVolume > 150 {
				newVolume = 150
			}
			m.pulseClient.SetSinkVolume(device.Index, newVolume)
		}

	case TabInputDevices:
		devices := items.([]Device)
		if m.selectedIndex < len(devices) {
			device := devices[m.selectedIndex]
			newVolume := device.Volume + delta
			if newVolume < 0 {
				newVolume = 0
			}
			if newVolume > 150 {
				newVolume = 150
			}
			m.pulseClient.SetSourceVolume(device.Index, newVolume)
		}
	}
}

func (m *model) toggleMute() {
	items := m.getFilteredItems()

	switch m.currentTab {
	case TabOutputDevices:
		devices := items.([]Device)
		if m.selectedIndex < len(devices) {
			m.pulseClient.ToggleSinkMute(devices[m.selectedIndex].Index)
		}

	case TabInputDevices:
		devices := items.([]Device)
		if m.selectedIndex < len(devices) {
			m.pulseClient.ToggleSourceMute(devices[m.selectedIndex].Index)
		}
	}
}

func (m *model) setAsDefault() {
	items := m.getFilteredItems()

	switch m.currentTab {
	case TabOutputDevices:
		devices := items.([]Device)
		if m.selectedIndex < len(devices) {
			m.pulseClient.SetDefaultSink(devices[m.selectedIndex].Name)
		}

	case TabInputDevices:
		devices := items.([]Device)
		if m.selectedIndex < len(devices) {
			m.pulseClient.SetDefaultSource(devices[m.selectedIndex].Name)
		}
	}
}

func (m model) View() string {
	var b strings.Builder

	// Render tabs
	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")

	// Render content
	b.WriteString(m.renderContent())

	// Render search bar
	if m.searchMode {
		b.WriteString("\n\n")
		b.WriteString(m.renderSearchBar())
	}

	// Render status bar
	b.WriteString("\n\n")
	b.WriteString(m.renderStatusBar())

	// Render help
	b.WriteString("\n")
	b.WriteString(m.renderHelp())

	return b.String()
}

func (m model) renderTabs() string {
	var tabs []string

	activeTabStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("63")).
		Padding(0, 2)

	inactiveTabStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 2)

	for i, name := range tabNames {
		if Tab(i) == m.currentTab {
			tabs = append(tabs, activeTabStyle.Render(name))
		} else {
			tabs = append(tabs, inactiveTabStyle.Render(name))
		}
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
}

func (m model) renderContent() string {
	items := m.getFilteredItems()

	switch m.currentTab {
	case TabPlayback:
		return m.renderStreams(items.([]Stream))
	case TabRecording:
		return m.renderStreams(items.([]Stream))
	case TabOutputDevices:
		return m.renderDevices(items.([]Device))
	case TabInputDevices:
		return m.renderDevices(items.([]Device))
	}

	return ""
}

func (m model) renderDevices(devices []Device) string {
	if len(devices) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("No devices found")
	}

	var b strings.Builder

	for i, device := range devices {
		isSelected := i == m.selectedIndex

		// Build device line
		var line strings.Builder

		// Availability icon
		if !device.Available {
			line.WriteString("⊗ ")
		} else if device.IsDefault {
			line.WriteString("✓ ")
		} else {
			line.WriteString("● ")
		}

		// Description
		line.WriteString(device.Description)

		// Mute indicator
		if device.Muted {
			line.WriteString(" [MUTED]")
		}

		// Unavailable indicator
		if !device.Available {
			line.WriteString(" [UNAVAILABLE]")
		}

		// Volume bar
		line.WriteString("\n  ")
		line.WriteString(renderVolumeBar(device.Volume))

		// Add peak meter for input devices (sources)
		if m.currentTab == TabInputDevices && device.Available {
			line.WriteString("  ")
			line.WriteString(renderPeakMeter(device.PeakLevel))
		}

		// Apply styling
		style := lipgloss.NewStyle().Padding(0, 1)
		if !device.Available {
			// Grey out unavailable devices
			style = style.Foreground(lipgloss.Color("240"))
		}
		if isSelected {
			style = style.
				Background(lipgloss.Color("237")).
				Bold(true)
		}

		b.WriteString(style.Render(line.String()))
		b.WriteString("\n")
	}

	return b.String()
}

func (m model) renderStreams(streams []Stream) string {
	if len(streams) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("No streams found")
	}

	var b strings.Builder

	for i, stream := range streams {
		isSelected := i == m.selectedIndex

		// Build stream line
		var line strings.Builder

		// App name
		line.WriteString(stream.AppName)
		if stream.Name != "" && stream.Name != stream.AppName {
			line.WriteString(fmt.Sprintf(" - %s", stream.Name))
		}

		// Mute indicator
		if stream.Muted {
			line.WriteString(" [MUTED]")
		}

		// Volume bar
		line.WriteString("\n  ")
		line.WriteString(renderVolumeBar(stream.Volume))

		// Apply styling
		style := lipgloss.NewStyle().Padding(0, 1)
		if isSelected {
			style = style.
				Background(lipgloss.Color("237")).
				Bold(true)
		}

		b.WriteString(style.Render(line.String()))
		b.WriteString("\n")
	}

	return b.String()
}

func renderVolumeBar(volume int) string {
	barWidth := 30
	filled := int(float64(volume) / 150.0 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	// Color based on volume
	style := lipgloss.NewStyle()
	if volume > 100 {
		style = style.Foreground(lipgloss.Color("196")) // Red
	} else if volume > 80 {
		style = style.Foreground(lipgloss.Color("214")) // Orange
	} else {
		style = style.Foreground(lipgloss.Color("46")) // Green
	}

	return fmt.Sprintf("%s %3d%%", style.Render(bar), volume)
}

func renderPeakMeter(peakLevel float64) string {
	meterWidth := 10
	filled := int(peakLevel * float64(meterWidth))
	if filled > meterWidth {
		filled = meterWidth
	}
	if filled < 0 {
		filled = 0
	}

	meter := strings.Repeat("=", filled) + strings.Repeat(" ", meterWidth-filled)

	// Color based on peak level
	style := lipgloss.NewStyle()
	if peakLevel > 0.8 {
		style = style.Foreground(lipgloss.Color("196")) // Red - clipping warning
	} else if peakLevel > 0.5 {
		style = style.Foreground(lipgloss.Color("214")) // Orange - good level
	} else if peakLevel > 0.1 {
		style = style.Foreground(lipgloss.Color("46")) // Green - active
	} else {
		style = style.Foreground(lipgloss.Color("240")) // Gray - quiet
	}

	return style.Render(fmt.Sprintf("[%s]", meter))
}

func (m model) renderSearchBar() string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Render(fmt.Sprintf("Search: %s_", m.searchQuery))
}

func (m model) renderStatusBar() string {
	if m.err != nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Render(fmt.Sprintf("Error: %v", m.err))
	}

	status := fmt.Sprintf("Last update: %s", m.lastUpdate.Format("15:04:05"))

	// Add indicator for "Show All Devices" mode
	if m.showAllDevices && (m.currentTab == TabOutputDevices || m.currentTab == TabInputDevices) {
		status += "  [Showing All Devices]"
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(status)
}

func (m model) renderHelp() string {
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	help := "↑/↓: Navigate  Tab: Switch  /: Search  +/-: Volume  m: Mute"

	if m.currentTab == TabOutputDevices || m.currentTab == TabInputDevices {
		help += "  d/Enter: Set Default  a: Toggle All"
	}

	help += "  r: Refresh  q: Quit"

	return helpStyle.Render(help)
}

func main() {
	// Create PulseAudio client
	pulseClient, err := NewPulseClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer pulseClient.Close()

	// Create initial model
	m := model{
		pulseClient: pulseClient,
		currentTab:  TabOutputDevices,
	}

	// Run the program
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
