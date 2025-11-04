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
	TabConfiguration
)

var tabNames = []string{"Playback", "Recording", "Output Devices", "Input Devices", "Configuration"}

// Model represents the application state
type model struct {
	pulseClient    *PulseClient
	currentTab     Tab
	selectedIndex  int
	scrollOffset   int  // Vertical scroll position for viewport
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
	cards         []Card
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
	case TabConfiguration:
		m.cards, err = m.pulseClient.ListCards()
		if err != nil {
			m.err = err
		}
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
		m.currentTab = (m.currentTab + 1) % 5
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.searchQuery = ""
		return m, func() tea.Msg { return updateDataMsg{} }

	case "shift+tab", "left":
		// Stop peak monitors when leaving Input Devices tab
		if m.currentTab == TabInputDevices {
			m.stopPeakMonitors()
		}
		m.currentTab = (m.currentTab + 4) % 5
		m.selectedIndex = 0
		m.scrollOffset = 0
		m.searchQuery = ""
		return m, func() tea.Msg { return updateDataMsg{} }

	case "up", "k":
		if m.selectedIndex > 0 {
			m.selectedIndex--
			m.adjustScroll()
		}
		return m, nil

	case "down", "j":
		maxIndex := m.getMaxIndex()
		if m.selectedIndex < maxIndex-1 {
			m.selectedIndex++
			m.adjustScroll()
		}
		return m, nil

	case "pgdown":
		// Jump down by 10 items or to end
		maxIndex := m.getMaxIndex()
		m.selectedIndex += 10
		if m.selectedIndex >= maxIndex {
			m.selectedIndex = maxIndex - 1
		}
		if m.selectedIndex < 0 {
			m.selectedIndex = 0
		}
		m.adjustScroll()
		return m, nil

	case "pgup":
		// Jump up by 10 items or to start
		m.selectedIndex -= 10
		if m.selectedIndex < 0 {
			m.selectedIndex = 0
		}
		m.adjustScroll()
		return m, nil

	case "home", "g":
		// Jump to first item
		m.selectedIndex = 0
		m.adjustScroll()
		return m, nil

	case "end", "G":
		// Jump to last item
		maxIndex := m.getMaxIndex()
		if maxIndex > 0 {
			m.selectedIndex = maxIndex - 1
		}
		m.adjustScroll()
		return m, nil

	case "/":
		m.searchMode = true
		return m, nil

	case "a":
		m.showAllDevices = !m.showAllDevices
		m.selectedIndex = 0 // Reset selection when toggling
		m.scrollOffset = 0
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
		if m.currentTab == TabConfiguration {
			m.setCardProfile()
		} else {
			m.setAsDefault()
		}
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
	case []Card:
		// For cards, count total profiles across all cards
		total := 0
		for _, card := range items {
			total += len(card.Profiles)
		}
		return total
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
			// Filter monitor sources unless showAllDevices is true
			if !m.showAllDevices && d.IsMonitor {
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

	case TabConfiguration:
		if query == "" {
			return m.cards
		}
		filtered := []Card{}
		for _, c := range m.cards {
			if strings.Contains(strings.ToLower(c.Description), query) ||
				strings.Contains(strings.ToLower(c.Name), query) {
				filtered = append(filtered, c)
			}
		}
		return filtered
	}

	return nil
}

