package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Device represents an audio device (sink or source)
type Device struct {
	Index       int
	Name        string
	Description string
	Volume      int // 0 to 100
	Muted       bool
	IsDefault   bool
	Available   bool    // true if device is available/usable
	State       string  // RUNNING, SUSPENDED, IDLE, etc.
	PeakLevel   float64 // 0.0 to 1.0, real-time audio level
	IsMonitor   bool    // true if this is a monitor source (e.g., "Monitor of...")
}

// Stream represents an audio stream (sink input or source output)
type Stream struct {
	Index       int
	Name        string
	AppName     string
	Volume      int
	Muted       bool
	DeviceIndex int
}

// Card represents a sound card with multiple profiles
type Card struct {
	Index         int
	Name          string
	Description   string
	ActiveProfile string // Technical name of active profile
	Profiles      []Profile
}

// Profile represents a configuration option for a card
type Profile struct {
	Name        string // Technical name (e.g., "output:mono-fallback")
	Description string // Display name (e.g., "Mono Output")
	Available   bool
	SinkCount   int // Number of output devices created
	SourceCount int // Number of input devices created
	Priority    int
}

// PeakMonitor monitors the peak audio level for a source
type PeakMonitor struct {
	sourceIndex int
	sourceName  string
	cmd         *exec.Cmd
	peakLevel   float64
	running     bool
	mu          sync.Mutex
	decay       float64 // Decay rate for smooth visual effect
}

// PulseClient wraps PulseAudio operations using pactl
type PulseClient struct {
	peakMonitors map[int]*PeakMonitor
	monitorMu    sync.Mutex
}

// NewPulseClient creates a new PulseAudio client
func NewPulseClient() (*PulseClient, error) {
	// Test if pactl is available
	if _, err := exec.LookPath("pactl"); err != nil {
		return nil, fmt.Errorf("pactl not found: %w", err)
	}
	// Test if parec is available
	if _, err := exec.LookPath("parec"); err != nil {
		return nil, fmt.Errorf("parec not found: %w", err)
	}
	return &PulseClient{
		peakMonitors: make(map[int]*PeakMonitor),
	}, nil
}

// Close closes the PulseAudio connection and stops all monitors
func (pc *PulseClient) Close() {
	pc.StopAllPeakMonitors()
}

// StartPeakMonitor starts monitoring the peak level for a source
func (pc *PulseClient) StartPeakMonitor(sourceIndex int, sourceName string) error {
	pc.monitorMu.Lock()
	defer pc.monitorMu.Unlock()

	// Check if already monitoring
	if monitor, exists := pc.peakMonitors[sourceIndex]; exists {
		if monitor.running {
			return nil // Already running
		}
	}

	monitor := &PeakMonitor{
		sourceIndex: sourceIndex,
		sourceName:  sourceName,
		running:     true,
		decay:       0.95, // Smooth decay
	}

	// For input sources, use the source name directly (no .monitor suffix needed)
	// Input sources (microphones) are already capturable sources
	monitorName := sourceName

	// Start parec with optimized settings for peak detection
	monitor.cmd = exec.Command("parec",
		"-d", monitorName,
		"--format=s16le",    // 16-bit signed little-endian
		"--rate=8000",       // Low sample rate for efficiency
		"--channels=1",      // Mono
		"--latency-msec=50") // 50ms latency for responsiveness

	stdout, err := monitor.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := monitor.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start parec: %w", err)
	}

	// Start reading samples in a goroutine
	go monitor.readSamples(stdout)

	pc.peakMonitors[sourceIndex] = monitor
	return nil
}

// StopPeakMonitor stops monitoring a specific source
func (pc *PulseClient) StopPeakMonitor(sourceIndex int) {
	pc.monitorMu.Lock()
	defer pc.monitorMu.Unlock()

	if monitor, exists := pc.peakMonitors[sourceIndex]; exists {
		monitor.Stop()
		delete(pc.peakMonitors, sourceIndex)
	}
}

