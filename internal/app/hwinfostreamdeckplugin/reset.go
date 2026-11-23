package hwinfostreamdeckplugin

import (
	"log"
	"time"
)

// keyPressResetsMinMax reports whether a key press resets the sensor's MIN/MAX.
// An active alert keeps the press for snoozing or clearing that alert.
func keyPressResetsMinMax(s *actionSettings) bool {
	return s.ResetMinMaxOnPress && s.CurrentThresholdID == ""
}

// takePendingScaleReset reports whether the tile's history must be cleared now.
// A reset is pending from the moment the source confirmed it until the first
// sample polled after that moment, so the cleared graph starts with fresh data.
func (p *Plugin) takePendingScaleReset(context string, pollTime uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	resetAt, pending := p.scaleResetAfter[context]
	if !pending || pollTime <= resetAt {
		return false
	}
	delete(p.scaleResetAfter, context)
	delete(p.smoothedValues, context)
	return true
}

// resetReadingScale asks the sensor source to reset the MIN/MAX of the tile's
// reading. What the source can do is the product-specific part: see
// sourceResetSupported, resetSourceMinMax and sameResetTarget in
// reset_source.go.
func (p *Plugin) resetReadingScale(context string, settings actionSettings) {
	if !p.sourceResetSupported() {
		return
	}
	p.mu.Lock()
	if p.scaleResetBusy[context] {
		p.mu.Unlock()
		return
	}
	if p.scaleResetBusy == nil {
		p.scaleResetBusy = make(map[string]bool)
	}
	p.scaleResetBusy[context] = true
	p.mu.Unlock()
	go func() {
		defer func() { p.mu.Lock(); delete(p.scaleResetBusy, context); p.mu.Unlock() }()
		if err := p.resetSourceMinMax(settings); err != nil {
			log.Printf("Reset MIN/MAX for %s: %v", settings.ReadingLabel, err)
			return
		}
		current, err := p.am.getSettings(context)
		if err != nil || !sameResetTarget(&current, &settings) {
			return
		}
		p.mu.Lock()
		if p.scaleResetAfter == nil {
			p.scaleResetAfter = make(map[string]uint64)
		}
		p.scaleResetAfter[context] = uint64(time.Now().UnixNano())
		p.mu.Unlock()
		log.Printf("Reset MIN/MAX for %s", settings.ReadingLabel)
	}()
}
