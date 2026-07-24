# HWiNFO Stream Deck Plugin

> This project has been adopted and is actively maintained again at [moeilijk/hwinfo-streamdeck](https://github.com/moeilijk/hwinfo-streamdeck). Thanks to Shayne Sweeney for building it and handing it over.

## ⚠⚠ v3.0 — major update ⚠⚠

The plugin core has been replaced with the actively developed engine from the [lhm-streamdeck](https://github.com/moeilijk/lhm-streamdeck) sister project. Existing HWiNFO tiles from v2.0.5 keep working after upgrading — the reading action and its settings are fully backwards compatible.

This plugin reads **HWiNFO exclusively** (Windows). For Libre Hardware Monitor, remote machines, or Linux, use the [lhm-streamdeck](https://github.com/moeilijk/lhm-streamdeck) plugin instead.

New in 3.0:

- **Sensor Reading** (upgraded) — graph and/or text, custom colors, fonts, formats, unit normalization, EMA smoothing, and per-tile update intervals
- **Composite Dashboard** — 2–4 readings on one key with overlaid graphs
- **Derived Metric** — formulas (sum, average, max, min, delta, pct) across 2–8 readings, with savable presets
- **Dial Carousel** — readings on the Stream Deck+ touch strip: rotate to cycle pages, multiple overview styles, page indicators
- **Settings action** — polling rate, default tile appearance, and a shared global threshold library
- **Sensor picker** with search, category filtering, and favorites shared across all tiles
- **Dynamic thresholds** — colors, alert text, hysteresis, dwell, cooldown, snooze, and sticky alarms; define globally by sensor type or per tile

---

>## Thank you & Looking for Maintainers
>
>Thank you everyone who has used and enjoyed this plugin. It started as a passion project and I continue to use it day to day. I am happy to finally release the full source on GitHub. When I first built it, it was closed under agreement with the HWiNFO64 project. They have since opened up the shared memory interface and now the plugin is freely open.
>
>I haven't had the time to dedicate to this project in some time and appreciate everyone for hanging in there. I hope to work with some of you who are eager to take the project over. I am happy and ready to hand over the reigns. If there are development questions I'm happy to share my thoughts on the code and structure that exists.
>
>*-Shayne*

---

![alt text](images/demo.gif "HWiNFO64 Stream Deck Plugin Demo")

> NOTICE: HWiNFO64 must be run in Sensors-only mode for the plugin to work. 

## Enabling Support in HWiNFO64

> NOTICE: It has been reported that running the "portable" version of HWiNFO64 doesn't work with this plugin. The recommendation is to run the version with the installer until I can figure out the issue.

1. Download and install HWiNFO64, if you haven't already

    [HWiNFO Website](https://www.hwinfo.com)

2. Choose "Sensors-only" mode

    ![alt text](images/sensorsonly.png "HWiNFO64 Sensors Only")

3. Click "Settings"

    ![alt text](images/clicksettings.png "HWiNFO64 Click Settings")

4. Ensure "Shared Memory Support" is checked

    ![alt text](images/sharedmemory.png "HWiNFO64 Settings")

5. (Optional) Recommended launch settings

    ![alt text](images/recommendedsettings.png "Quit HWiNFO64")

6. Click "OK" then, "Run"

    > If the plugin doesn't work immediately, you may have to quit and reopen HWiNFO64.
    >
    > From the system tray:
    >
    > ![alt text](images/contextquit.png "Quit HWiNFO64")


## Install and Setup the Plugin

1. Download the latest pre-compiled plugin

    [Plugin Releases](../../releases)

    > When upgrading, first uninstall: within the Stream Deck app choose "More Actions..." (bottom-right), locate "HWiNFO" and choose "Uninstall". Your tiles and settings will be preserved.

2. Double-click to install the plugin

3. Choose "Install" when prompted by Stream Deck

    ![alt text](images/streamdeckinstall.png "Stream Deck Plugin Installation")

4. Locate "HWiNFO" under "Custom" in the action list

    ![alt text](images/streamdeckactionlist.png "Stream Deck Action List")

5. Drag one of the "HWiNFO" actions from the list to a tile in the canvas area

    ![alt text](images/dragaction.gif "Drag Action")

6. Configure the action to display the sensor reading you wish

    ![alt text](images/configureaction.gif "Configure Action")

    > Screenshots show the v2 configuration screen; v3 adds a searchable sensor picker with categories and favorites, appearance controls, and thresholds in the same panel.

## Sensor source

The plugin reads the local **HWiNFO64 shared memory** — no further configuration needed beyond the HWiNFO setup above. For Libre Hardware Monitor or remote/Linux machines, use the [lhm-streamdeck](https://github.com/moeilijk/lhm-streamdeck) plugin instead.

## Building from source

Requires Go, and for the HWiNFO shared-memory bridge a Windows C toolchain (on Linux/WSL: `mingw-w64`).

```sh
make plugin   # builds hwinfo.exe and hwinfo-bridge.exe into the .sdPlugin folder
make verify   # builds all targets, runs Go + Property Inspector tests, validates the manifest
make release  # packs build/com.exension.hwinfo.streamDeckPlugin
```

### Architecture

The Stream Deck plugin (`hwinfo.exe`) talks to a sensor bridge (`hwinfo-bridge.exe`) over gRPC
([hashicorp/go-plugin](https://github.com/hashicorp/go-plugin)); the bridge reads the HWiNFO shared memory.
For development without Windows/HWiNFO there is `cmd/mock-bridge`: the same gRPC interface with controllable
mock sensors (HTTP control API on `:9999`), used by the integration suites in `tests/integration/`
(`scripts/run-integration-tests.sh`, runs against a [DeckBridge](https://github.com/moeilijk/DeckBridge) emulator on Linux/WSL).
