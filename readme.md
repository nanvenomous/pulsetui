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
  - Press `a` to toggle showing all devices
- **Device availability indicators**:
  - `✓` Default device (currently in use)
  - `●` Available device
  - `⊗` Unavailable device (shown only when "Show All" is enabled)
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

- PulseAudio with `pactl` command
- Linux system with PulseAudio running