// StopAllPeakMonitors stops all peak monitors
func (pc *PulseClient) StopAllPeakMonitors() {
	pc.monitorMu.Lock()
	defer pc.monitorMu.Unlock()

	for _, monitor := range pc.peakMonitors {
		monitor.Stop()
	}
	pc.peakMonitors = make(map[int]*PeakMonitor)
}

// GetPeakLevel returns the current peak level for a source
func (pc *PulseClient) GetPeakLevel(sourceIndex int) float64 {
	pc.monitorMu.Lock()
	defer pc.monitorMu.Unlock()

	if monitor, exists := pc.peakMonitors[sourceIndex]; exists {
		return monitor.GetPeakLevel()
	}
	return 0.0
}

// Stop stops the peak monitor
func (pm *PeakMonitor) Stop() {
	pm.mu.Lock()
	pm.running = false
	pm.mu.Unlock()

	if pm.cmd != nil && pm.cmd.Process != nil {
		pm.cmd.Process.Kill()
		pm.cmd.Wait()
	}
}

// GetPeakLevel returns the current peak level
func (pm *PeakMonitor) GetPeakLevel() float64 {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.peakLevel
}

// readSamples reads audio samples and calculates peak levels
func (pm *PeakMonitor) readSamples(r io.Reader) {
	// Buffer size for 50ms of audio at 8000 Hz, 16-bit mono
	// 8000 samples/sec * 0.05 sec * 2 bytes/sample = 800 bytes
	buf := make([]byte, 800)

	for {
		pm.mu.Lock()
		running := pm.running
		pm.mu.Unlock()

		if !running {
			break
		}

		n, err := io.ReadFull(r, buf)
		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				// Only log non-EOF errors if needed
			}
			break
		}

		// Calculate peak amplitude from samples
		var peak float64
		for i := 0; i < n-1; i += 2 {
			// Read 16-bit signed sample (little-endian)
			sample := int16(binary.LittleEndian.Uint16(buf[i : i+2]))

			// Convert to absolute value and normalize to 0.0-1.0
			absValue := math.Abs(float64(sample)) / 32768.0
			if absValue > peak {
				peak = absValue
			}
		}

		// Update peak level with decay
		pm.mu.Lock()
		// If new peak is higher, use it; otherwise apply decay
		if peak > pm.peakLevel {
			pm.peakLevel = peak
		} else {
			pm.peakLevel *= pm.decay
		}
		pm.mu.Unlock()

		// Small sleep to avoid spinning too fast
		time.Sleep(10 * time.Millisecond)
	}
}

