# HWiNFO Stream Deck Plugin

[![Build & Test](https://github.com/moeilijk/hwinfo-streamdeck/actions/workflows/build.yml/badge.svg)](https://github.com/moeilijk/hwinfo-streamdeck/actions/workflows/build.yml)

Show live [HWiNFO64](https://www.hwinfo.com) sensor readings (temperatures, loads, clocks, fan speeds, and more) on your Elgato Stream Deck, as graphs, text, or both.

![alt text](images/demo.gif "HWiNFO64 Stream Deck Plugin Demo")

**Requirements:** Windows 10 or later, [HWiNFO64](https://www.hwinfo.com), Stream Deck software 6.9 or later. The Dial Carousel action requires a Stream Deck+.

This plugin reads HWiNFO exclusively. For Libre Hardware Monitor, remote machines, or Linux, use the [lhm-streamdeck](https://github.com/moeilijk/lhm-streamdeck) sister plugin instead.

## v3.0

The plugin core has been replaced with the actively developed engine from the [lhm-streamdeck](https://github.com/moeilijk/lhm-streamdeck) sister project. v3 also has a new plugin identity (`com.moeilijk.hwinfo`): tiles configured with the original v2.0.5 plugin are not carried over. Uninstall the old "HWiNFO" plugin and set up your tiles again.

## Actions

- **Sensor Reading**: one reading per key with graph and/or text, custom colors, fonts, number formats, unit normalization, EMA smoothing, and per-tile update intervals
- **Composite Dashboard**: 2–4 readings on one key with overlaid graphs
- **Derived Metric**: formulas (sum, average, max, min, delta, pct) across 2–8 readings, with savable presets
- **Dial Carousel**: readings on the Stream Deck+ touch strip, with rotate-to-cycle paging, multiple overview styles, and page indicators
- **Settings**: polling rate, a shared global threshold library, and live connection status

All sensor pickers support search, category filtering, and favorites shared across tiles. Thresholds (colors, alert text, hysteresis, dwell, cooldown, snooze, sticky alarms) can be defined per tile or globally by sensor type.

### Composite Dashboard

The Composite Dashboard action shows 2–4 readings on one key, each in its own slot. The tile chooses the render mode (text, graph, or both), the number of slots, a per-tile update interval, and smoothing; each slot then picks its own sensor and reading and can override the label, colors, scale, number format, and render mode. Every slot also carries its own threshold list with the full feature set described below, affecting only that slot's area. Slot graphs are blended so overlapping areas stay readable, with the text drawn on top.

### Derived Metric

The Derived Metric action combines 2–8 readings into one computed value: sum, average, max, min, delta (always 2 readings), or pct. Slots are filled from the sensor list or from favorites, and an "All sensors" selector fills every slot with the same sensor at once; each slot can divide or normalize its value before the formula runs. Complete setups can be saved as named presets and reloaded on any Derived Metric tile.

### Dial Carousel (Stream Deck+)

The Dial Carousel action turns one dial into a scrollable list of readings:

- Rotate the dial to cycle through the pages; a "Reverse dial" option flips the direction.
- Press the dial to switch between fullscreen and an overview of neighbouring pages, rendered as stacked strips or as carousel cards.
- Tap the touch strip to snooze an active alert or clear a sticky alarm, the same as pressing a key.

Pages can be added one at a time, or in bulk: **Bulk Add** creates many pages from one rule (all readings on a sensor, a numbered set such as all CPU cores, or the same reading across matching sensors), with a live preview and a name template. Each page has the full set of reading-tile settings, including its own thresholds. A threshold can be marked **Bring to front**: when it triggers, its page automatically becomes the active one. The page indicator (dots or a count) and the separator drawn between adjacent dials are configurable.

### Display options

Every graph tile can render text only, graph only, or both. The graph can be confined to the bottom part of the tile, the line thickness is adjustable, and the title and value can be drawn with an outline stroke to stay readable on top of the graph. Each tile can override the global poll interval and smooth the displayed value (EMA); thresholds always evaluate the raw value, so smoothing never delays or masks alerts.

### Title behavior

- By default the tile shows the reading label reported by HWiNFO inside the graph area.
- Entering text in the Title field replaces that label; Stream Deck stores the custom string per action.
- If you enable the **Show Title** checkbox in Stream Deck's title settings, the text renders outside the graph (the standard Stream Deck caption) while the graph can be left empty.
- Clearing the Title field while **Show Title** is enabled produces an empty caption, letting you hide the text entirely when you only want the graph.

### Threshold alerts

- Add as many thresholds as you want; each can be enabled or disabled independently.
- Each threshold defines a comparison operator and value (e.g. `>= 70`). Thresholds are evaluated top to bottom and the **last match wins**; use the arrow buttons to reorder them.
- Per-threshold colors: background, foreground, highlight, value text, and alert text. The optional alert text is shown under the value and supports `{value}` and `{unit}` placeholders.
- **Hysteresis**: the reading must clear the threshold by this amount before the alert deactivates, preventing rapid on/off flicker.
- **Dwell time**: the threshold must be exceeded for this many milliseconds before the alert activates.
- **Cooldown**: after an alert clears, it cannot trigger again until this many milliseconds have passed.
- **Sticky alerts**: once triggered, the alert stays active until cleared manually by pressing the key.

Thresholds can also be defined once in the Settings action's global library, optionally scoped to a sensor type (temperature, fan, usage, and so on). Global thresholds apply to every matching tile automatically and can be suppressed per tile.

#### Alert snooze

Press the key while an alert is active to step through the snooze presets: **5m**, **15m**, **1h**, and **Until resumed**. Snoozed tiles render in a muted state with a countdown; pressing past the last preset resumes normal alert behavior. On a dial, tapping the touch strip does the same for the active page.

### Settings

The Settings action provides plugin-wide configuration: the polling rate (250 ms to 10 s), the appearance of the settings tile itself, and the global threshold library. The tile shows the current polling interval, and its Property Inspector shows the live HWiNFO connection status.

## Enabling Support in HWiNFO64

> If the "portable" version of HWiNFO64 gives trouble with this plugin, use the installer version.

1. Download and install HWiNFO64, if you haven't already

    [HWiNFO Website](https://www.hwinfo.com)

2. Choose "Sensors-only" mode

    ![alt text](images/sensorsonly.png "HWiNFO64 Sensors Only")

3. Click "Settings"

    ![alt text](images/clicksettings.png "HWiNFO64 Click Settings")

4. Ensure "Shared Memory Support" is checked

    ![alt text](images/sharedmemory.png "HWiNFO64 Settings")

5. Click "OK" then, "Start"

## Install and Setup the Plugin

1. Download the latest pre-compiled plugin

    [Plugin Releases](../../releases)

    > Upgrading from v2.0.5? First uninstall the old plugin: within the Stream Deck app choose "More Actions..." (bottom-right), locate "HWiNFO" and choose "Uninstall". v3 installs as a new plugin, so old tiles are not migrated and need to be configured again.

2. Double-click to install the plugin

3. Choose "Install" when prompted by Stream Deck

4. Locate the actions under the "HWiNFO" category in the action list

5. Drag one of the actions from the list to a tile in the canvas area

    ![alt text](images/dragaction.gif "Drag Action")

6. Configure the action to display the sensor reading you wish

    ![alt text](images/configureaction.gif "Configure Action")

    > Screenshots show the v2 configuration screen; v3 adds a searchable sensor picker with categories and favorites, appearance controls, and thresholds in the same panel.

## Troubleshooting

- **Tiles show "HWiNFO Unavailable" or "Please Launch HWiNFO64"**: HWiNFO is not running, is not in Sensors-only mode, or Shared Memory Support is off. Walk through the HWiNFO setup above; if the plugin doesn't recover immediately, quit and reopen HWiNFO64.
- **Tiles stop updating after ~12 hours**: the free version of HWiNFO limits Shared Memory Support to 12 hours of continuous runtime. Restart HWiNFO (or re-enable the setting) to continue; a HWiNFO Pro license removes the limit. This is HWiNFO policy, not something the plugin can work around.
- **A tile shows no value after upgrading or after hardware changes**: sensor ids can change, and the plugin first tries to re-match the configured reading by its label automatically. If the tile stays empty, open its configuration and select the sensor and reading again.
- **Reporting a bug**: the plugin writes a log to `%APPDATA%\Elgato\StreamDeck\Plugins\com.moeilijk.hwinfo.sdPlugin\hwinfo.log`; the last lines are usually enough to pinpoint the problem. Please attach them to your issue.

## Building from source

Requires Go and a Windows C toolchain for the HWiNFO shared-memory bridge.

```sh
make plugin   # builds hwinfo.exe and hwinfo-bridge.exe into the .sdPlugin folder
make verify   # builds all targets, runs Go + Property Inspector tests, validates the manifest
make release  # packs build/com.moeilijk.hwinfo-X.Y.Z.streamDeckPlugin
```

### Architecture

The Stream Deck plugin (`hwinfo.exe`) talks to a sensor bridge (`hwinfo-bridge.exe`) over gRPC
([hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)); the bridge reads the HWiNFO shared memory.
`cmd/mock-bridge` provides controllable mock sensors for the automated integration tests in `tests/integration/`.

## History and credits

This plugin was created by [Shayne Sweeney](https://github.com/shayne), who built it as a closed-source passion project, open-sourced it once HWiNFO opened up the shared memory interface, and maintained it through v2.0.5. In 2026 he handed the project over to me ([moeilijk](https://github.com/moeilijk)). Thanks, Shayne.

## License

[GPL-3.0](LICENSE). The plugin was created by Shayne Sweeney and was originally unlicensed; he gave me permission to place it under GPL-3.0 when I took over maintenance in 2026.
