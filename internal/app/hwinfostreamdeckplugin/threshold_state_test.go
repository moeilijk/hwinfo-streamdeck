package hwinfostreamdeckplugin

import (
	"testing"
	"time"
)

func TestEvaluateThresholdStateDwell(t *testing.T) {
	threshold := &Threshold{
		Enabled:  true,
		Operator: ">=",
		Value:    70,
		DwellMs:  2000,
	}
	state := &thresholdRuntimeState{}
	now := time.Unix(100, 0)

	if active := evaluateThresholdState(75, threshold, state, now); active {
		t.Fatalf("expected threshold to remain pending before dwell completes")
	}
	if state.PendingSince.IsZero() {
		t.Fatalf("expected pending state to start on first match")
	}

	if active := evaluateThresholdState(75, threshold, state, now.Add(1500*time.Millisecond)); active {
		t.Fatalf("expected threshold to remain pending before dwell duration")
	}

	if active := evaluateThresholdState(75, threshold, state, now.Add(2*time.Second)); !active {
		t.Fatalf("expected threshold to activate after dwell duration")
	}
	if !state.Active {
		t.Fatalf("expected state to be active after dwell duration")
	}
}

func TestEvaluateThresholdStateHysteresis(t *testing.T) {
	threshold := &Threshold{
		Enabled:    true,
		Operator:   ">=",
		Value:      70,
		Hysteresis: 5,
	}
	state := &thresholdRuntimeState{}
	now := time.Unix(200, 0)

	if active := evaluateThresholdState(75, threshold, state, now); !active {
		t.Fatalf("expected threshold to activate immediately")
	}
	if active := evaluateThresholdState(68, threshold, state, now.Add(time.Second)); !active {
		t.Fatalf("expected threshold to remain active inside hysteresis window")
	}
	if active := evaluateThresholdState(64, threshold, state, now.Add(2*time.Second)); active {
		t.Fatalf("expected threshold to clear once value crosses hysteresis boundary")
	}
	if state.Active {
		t.Fatalf("expected threshold runtime state to be inactive after clear")
	}
}

func TestEvaluateThresholdStateCooldown(t *testing.T) {
	threshold := &Threshold{
		Enabled:    true,
		Operator:   ">=",
		Value:      70,
		CooldownMs: 5000,
	}
	state := &thresholdRuntimeState{}
	now := time.Unix(300, 0)

	if active := evaluateThresholdState(75, threshold, state, now); !active {
		t.Fatalf("expected threshold to activate immediately")
	}
	if active := evaluateThresholdState(60, threshold, state, now.Add(time.Second)); active {
		t.Fatalf("expected threshold to clear when value drops below threshold")
	}
	if state.CooldownUntil.IsZero() {
		t.Fatalf("expected cooldown timer to be set after clear")
	}
	if active := evaluateThresholdState(80, threshold, state, now.Add(2*time.Second)); active {
		t.Fatalf("expected threshold to remain inactive during cooldown")
	}
	if active := evaluateThresholdState(80, threshold, state, now.Add(6*time.Second)); !active {
		t.Fatalf("expected threshold to reactivate after cooldown expires")
	}
}

func TestEvaluateThresholdStateStickyLatch(t *testing.T) {
	threshold := &Threshold{
		Enabled:  true,
		Operator: ">=",
		Value:    70,
		Sticky:   true,
	}
	state := &thresholdRuntimeState{}
	now := time.Unix(400, 0)

	if active := evaluateThresholdState(75, threshold, state, now); !active {
		t.Fatalf("expected threshold to activate immediately")
	}
	if !state.Latched {
		t.Fatalf("expected sticky threshold to latch when activated")
	}
	if active := evaluateThresholdState(60, threshold, state, now.Add(time.Second)); !active {
		t.Fatalf("expected sticky threshold to stay active until manually cleared")
	}
	if !state.Latched {
		t.Fatalf("expected latch to remain set until manual clear")
	}
}