// parseDeviceList parses pactl list output for sinks or sources
func parseDeviceList(output string, deviceType string) []Device {
	devices := []Device{}

	// Split by device sections
	pattern := regexp.MustCompile(`(?m)^` + deviceType + ` #(\d+)`)
	matches := pattern.FindAllStringSubmatchIndex(output, -1)

	for i, match := range matches {
		start := match[0]
		end := len(output)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}

		section := output[start:end]
		device := Device{Index: -1, Volume: 50, Available: true} // Default to available

		// Parse index
		if idxMatch := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(section); len(idxMatch) > 1 {
			device.Index, _ = strconv.Atoi(idxMatch[1])
		}

		// Parse state
		if stateMatch := regexp.MustCompile(`(?m)^\s*State: (.+)$`).FindStringSubmatch(section); len(stateMatch) > 1 {
			device.State = strings.TrimSpace(stateMatch[1])
		}

		// Parse name
		if nameMatch := regexp.MustCompile(`(?m)^\s*Name: (.+)$`).FindStringSubmatch(section); len(nameMatch) > 1 {
			device.Name = strings.TrimSpace(nameMatch[1])
		}

		// Parse description
		if descMatch := regexp.MustCompile(`(?m)^\s*Description: (.+)$`).FindStringSubmatch(section); len(descMatch) > 1 {
			device.Description = strings.TrimSpace(descMatch[1])
		}

		// Check if this is a monitor source
		if strings.HasPrefix(device.Description, "Monitor of ") {
			device.IsMonitor = true
		}

		// Parse volume
		if volMatch := regexp.MustCompile(`(?m)Volume:.*?(\d+)%`).FindStringSubmatch(section); len(volMatch) > 1 {
			device.Volume, _ = strconv.Atoi(volMatch[1])
		}

		// Parse mute
		if muteMatch := regexp.MustCompile(`(?m)^\s*Mute: (yes|no)`).FindStringSubmatch(section); len(muteMatch) > 1 {
			device.Muted = muteMatch[1] == "yes"
		}

		// Parse port availability, preferring the active port over inactive ones.
		activePort := ""
		if activeMatch := regexp.MustCompile(`(?m)^\s*Active Port: (.+)$`).FindStringSubmatch(section); len(activeMatch) > 1 {
			activePort = strings.TrimSpace(activeMatch[1])
		}

		portAvailability := map[string]string{}
		portPattern := regexp.MustCompile(`(?mi)^\s+(\S+):.*\b(available|availability)\s*[: ]\s*([a-z]+)`)
		portMatches := portPattern.FindAllStringSubmatch(section, -1)
		for _, pm := range portMatches {
			if len(pm) > 3 {
				portAvailability[strings.TrimSpace(pm[1])] = strings.ToLower(pm[3])
			}
		}

		isPortAvailable := func(status string) bool {
			status = strings.ToLower(status)
			return status != "no" && status != "not"
		}

		// Default to available unless we can derive a better answer
		device.Available = true
		if activePort != "" {
			if status, ok := portAvailability[activePort]; ok {
				device.Available = isPortAvailable(status)
			}
		} else if len(portAvailability) > 0 {
			device.Available = false
			for _, status := range portAvailability {
				if isPortAvailable(status) {
					device.Available = true
					break
				}
			}
		}

		if device.Index >= 0 && device.Name != "" {
			devices = append(devices, device)
		}
	}

	return devices
}

// ListSinks returns all output devices
func (pc *PulseClient) ListSinks() ([]Device, error) {
	cmd := exec.Command("pactl", "list", "sinks")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list sinks: %w", err)
	}

	devices := parseDeviceList(string(output), "Sink")

	// Get default sink
	cmd = exec.Command("pactl", "get-default-sink")
	if defaultOutput, err := cmd.Output(); err == nil {
		defaultName := strings.TrimSpace(string(defaultOutput))
		for i := range devices {
			if devices[i].Name == defaultName {
				devices[i].IsDefault = true
				break
			}
		}
	}

	return devices, nil
}

// ListSources returns all input devices
func (pc *PulseClient) ListSources() ([]Device, error) {
	cmd := exec.Command("pactl", "list", "sources")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list sources: %w", err)
	}

	devices := parseDeviceList(string(output), "Source")

	// Get default source
	cmd = exec.Command("pactl", "get-default-source")
	if defaultOutput, err := cmd.Output(); err == nil {
		defaultName := strings.TrimSpace(string(defaultOutput))
		for i := range devices {
			if devices[i].Name == defaultName {
				devices[i].IsDefault = true
				break
			}
		}
	}

	return devices, nil
}

// parseStreamList parses pactl list output for sink-inputs or source-outputs
func parseStreamList(output string, streamType string) []Stream {
	streams := []Stream{}

	pattern := regexp.MustCompile(`(?m)^` + streamType + ` #(\d+)`)
	matches := pattern.FindAllStringSubmatchIndex(output, -1)

	for i, match := range matches {
		start := match[0]
		end := len(output)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}

		section := output[start:end]
		stream := Stream{Index: -1, Volume: 50}

		// Parse index
		if idxMatch := regexp.MustCompile(`#(\d+)`).FindStringSubmatch(section); len(idxMatch) > 1 {
			stream.Index, _ = strconv.Atoi(idxMatch[1])
		}

		// Parse application name
		if appMatch := regexp.MustCompile(`(?m)^\s*application\.name = "(.+?)"`).FindStringSubmatch(section); len(appMatch) > 1 {
			stream.AppName = appMatch[1]
		}

		// Parse media name
		if nameMatch := regexp.MustCompile(`(?m)^\s*media\.name = "(.+?)"`).FindStringSubmatch(section); len(nameMatch) > 1 {
			stream.Name = nameMatch[1]
		}

		if stream.Name == "" {
			stream.Name = stream.AppName
		}

		// Parse volume
		if volMatch := regexp.MustCompile(`(?m)Volume:.*?(\d+)%`).FindStringSubmatch(section); len(volMatch) > 1 {
			stream.Volume, _ = strconv.Atoi(volMatch[1])
		}

		// Parse mute
		if muteMatch := regexp.MustCompile(`(?m)^\s*Mute: (yes|no)`).FindStringSubmatch(section); len(muteMatch) > 1 {
			stream.Muted = muteMatch[1] == "yes"
		}

		// Parse device index
		devicePattern := "Sink"
		if streamType == "Source Output" {
			devicePattern = "Source"
		}
		if devMatch := regexp.MustCompile(devicePattern + `: (\d+)`).FindStringSubmatch(section); len(devMatch) > 1 {
			stream.DeviceIndex, _ = strconv.Atoi(devMatch[1])
		}

		if stream.Index >= 0 {
			streams = append(streams, stream)
		}
	}

	return streams
}

