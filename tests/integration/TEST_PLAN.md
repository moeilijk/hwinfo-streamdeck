# Integration test plan

Each test deletes any leftover slots first, creates new tiles, tests the feature, then removes everything.

---

### test-global-thresholds.js — keys 0–3

**New tiles:** settings (0), reading temp (1), reading load (2), reading suppress (3)
- tiles set to CPU Package / CPU Total / CPU Package

**On:** add global threshold "HighTemp", ReadingType=Temp, operator=>, value=80, enabled=true

**Test (10 assertions):**
1a. Sensor at 45°C → no active threshold on temp tile
1b. Sensor raised to 90°C → global threshold active on temp tile
2.  Load at 90% → no active threshold on load tile (type filter)
3a. Suppress global on suppress tile, sensor at 90°C → suppress tile does not fire
3b. Temp tile (not suppressed) fires during same period
4.  Unsuppress suppress tile → fires again
5a. CPU Package reading received with raw type "Temperature"
5b. Mapped type "Temp" matches global readingType "Temp" — no "None match"
5c. CPU Total maps to "Usage", correctly excluded from Temp global
5d. All reading type strings normalize correctly (Voltage→Volt, Load→Usage, etc.)

**Off:** global threshold deleted, mock reset, all 4 tiles deleted

---

### test-per-tile-thresholds.js — keys 10–13

**New tiles:** settings (10), tile A CPU Package (11), tile B CPU Total (12), tile C CPU Package (13)
- tiles configured to respective readings

**On:** add per-tile threshold on tile A: "HighTemp", operator=>, value=80, dwellMs=1000, cooldownMs=5000

**Test (8 assertions):**
1a. Tile A at 45°C → no active threshold
1b. Tile A at 90°C → per-tile threshold active
2.  Tile B has no active threshold while tile A fires (isolation)
3.  smoothingAlpha=0.3 set on tile C → persisted
4.  updateIntervalOverrideMs=2000 set on tile C → persisted
5a. Override set to 5000ms; sensor at 90°C; at 2.5s mark → tile C not yet evaluated
5b. After 5s total → tile C evaluated and threshold fired
6.  graphHeightPct=60, graphLineThickness=3, textStroke=true, textStrokeColor=#ff0000 on tile C → all persisted

**Off:** updateIntervalOverrideMs reset to 0, mock reset, all 4 tiles deleted

---

### test-composite-thresholds.js — keys 20–21

**New tiles:** settings (20), composite (21)
- mock-bridge active (automatic on the Linux harness)
- Composite slot 0 = CPU Package (temp)

**On:** add threshold on composite slot 0: "SlotHighTemp", operator=>, value=80, dwellMs=1000, cooldownMs=5000

**Test (7 assertions):**
1a. Slot 0 at 45°C → not active
1b. Slot 0 at 90°C → slot 0 threshold active
2a. Slot 0 state confirmed (fires or in cooldown)
2b. Slot 1 configured (CPU Total), no threshold → slot 1 not active while slot 0 fires
3.  smoothingAlpha=0.2 on composite → persisted
4.  updateIntervalOverrideMs=3000 on composite → persisted
5.  slot0 mode Text only and slot1 mode Graph only → persisted

**Off:** mock reset, both tiles deleted

---

### test-derived-thresholds.js — keys 24–26

**New tiles:** settings (30), derived (31), reference reading (32)
- mock-bridge active (automatic on the Linux harness)
- Derived tile: slotCount=2, formula=average, slot 0=CPU Package, slot 1=CPU Core 0
- Reference reading tile: CPU Package

**On:** add global threshold "AllTypesHigh", readingType="" (all types), operator=>, value=80

**Test (7 assertions):**
1a. Sensors at 45°C/42°C (avg ~43.5°C) → derived tile not active
1b. Both sensors raised to 90°C → global threshold active on derived tile
1c. Global threshold also fires on reference reading tile
2a. Suppress global on derived tile; sensors at 90°C → derived tile does not fire
2b. Reference tile still fires while derived is suppressed
3.  Unsuppress derived tile; sensors still at 90°C → derived tile fires again
4.  smoothingAlpha=0.4, updateIntervalOverrideMs=2500, graphHeightPct=70, graphLineThickness=2, textStroke=true on derived tile → all persisted

**Off:** global threshold deleted, mock reset, all 3 tiles deleted

---

### test-composite-global-suppress.js — keys 14–16

**New tiles:** settings (40), composite (41), reference reading (42)
- mock-bridge active (automatic on the Linux harness)
- Composite slot 0 = CPU Package; reference reading tile = CPU Package

**On:** add global threshold "TempHigh", ReadingType=Temp, operator=>, value=80

**Test (5 assertions):**
1a. Sensor at 45°C → composite slot 0 not active
1b. Sensor at 90°C → global threshold active on composite slot 0
2a. Suppress global on composite slot 0; sensor at 90°C → slot 0 does not fire
2b. Reference reading tile (not suppressed) still fires
3.  Unsuppress composite slot 0; sensors still at 90°C → slot 0 fires again

**Off:** sensor reset to 45°C, global threshold deleted, mock reset, all 3 tiles deleted

---

### test-settings-tile.js — key 5

**New tile:** settings (50)
- mock-bridge active (automatic on the Linux harness)

**On:** settings tile connected with mock sensor server running

**Test (5 assertions):**
1a. connectionStatus = Connected
1b. currentRate is a positive number
1c. sourceProfiles is an array
2a. setPollInterval(2000) → currentRate = 2000
2b. pollInterval=2000 persisted to global settings file

**Off:** pollInterval reset to original value, mock reset, settings tile deleted

---

### test-favorites.js — keys 59–61

**New tiles:** settings (59), tile A reading (60), tile B reading (61)
- mock-bridge active (automatic on the Linux harness)
- Tile A configured: sensor /mockcpu/0, reading CPU Package
- Any pre-existing favorites removed

**On:** toggle favorite on tile A (CPU Package)

**Test (6 assertions):**
1a. 1 favorite saved after toggle
1b. Favorite id = /mockcpu/0|<readingId>
1c. Favorite readingLabel = CPU Package
2a. Open tile B → 1 favorite visible in catalog
2b. Apply favorite to tile B → sensorUid = /mockcpu/0
3.  Remove favorite → favorites list empty

**Off:** favorites list empty, mock reset, all 3 tiles deleted

---