func TestClearStickyThresholdStateSuppressesUntilClear(t *testing.T) {
	threshold := &Threshold{
		Enabled:    true,
		Operator:   ">=",
		Value:      70,
		Hysteresis: 5,
		Sticky:     true,
	}
	state := &thresholdRuntimeState{}
	now := time.Unix(500, 0)

	if active := evaluateThresholdState(75, threshold, state, now); !active {
		t.Fatalf("expected sticky threshold to activate immediately")
	}
	state.LatchedDisplayText = "75 C"
	state.LatchedAlertText = "HOT"

	if !clearStickyThresholdState(state) {
		t.Fatalf("expected manual clear to succeed for a latched threshold")
	}
	if state.Latched || state.Active {
		t.Fatalf("expected manual clear to remove active sticky state")
	}
	if !state.SuppressedUntilClear {
		t.Fatalf("expected manual clear to suppress retrigger until the threshold clears")
	}
	if state.LatchedDisplayText != "" || state.LatchedAlertText != "" {
		t.Fatalf("expected manual clear to drop latched display snapshot")
	}

	if active := evaluateThresholdState(72, threshold, state, now.Add(time.Second)); active {
		t.Fatalf("expected threshold to stay suppressed while still above the clear boundary")
	}
	if !state.SuppressedUntilClear {
		t.Fatalf("expected suppression to remain until the hysteresis clear boundary is crossed")
	}

	if active := evaluateThresholdState(64, threshold, state, now.Add(2*time.Second)); active {
		t.Fatalf("expected threshold to stay inactive once the clear boundary is crossed")
	}
	if state.SuppressedUntilClear {
		t.Fatalf("expected suppression to lift after the value returns to normal")
	}

	if active := evaluateThresholdState(75, threshold, state, now.Add(3*time.Second)); !active {
		t.Fatalf("expected threshold to be able to latch again after recovering")
	}
	if !state.Latched {
		t.Fatalf("expected sticky threshold to latch again after recovery")
	}
}

func TestStickySnapshotShouldUpdate(t *testing.T) {
	tests := []struct {
		name          string
		threshold     Threshold
		currentValue  float64
		snapshotValue float64
		expectUpdate  bool
	}{
		{
			name:          "greater than keeps highest value",
			threshold:     Threshold{Operator: ">=", Value: 70},
			currentValue:  82,
			snapshotValue: 75,
			expectUpdate:  true,
		},
		{
			name:          "greater than ignores lower value",
			threshold:     Threshold{Operator: ">=", Value: 70},
			currentValue:  72,
			snapshotValue: 75,
			expectUpdate:  false,
		},
		{
			name:          "less than keeps lowest value",
			threshold:     Threshold{Operator: "<=", Value: 30},
			currentValue:  24,
			snapshotValue: 26,
			expectUpdate:  true,
		},
		{
			name:          "less than ignores higher value",
			threshold:     Threshold{Operator: "<=", Value: 30},
			currentValue:  28,
			snapshotValue: 26,
			expectUpdate:  false,
		},
	}

	for _, tc := range tests {
		if got := stickySnapshotShouldUpdate(&tc.threshold, tc.currentValue, tc.snapshotValue); got != tc.expectUpdate {
			t.Fatalf("%s: expected update=%t, got %t", tc.name, tc.expectUpdate, got)
		}
	}
}

func TestSetThresholdSnoozeExpires(t *testing.T) {
	p := &Plugin{
		thresholdDirty: make(map[string]bool),
	}
	now := time.Unix(700, 0)

	p.setThresholdSnooze("ctx-a", 5*time.Minute, now)

	if _, ok := p.currentThresholdSnooze("ctx-a", now.Add(4*time.Minute)); !ok {
		t.Fatalf("expected timed snooze to remain active before expiry")
	}
	if _, ok := p.currentThresholdSnooze("ctx-a", now.Add(6*time.Minute)); ok {
		t.Fatalf("expected timed snooze to expire after its duration")
	}
}

func TestSetThresholdSnoozeIndefiniteUntilCleared(t *testing.T) {
	p := &Plugin{
		thresholdDirty: make(map[string]bool),
	}
	now := time.Unix(800, 0)

	p.setThresholdSnooze("ctx-a", 0, now)

	if _, ok := p.currentThresholdSnooze("ctx-a", now.Add(24*time.Hour)); !ok {
		t.Fatalf("expected indefinite snooze to remain active until cleared")
	}
	if !p.clearThresholdSnooze("ctx-a") {
		t.Fatalf("expected manual clear to remove an active snooze")
	}
	if _, ok := p.currentThresholdSnooze("ctx-a", now.Add(24*time.Hour)); ok {
		t.Fatalf("expected cleared snooze to be inactive")
	}
}

