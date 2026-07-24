package hwinfostreamdeckplugin

import (
	"fmt"
	"sync"
	"time"
)

type actionManager struct {
	mux            sync.RWMutex
	actions        map[string]*actionData
	lastRun        map[string]time.Time
	updateInterval time.Duration
	intervalChan   chan time.Duration
}

func newActionManager(interval time.Duration) *actionManager {
	if interval < 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	if interval > 10*time.Second {
		interval = 10 * time.Second
	}
	return &actionManager{
		actions:        make(map[string]*actionData),
		lastRun:        make(map[string]time.Time),
		updateInterval: interval,
		intervalChan:   make(chan time.Duration, 1),
	}
}

func (tm *actionManager) Run(updateTiles func(*actionData), onTick func()) {
	go func() {
		ticker := time.NewTicker(tm.updateInterval)
		defer ticker.Stop()

		for {
			select {
			case newInterval := <-tm.intervalChan:
				ticker.Stop()
				tm.mux.Lock()
				tm.updateInterval = newInterval
				tm.mux.Unlock()
				ticker = time.NewTicker(newInterval)

			case <-ticker.C:
				now := time.Now()
				var toUpdate []*actionData
				tm.mux.Lock()
				for _, data := range tm.actions {
					if data.settings.IsValid {
						last := tm.lastRun[data.context]
						if data.settings.InErrorState && now.Sub(last) < 5*time.Second {
							continue
						}
						toUpdate = append(toUpdate, data)
					}
				}
				tm.mux.Unlock()

				for _, data := range toUpdate {
					updateTiles(data)
					tm.mux.Lock()
					tm.lastRun[data.context] = now
					tm.mux.Unlock()
				}
				if onTick != nil {
					onTick()
				}
			}
		}
	}()
}

func (tm *actionManager) SetAction(action, context string, settings *actionSettings) {
	tm.mux.Lock()
	tm.actions[context] = &actionData{action, context, settings}
	tm.mux.Unlock()
}

func (tm *actionManager) RemoveAction(context string) {
	tm.mux.Lock()
	delete(tm.actions, context)
	delete(tm.lastRun, context)
	tm.mux.Unlock()
}

func (tm *actionManager) getSettings(context string) (actionSettings, error) {
	tm.mux.RLock()
	data, ok := tm.actions[context]
	tm.mux.RUnlock()
	if !ok {
		return actionSettings{}, fmt.Errorf("getSettings invalid key: %s", context)
	}
	// return full copy of settings, not reference to stored settings
	return *data.settings, nil
}

// SetInterval dynamically updates the polling interval
func (tm *actionManager) SetInterval(d time.Duration) {
	if d < 250*time.Millisecond {
		d = 250 * time.Millisecond
	}
	if d > 10*time.Second {
		d = 10 * time.Second
	}

	// Update the cached interval immediately so GetInterval reflects the latest value.
	tm.mux.Lock()
	tm.updateInterval = d
	tm.mux.Unlock()

	select {
	case tm.intervalChan <- d:
	default:
		// Replace the queued value so the ticker switches to the newest interval.
		select {
		case <-tm.intervalChan:
		default:
		}
		tm.intervalChan <- d
	}
}

// AllActions returns a snapshot of all registered action data entries.
func (tm *actionManager) AllActions() []actionData {
	tm.mux.RLock()
	defer tm.mux.RUnlock()
	out := make([]actionData, 0, len(tm.actions))
	for _, d := range tm.actions {
		out = append(out, *d)
	}
	return out
}

// GetInterval returns the current polling interval
func (tm *actionManager) GetInterval() time.Duration {
	tm.mux.RLock()
	defer tm.mux.RUnlock()
	return tm.updateInterval
}