// ListSinkInputs returns all playback streams
func (pc *PulseClient) ListSinkInputs() ([]Stream, error) {
	cmd := exec.Command("pactl", "list", "sink-inputs")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list sink inputs: %w", err)
	}

	return parseStreamList(string(output), "Sink Input"), nil
}

// ListSourceOutputs returns all recording streams
func (pc *PulseClient) ListSourceOutputs() ([]Stream, error) {
	cmd := exec.Command("pactl", "list", "source-outputs")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list source outputs: %w", err)
	}

	return parseStreamList(string(output), "Source Output"), nil
}

// SetSinkVolume sets the volume for a sink
func (pc *PulseClient) SetSinkVolume(index int, volume int) error {
	cmd := exec.Command("pactl", "set-sink-volume", strconv.Itoa(index), fmt.Sprintf("%d%%", volume))
	return cmd.Run()
}

// SetSourceVolume sets the volume for a source
func (pc *PulseClient) SetSourceVolume(index int, volume int) error {
	cmd := exec.Command("pactl", "set-source-volume", strconv.Itoa(index), fmt.Sprintf("%d%%", volume))
	return cmd.Run()
}

// SetSinkInputVolume sets the volume for a sink input
func (pc *PulseClient) SetSinkInputVolume(index int, volume int) error {
	cmd := exec.Command("pactl", "set-sink-input-volume", strconv.Itoa(index), fmt.Sprintf("%d%%", volume))
	return cmd.Run()
}

// SetSourceOutputVolume sets the volume for a source output
func (pc *PulseClient) SetSourceOutputVolume(index int, volume int) error {
	cmd := exec.Command("pactl", "set-source-output-volume", strconv.Itoa(index), fmt.Sprintf("%d%%", volume))
	return cmd.Run()
}

// SetDefaultSink sets the default output device
func (pc *PulseClient) SetDefaultSink(name string) error {
	cmd := exec.Command("pactl", "set-default-sink", name)
	return cmd.Run()
}

// SetDefaultSource sets the default input device
func (pc *PulseClient) SetDefaultSource(name string) error {
	cmd := exec.Command("pactl", "set-default-source", name)
	return cmd.Run()
}

// ToggleSinkMute toggles mute for a sink
func (pc *PulseClient) ToggleSinkMute(index int) error {
	cmd := exec.Command("pactl", "set-sink-mute", strconv.Itoa(index), "toggle")
	return cmd.Run()
}

// ToggleSourceMute toggles mute for a source
func (pc *PulseClient) ToggleSourceMute(index int) error {
	cmd := exec.Command("pactl", "set-source-mute", strconv.Itoa(index), "toggle")
	return cmd.Run()
}

// MoveSinkInput moves a sink input to a different sink
func (pc *PulseClient) MoveSinkInput(inputIndex, sinkIndex int) error {
	cmd := exec.Command("pactl", "move-sink-input", strconv.Itoa(inputIndex), strconv.Itoa(sinkIndex))
	return cmd.Run()
}