func TestThresholdSnoozeIsTileScoped(t *testing.T) {
	p := &Plugin{
		thresholdDirty: make(map[string]bool),
	}
	now := time.Unix(900, 0)

	p.setThresholdSnooze("ctx-a", 5*time.Minute, now)
	p.setThresholdSnooze("ctx-b", 15*time.Minute, now)

	snoozeA, okA := p.currentThresholdSnooze("ctx-a", now.Add(time.Minute))
	snoozeB, okB := p.currentThresholdSnooze("ctx-b", now.Add(time.Minute))
	if !okA || !okB {
		t.Fatalf("expected snoozes to be tracked independently per tile")
	}
	if snoozeA.Duration != 5*time.Minute {
		t.Fatalf("expected ctx-a snooze duration to remain 5m, got %v", snoozeA.Duration)
	}
	if snoozeB.Duration != 15*time.Minute {
		t.Fatalf("expected ctx-b snooze duration to remain 15m, got %v", snoozeB.Duration)
	}
}

func TestNormalizeThresholdSnoozeDurations(t *testing.T) {
	got := normalizeThresholdSnoozeDurations([]int{
		900000,
		300000,
		12345,
		0,
		3600000,
		900000,
	})
	want := []int{300000, 900000, 3600000, 0}

	if len(got) != len(want) {
		t.Fatalf("expected %d snooze durations, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected snooze duration %d at index %d, got %d", want[i], i, got[i])
		}
	}
}

func TestNextThresholdSnoozeDuration(t *testing.T) {
	configured := normalizeThresholdSnoozeDurations([]int{0, 3600000, 900000, 300000})

	next, ok := nextThresholdSnoozeDuration(configured, nil)
	if !ok || next != 5*time.Minute {
		t.Fatalf("expected first snooze duration to be 5m, got %v ok=%t", next, ok)
	}

	next, ok = nextThresholdSnoozeDuration(configured, &thresholdSnoozeState{Duration: 5 * time.Minute})
	if !ok || next != 15*time.Minute {
		t.Fatalf("expected second snooze duration to be 15m, got %v ok=%t", next, ok)
	}

	next, ok = nextThresholdSnoozeDuration(configured, &thresholdSnoozeState{Duration: 15 * time.Minute})
	if !ok || next != time.Hour {
		t.Fatalf("expected third snooze duration to be 1h, got %v ok=%t", next, ok)
	}

	next, ok = nextThresholdSnoozeDuration(configured, &thresholdSnoozeState{Duration: time.Hour})
	if !ok || next != 0 {
		t.Fatalf("expected final snooze duration to be manual resume, got %v ok=%t", next, ok)
	}

	if next, ok = nextThresholdSnoozeDuration(configured, &thresholdSnoozeState{Duration: 0}); ok {
		t.Fatalf("expected no next snooze duration after manual resume preset, got %v", next)
	}
}

func TestAdvanceConfiguredThresholdSnoozeCanClearExistingSnooze(t *testing.T) {
	p := &Plugin{
		thresholdDirty: make(map[string]bool),
	}
	now := time.Unix(950, 0)
	configured := []int{0}

	if !p.advanceConfiguredThresholdSnooze("ctx-a", configured, now) {
		t.Fatalf("expected first advance to set the configured snooze")
	}
	if _, ok := p.currentThresholdSnooze("ctx-a", now); !ok {
		t.Fatalf("expected snooze to be active after first advance")
	}
	if !p.advanceConfiguredThresholdSnooze("ctx-a", configured, now.Add(time.Second)) {
		t.Fatalf("expected second advance to clear the existing snooze")
	}
	if _, ok := p.currentThresholdSnooze("ctx-a", now.Add(time.Second)); ok {
		t.Fatalf("expected snooze to be cleared after cycling past final preset")
	}
}

func TestThresholdSnoozeText(t *testing.T) {
	now := time.Unix(1000, 0)

	if got := thresholdSnoozeText(thresholdSnoozeState{}, now); got != "Snoozed" {
		t.Fatalf("expected indefinite snooze text, got %q", got)
	}

	state := thresholdSnoozeState{
		Duration: 5 * time.Minute,
		SetAt:    now,
		Until:    now.Add(5 * time.Minute),
	}
	if got := thresholdSnoozeText(state, now.Add(61*time.Second)); got != "Snoozed\n3:59" {
		t.Fatalf("expected countdown text, got %q", got)
	}
}