// adjustScroll adjusts the scroll offset to keep the selected item visible
func (m *model) adjustScroll() {
	// Calculate available height for content
	// Account for: tabs (3 lines), status bar (2 lines), help (2 lines), search bar if active (2 lines)
	headerHeight := 3
	footerHeight := 4
	if m.searchMode {
		footerHeight += 2
	}
	
	availableHeight := m.height - headerHeight - footerHeight
	if availableHeight < 1 {
		availableHeight = 1
	}

	// Get the visual line for the selected index
	visualLine := m.getVisualLineForIndex(m.selectedIndex)
	
	// Scroll down if selected item is below visible area
	if visualLine >= m.scrollOffset+availableHeight {
		m.scrollOffset = visualLine - availableHeight + 1
	}
	
	// Scroll up if selected item is above visible area
	if visualLine < m.scrollOffset {
		m.scrollOffset = visualLine
	}
	
	// Ensure scroll offset doesn't go negative
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// getVisualLineForIndex returns which visual line an item index corresponds to
// This accounts for multi-line items (devices/streams have 2 lines each, cards have header + profiles)
func (m *model) getVisualLineForIndex(index int) int {
	items := m.getFilteredItems()
	
	switch m.currentTab {
	case TabPlayback, TabRecording:
		// Each stream takes 2 lines (name + volume bar)
		return index * 2
		
	case TabOutputDevices, TabInputDevices:
		// Each device takes 2-3 lines (description + volume/peak meter)
		return index * 3
		
	case TabConfiguration:
		// For cards: count lines including card headers
		cards := items.([]Card)
		currentIndex := 0
		line := 0
		
		for _, card := range cards {
			line++ // Card header
			for range card.Profiles {
				if currentIndex == index {
					return line
				}
				line++ // Profile line
				currentIndex++
			}
			line++ // Blank line after card
		}
		return line
	}
	
	return index
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

func (m *model) setCardProfile() {
	items := m.getFilteredItems()
	cards := items.([]Card)

	// Find which card and profile the selectedIndex refers to
	currentIndex := 0
	for _, card := range cards {
		for _, profile := range card.Profiles {
			if currentIndex == m.selectedIndex {
				// Found the selected profile
				if profile.Available {
					err := m.pulseClient.SetCardProfile(card.Index, profile.Name)
					if err != nil {
						m.err = fmt.Errorf("failed to set profile: %w", err)
					}
				}
				return
			}
			currentIndex++
		}
	}
}

func (m model) View() string {
	var b strings.Builder

	// Render tabs
	b.WriteString(m.renderTabs())
	b.WriteString("\n\n")

	// Render content with scrolling
	content := m.renderContent()
	scrolledContent := m.applyScrolling(content)
	b.WriteString(scrolledContent)

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

// applyScrolling applies vertical scrolling to content based on scrollOffset
func (m model) applyScrolling(content string) string {
	lines := strings.Split(content, "\n")
	
	// Calculate available height for content
	headerHeight := 3
	footerHeight := 4
	if m.searchMode {
		footerHeight += 2
	}
	
	availableHeight := m.height - headerHeight - footerHeight
	if availableHeight < 1 {
		availableHeight = 1
	}
	
	// Apply scrolling
	startLine := m.scrollOffset
	endLine := m.scrollOffset + availableHeight
	
	if startLine >= len(lines) {
		return ""
	}
	
	if endLine > len(lines) {
		endLine = len(lines)
	}
	
	visibleLines := lines[startLine:endLine]
	return strings.Join(visibleLines, "\n")
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
	case TabConfiguration:
		return m.renderCards(items.([]Card))
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

func (m model) renderCards(cards []Card) string {
	if len(cards) == 0 {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("No sound cards found")
	}

	var b strings.Builder
	currentIndex := 0

	for _, card := range cards {
		// Card header
		cardHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Render(card.Description)

		b.WriteString(cardHeader)
		b.WriteString("\n")

		// Render each profile
		for _, profile := range card.Profiles {
			isSelected := currentIndex == m.selectedIndex
			isActive := profile.Name == card.ActiveProfile

			// Build profile line
			var line strings.Builder

			// Active indicator
			if isActive {
				line.WriteString("● ")
			} else {
				line.WriteString("  ")
			}

			// Profile description
			line.WriteString(profile.Description)

			// Sink/Source counts
			line.WriteString(fmt.Sprintf(" (%d out, %d in)", profile.SinkCount, profile.SourceCount))

			// Unavailable indicator
			if !profile.Available {
				line.WriteString(" [UNAVAILABLE]")
			}

			// Apply styling
			style := lipgloss.NewStyle().Padding(0, 1)
			if !profile.Available {
				// Grey out unavailable profiles
				style = style.Foreground(lipgloss.Color("240"))
			} else if isActive {
				// Highlight active profile
				style = style.Foreground(lipgloss.Color("46")) // Green
			}

			if isSelected {
				style = style.
					Background(lipgloss.Color("237")).
					Bold(true)
			}

			b.WriteString(style.Render(line.String()))
			b.WriteString("\n")

			currentIndex++
		}

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
	if m.showAllDevices {
		if m.currentTab == TabInputDevices {
			status += "  [Showing All + Monitors]"
		} else if m.currentTab == TabOutputDevices {
			status += "  [Showing All Devices]"
		}
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(status)
}

func (m model) renderHelp() string {
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1)

	help := "↑↓/jk: Navigate  PgUp/PgDn: Jump  g/G: Top/Bottom  Tab: Switch  /: Search"

	if m.currentTab == TabPlayback || m.currentTab == TabRecording {
		help += "  +/-: Volume"
	} else if m.currentTab == TabOutputDevices || m.currentTab == TabInputDevices {
		help += "  +/-: Volume  d: Default  a: All"
	} else if m.currentTab == TabConfiguration {
		help += "  Enter: Select"
	}

	help += "  q: Quit"

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
