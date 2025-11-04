# PulseTUI - Developer Guide

## Project Overview

**PulseTUI** is a Terminal User Interface (TUI) application for managing PulseAudio on Linux, inspired by pavucontrol. Built with Go and the Bubble Tea framework for interactive terminal UI.

**Purpose**: Provides a clean, keyboard-driven interface to:
- View and control audio playback/recording streams
- Manage output devices (speakers, headphones)
- Manage input devices (microphones) with real-time peak level monitoring
- Configure sound card profiles (mono/stereo, input/output combinations)
- Adjust volumes, mute devices, set defaults

## Tech Stack

- **Language**: Go 1.25.3
- **UI Framework**: [Charm Bubble Tea](https://github.com/charmbracelet/bubbletea) (Elm-inspired TUI)
- **Styling**: [Charm Lipgloss](https://github.com/charmbracelet/lipgloss)
- **Audio Backend**: PulseAudio via `pactl` and `parec` command-line tools

## Project Structure

```
pulsetui/
├── main.go           - TUI application logic, UI rendering, event handling
├── pulse_client.go   - PulseAudio client wrapper, peak monitoring
├── go.mod            - Go module dependencies
├── readme.md         - User documentation
└── test_monitor_filter.sh - Manual test script
```

### File Organization

**main.go** (~870 lines):
- Defines the Bubble Tea model and state management
- Handles keyboard navigation and commands
- Renders all UI components (tabs, device lists, volume bars, peak meters, card profiles)
- Main function initializes and runs the TUI

**pulse_client.go** (~610 lines):
- `PulseClient`: Wrapper around PulseAudio pactl commands
- `Device`: Represents audio devices (sinks/sources)
- `Stream`: Represents active audio streams
- `Card`: Represents sound cards with profiles
- `Profile`: Represents configuration options for cards
- `PeakMonitor`: Real-time audio level monitoring using parec
- Parsing functions for pactl output (devices, streams, cards)
- All PulseAudio control operations (volume, mute, default device, profiles, etc.)

## Essential Commands

### Build
```bash
go build
```

Produces a single binary `./pulsetui` with no additional dependencies (except PulseAudio).

### Run
```bash
./pulsetui
```

### Run (Direct)
```bash
go run .
```

### Test Monitor Filtering
```bash
./test_monitor_filter.sh
```

### Dependencies
```bash
go mod download  # Download dependencies
go mod tidy      # Clean up dependencies
```

## Code Architecture

### Bubble Tea Pattern (Elm Architecture)

The application follows the Model-Update-View pattern:

1. **Model** (`model` struct): Application state
   - Current tab, selection, search query
   - Cached audio data (devices, streams, cards)
   - PulseClient instance

2. **Update** (`Update` method): State transitions based on messages
   - `tea.KeyMsg`: Keyboard input
   - `tickMsg`: Periodic updates (1 second)
   - `updateDataMsg`: Refresh audio data
   - `updatePeaksMsg`: Update peak meters (50ms)

3. **View** (`View` method): Render current state to string
   - Returns full terminal output as formatted string
   - Uses Lipgloss for styling

4. **Commands** (`tea.Cmd`): Async operations
   - Return messages to trigger state updates
   - Used for timers, data fetching

### State Management

**Tabs** (5 main views):
- `TabPlayback`: Active audio playback streams
- `TabRecording`: Active recording streams
- `TabOutputDevices`: Audio output devices
- `TabInputDevices`: Audio input devices with peak monitoring
- `TabConfiguration`: Sound card profile management

**Data Flow**:
1. User action triggers keyboard event
2. `Update()` handles event, modifies state
3. May execute pactl commands via `PulseClient`
4. Returns command to refresh data
5. `View()` re-renders with new state

**Polling**: 1-second timer continuously refreshes current tab data

### Peak Level Monitoring (Input Devices Only)

**Implementation** (pulse_client.go:40-225):
- Uses `parec` (PulseAudio record) to capture live audio
- Spawns goroutine per monitored source
- Reads 16-bit PCM samples at 8kHz (optimized for efficiency)
- Calculates peak amplitude every 50ms
- Smooth decay for better visual feedback
- Only active when viewing Input Devices tab

**Performance**:
- ~1-2% CPU per monitored device
- Automatically starts monitors when entering Input Devices tab
- Automatically stops when switching tabs or quitting

## Naming Conventions

### Variables
- **camelCase** for local variables: `selectedIndex`, `searchQuery`
- **PascalCase** for exported types/functions: `PulseClient`, `Device`
- Descriptive names preferred over abbreviations

### Types
- Struct names are singular nouns: `Device`, `Stream`, `PeakMonitor`
- Interface pattern not used (direct implementation)

### Functions
- Methods use receiver: `(pc *PulseClient) ListSinks()`
- Boolean functions use `is` prefix: `IsMonitor`
- Action functions use imperative verbs: `SetVolume`, `ToggleMute`

### Constants
- `Tab` enum uses `TabPlayback`, `TabRecording` pattern
- Global arrays like `tabNames` use camelCase

## Code Patterns

### PulseAudio Interaction

**Command Execution Pattern**:
```go
cmd := exec.Command("pactl", "list", "sinks")
output, err := cmd.Output()
if err != nil {
    return nil, fmt.Errorf("failed to list sinks: %w", err)
}
// Parse output...
```

**Parsing Pattern**:
- Use regex to split output into device/stream sections
- Extract fields with additional regex patterns
- Build struct with defaults, then populate from matches

### Concurrency

**Peak Monitoring**:
- Each `PeakMonitor` runs its own goroutine
- Mutex-protected shared state (`peakLevel`, `running`)
- Cleanup pattern: Set `running=false`, kill process, wait

**Synchronization**:
- `monitorMu` protects `peakMonitors` map
- Individual monitor mutex protects internal state
- No shared state between monitors

### Error Handling

**Pattern**: Return errors up, display in UI status bar
```go
if err != nil {
    m.err = err  // Store in model
}
```

**User-Facing**: Errors shown in red status bar (main.go:684-689)

### Filtering Logic

**Device Availability** (main.go:305-343):
- By default, hide unavailable devices (`Available == false`)
- By default, hide monitor sources (`IsMonitor == true`) on Input Devices tab
- Toggle with `a` key to show all

**Determination** (pulse_client.go:280-289):
- `Available`: Check for "not available" in port info
- `IsMonitor`: Description starts with "Monitor of "

### UI Rendering

**Style Pattern**:
```go
style := lipgloss.NewStyle().
    Foreground(lipgloss.Color("240")).
    Padding(0, 1)
return style.Render(text)
```

**Color Coding**:
- Volume bars: Green (≤80%), Orange (81-100%), Red (>100%)
- Peak meters: Gray (quiet), Green (active), Orange (good), Red (clipping)
- Unavailable devices: Foreground color 240 (gray)
- Active card profile: Green (color 46)

**Icons**:
- `✓` Default device
- `●` Available device / Active profile
- `⊗` Unavailable device

### Card Profile Management

**Concept** (pulse_client.go:39-60):
- `Card`: Physical sound card hardware
- `Profile`: Configuration option (e.g., "Mono Output + Mono Input", "Stereo Output", "Off")
- Each profile creates different sinks/sources when activated
- Profiles have sink/source counts, priority, availability

**Parsing** (pulse_client.go:517-578):
- Parse `pactl list cards` output with regex
- Extract card index, name, description
- Parse profiles section with counts and availability
- Identify active profile by name match

**Profile Selection** (main.go:477-495):
- Configuration tab displays all cards with their profiles
- Selection index spans all profiles across all cards
- When profile selected, map index back to card + profile
- Call `SetCardProfile(cardIndex, profileName)` to activate

**UI Pattern** (main.go:684-752):
- Show card name as header (bold)
- List all profiles indented
- Active profile marked with ● in green
- Unavailable profiles grayed out
- Show sink/source counts for each profile

## Important Patterns & Gotchas

### 1. Tab-Specific Data Loading
Only the current tab's data is refreshed on each tick. When switching tabs, explicitly trigger `updateDataMsg` to load new data.

### 2. Peak Monitors Lifecycle
- Start: When entering Input Devices tab AND device is available
- Stop: When leaving Input Devices tab OR quitting
- Check existing monitors before starting (avoid duplicates)

### 3. Monitor Source Naming
Input sources (microphones) are directly capturable - use the source `Name` directly with parec, NOT `name.monitor`. The `.monitor` suffix is only for sinks (outputs).

### 4. Volume Range
Volume is 0-150% (PulseAudio allows over 100% for amplification). UI caps at 150% to prevent damage.

### 5. Regex Parsing Gotchas
- Use `(?m)` multiline flag for `^` and `$` anchors
- Availability detection requires checking for "not available" (negative check)
- Handle missing fields gracefully with default values
- Profile parsing uses `(?s)` flag for multiline sections

### 6. Search Filtering
Search is case-insensitive and matches both `Name` and `Description` fields. For devices, also respects `showAllDevices` toggle. For cards, matches card name or description.

### 7. Index-Based Selection
Selection index refers to *filtered* items, not full list. Reset `selectedIndex` when toggling filters or changing tabs. For Configuration tab, index spans all profiles across all visible cards.

### 8. Profile Changes Are Disruptive
Changing a card's profile can interrupt active audio streams. Devices (sinks/sources) are destroyed and recreated. Warn users when selecting "off" profile implicitly by showing unavailable status.

## Testing Approach

**Current State**: No automated tests

**Manual Testing**:
- Use `test_monitor_filter.sh` to verify monitor source filtering
- Test with different audio configurations (multiple devices, unplugged devices)
- Verify peak meters respond to microphone input
- Test profile switching in Configuration tab with multiple sound cards

**Testing Workflow**:
1. Build the application
2. Run and navigate through all tabs
3. Test volume changes, mute, default device selection
4. Test with devices plugged/unplugged
5. Verify peak meters on Input Devices tab
6. Test profile switching and verify devices update accordingly

## Development Workflow

### Making Changes

1. **Edit Code**: Modify `main.go` or `pulse_client.go`
2. **Build**: `go build`
3. **Test**: Run `./pulsetui` and verify behavior
4. **Iterate**: No hot reload - rebuild after each change

### Adding New Features

**New Tab**:
1. Add constant to `Tab` enum
2. Add name to `tabNames` array
3. Add data field to `model` struct
4. Add case in `updateData()` to load data
5. Add case in `renderContent()` to display
6. Update keyboard shortcuts if needed

**New Device Operation**:
1. Add method to `PulseClient` with pactl command
2. Add keyboard handler in `handleNormalKey()`
3. Trigger `updateDataMsg` to refresh after change

**New Visual Element**:
1. Create render function returning string
2. Use Lipgloss for styling
3. Call from appropriate render method
4. Test terminal width/height constraints

### Debugging

**PulseAudio Issues**:
```bash
pactl list sinks         # Verify device data
pactl list sources       # Verify source data
pactl list sink-inputs   # Verify stream data
parec -d SOURCE_NAME     # Test audio capture
```

**UI Issues**:
- Add debug output to stderr: `fmt.Fprintf(os.Stderr, "Debug: %v\n", value)`
- Check terminal size constraints
- Verify Lipgloss style application

**Concurrency Issues**:
- Check mutex usage around shared state
- Verify goroutine cleanup (use `defer`)
- Test rapid tab switching

## Dependencies

### Required System Packages
- PulseAudio with `pactl` command (control interface)
- PulseAudio with `parec` command (audio capture for peak monitoring)

### Go Modules
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/lipgloss` - Terminal styling

All other dependencies are indirect (Charm ecosystem utilities).

## Commit Patterns

Based on git history:
- **edit**: Significant feature changes or refinements
- **fix**: Bug fixes
- **initial**: Project initialization

Keep commits focused and descriptive.

## Future Considerations

**Potential Improvements**:
- Automated tests for parsing logic
- Stream moving (route playback to different device)
- Configuration file for preferences
- System tray integration
- More granular volume control
- Port selection within profiles

**Architecture Notes**:
- Consider extracting parsing logic for testability
- Could benefit from interface abstraction for PulseClient (testing, mocking)
- Peak monitoring could be optimized with buffer pooling for very large device counts

## Recent Features

### Configuration Tab (Added Nov 2024)
Adds sound card profile management similar to pavucontrol's Configuration tab. Users can now:
- Switch between mono/stereo/surround profiles
- Enable/disable input or output independently  
- Turn off unused sound cards
- View all available profiles with device counts

**Implementation details**:
- New `Card` and `Profile` types in pulse_client.go
- `ListCards()` and `SetCardProfile()` methods
- `TabConfiguration` with profile selection UI
- Profile index mapping spans all profiles across all cards
- Active profiles marked with ● in green
