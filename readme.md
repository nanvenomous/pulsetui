# PulseTUI

A simple and clean TUI (Terminal User Interface) application for managing PulseAudio, inspired by pavucontrol.

## Features

- **Clean interface**: Simplified design compared to pavucontrol for better visual clarity
- **Tab navigation**: Four main tabs
  - Playback: Active audio streams playing
  - Recording: Active recording streams
  - Output Devices: Audio output devices (speakers, headphones)
  - Input Devices: Audio input devices (microphones)
- **Smart filtering**: By default, only shows available/usable devices
  - Unavailable devices (e.g., unplugged HDMI) are hidden
  - Monitor sources (e.g., "Monitor of Speaker") are hidden on Input Devices tab
  - Press `a` to toggle showing all devices and monitors
- **Device availability indicators**:
  - `✓` Default device (currently in use)
  - `●` Available device
  - `⊗` Unavailable device (shown only when "Show All" is enabled)
- **Real-time peak level monitoring**: Input devices show live audio level meters
  - Tap your microphone to see which device it is!
  - Color-coded: gray (quiet), green (active), orange (good level), red (clipping)
  - Low CPU usage (~1-2% per monitored device)
- **Device search**: Quick search with `/` key
- **Volume control**: Visual volume bars with +/- keys (color-coded: green/orange/red)
- **Default device selection**: Set default devices with Enter/d
- **Real-time updates**: Immediate refresh on changes + 1-second polling for external updates

## Building

```bash
go build
```

## Running

```bash
./pulsetui
```

## Keyboard Shortcuts

### Navigation
- `Tab` / `→`: Next tab
- `Shift+Tab` / `←`: Previous tab
- `↑` / `k`: Move up
- `↓` / `j`: Move down
- `/`: Search mode

### Volume & Device Control
- `+` / `=`: Increase volume (+5%)
- `-` / `_`: Decrease volume (-5%)
- `m`: Toggle mute (devices only)
- `d` / `Enter`: Set as default (devices only)

### View Options
- `a`: Toggle show all devices (including unavailable)
- `r`: Manual refresh
- `q` / `Ctrl+C`: Quit

## Requirements

- PulseAudio with `pactl` and `parec` commands
- Linux system with PulseAudio running

## How Peak Level Monitoring Works

The peak level meter on the Input Devices tab shows real-time audio levels for each microphone/input device. This makes it easy to identify which physical device is which:

1. Navigate to the "Input Devices" tab
2. Tap or make noise near your microphone
3. Watch the peak meter `[=====     ]` light up in real-time
4. The meter updates 20 times per second for smooth visualization

**Technical details:**
- Uses PulseAudio's monitor sources to capture audio levels
- Optimized with 8kHz sample rate (sufficient for level detection)
- Peak meters only active when viewing Input Devices tab (no overhead otherwise)
- Smooth decay for better visual feedback