// MoveSourceOutput moves a source output to a different source
func (pc *PulseClient) MoveSourceOutput(outputIndex, sourceIndex int) error {
	cmd := exec.Command("pactl", "move-source-output", strconv.Itoa(outputIndex), strconv.Itoa(sourceIndex))
	return cmd.Run()
}

// parseCardList parses pactl list output for cards
func parseCardList(output string) []Card {
	cards := []Card{}

	// Split by card sections
	pattern := regexp.MustCompile(`(?m)^Card #(\d+)`)
	matches := pattern.FindAllStringSubmatchIndex(output, -1)

	for i, match := range matches {
		start := match[0]
		end := len(output)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}

		section := output[start:end]
		card := Card{Index: -1}

		// Parse index
		if idxMatch := regexp.MustCompile(`Card #(\d+)`).FindStringSubmatch(section); len(idxMatch) > 1 {
			card.Index, _ = strconv.Atoi(idxMatch[1])
		}

		// Parse name
		if nameMatch := regexp.MustCompile(`(?m)^\s*Name: (.+)$`).FindStringSubmatch(section); len(nameMatch) > 1 {
			card.Name = strings.TrimSpace(nameMatch[1])
		}

		// Parse description - try device.description property first, fallback to alsa.card_name
		if descMatch := regexp.MustCompile(`(?m)^\s*device\.description = "(.+)"`).FindStringSubmatch(section); len(descMatch) > 1 {
			card.Description = strings.TrimSpace(descMatch[1])
		} else if descMatch := regexp.MustCompile(`(?m)^\s*alsa\.card_name = "(.+)"`).FindStringSubmatch(section); len(descMatch) > 1 {
			card.Description = strings.TrimSpace(descMatch[1])
		}

		// Parse active profile
		if activeMatch := regexp.MustCompile(`(?m)^\s*Active Profile: (.+)$`).FindStringSubmatch(section); len(activeMatch) > 1 {
			card.ActiveProfile = strings.TrimSpace(activeMatch[1])
		}

		// Parse profiles section
		profilesPattern := regexp.MustCompile(`(?s)Profiles:\s*\n(.*?)(?:\n\s*Active Profile:|\n\s*Ports:|\z)`)
		if profilesMatch := profilesPattern.FindStringSubmatch(section); len(profilesMatch) > 1 {
			profilesText := profilesMatch[1]

			// Parse individual profile lines
			// Format: "profile-name: Description (sinks: N, sources: M, priority: P, available: yes/no)"
			// Profile names can contain colons (e.g., "output:mono-fallback+input:mono-fallback")
			// So we match everything up to ": " followed by the description
			profileLinePattern := regexp.MustCompile(`(?m)^\s*(.+?):\s+(.+?)\s+\(sinks:\s*(\d+),\s*sources:\s*(\d+),\s*priority:\s*(\d+),\s*available:\s*(yes|no)\)`)
			profileMatches := profileLinePattern.FindAllStringSubmatch(profilesText, -1)

			for _, pm := range profileMatches {
				if len(pm) > 6 {
					profile := Profile{
						Name:        strings.TrimSpace(pm[1]),
						Description: strings.TrimSpace(pm[2]),
						Available:   pm[6] == "yes",
					}
					profile.SinkCount, _ = strconv.Atoi(pm[3])
					profile.SourceCount, _ = strconv.Atoi(pm[4])
					profile.Priority, _ = strconv.Atoi(pm[5])

					card.Profiles = append(card.Profiles, profile)
				}
			}
		}

		if card.Index >= 0 && card.Name != "" {
			cards = append(cards, card)
		}
	}

	return cards
}

// ListCards returns all sound cards with their profiles
func (pc *PulseClient) ListCards() ([]Card, error) {
	cmd := exec.Command("pactl", "list", "cards")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list cards: %w", err)
	}

	return parseCardList(string(output)), nil
}

// SetCardProfile changes the active profile for a card
func (pc *PulseClient) SetCardProfile(cardIndex int, profileName string) error {
	cmd := exec.Command("pactl", "set-card-profile", strconv.Itoa(cardIndex), profileName)
	return cmd.Run()
}
