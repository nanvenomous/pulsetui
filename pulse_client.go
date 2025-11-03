package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Device represents an audio device (sink or source)
type Device struct {
	Index       int
	Name        string
	Description string
	Volume      int // 0 to 100
	Muted       bool
	IsDefault   bool
	Available   bool   // true if device is available/usable
	State       string // RUNNING, SUSPENDED, IDLE, etc.
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

// PulseClient wraps PulseAudio operations using pactl
type PulseClient struct{}

// NewPulseClient creates a new PulseAudio client
func NewPulseClient() (*PulseClient, error) {
	// Test if pactl is available
	if _, err := exec.LookPath("pactl"); err != nil {
		return nil, fmt.Errorf("pactl not found: %w", err)
	}
	return &PulseClient{}, nil
}

// Close closes the PulseAudio connection (no-op for pactl)
func (pc *PulseClient) Close() {}

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

		// Parse volume
		if volMatch := regexp.MustCompile(`(?m)Volume:.*?(\d+)%`).FindStringSubmatch(section); len(volMatch) > 1 {
			device.Volume, _ = strconv.Atoi(volMatch[1])
		}

		// Parse mute
		if muteMatch := regexp.MustCompile(`(?m)^\s*Mute: (yes|no)`).FindStringSubmatch(section); len(muteMatch) > 1 {
			device.Muted = muteMatch[1] == "yes"
		}

		// Parse port availability
		// Look for "not available" in the Ports section
		if strings.Contains(section, "not available") {
			device.Available = false
		}
		// If we find "available" (not "not available"), it's available
		if regexp.MustCompile(`availability.*?\bavailable\b`).MatchString(section) &&
			!strings.Contains(section, "not available") {
			device.Available = true
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
