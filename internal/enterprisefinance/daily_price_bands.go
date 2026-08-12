package enterprisefinance

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const minutesPerUTCDay = 24 * 60

// ValidateDailyPriceBands rejects schedules that would require a pricing
// guess. A non-empty schedule must be an exact, non-overlapping partition of
// one UTC day; a cross-midnight interval must be supplied as two bands.
func ValidateDailyPriceBands(bands []DailyPriceBand) error {
	if len(bands) == 0 {
		return nil
	}
	if len(bands) > 64 {
		return invalid("daily_price_band_count")
	}
	ordered := canonicalDailyPriceBands(bands)
	seenRefs := make(map[string]struct{}, len(ordered))
	expectedStart := 0
	for _, band := range ordered {
		bandRef := strings.TrimSpace(band.BandRef)
		if !meterPattern.MatchString(bandRef) {
			return invalid("daily_price_band_ref")
		}
		if _, exists := seenRefs[bandRef]; exists {
			return invalid("duplicate_daily_price_band_ref")
		}
		seenRefs[bandRef] = struct{}{}
		if band.StartMinuteUTC < 0 || band.StartMinuteUTC >= minutesPerUTCDay ||
			band.EndMinuteUTC <= 0 || band.EndMinuteUTC > minutesPerUTCDay ||
			band.StartMinuteUTC >= band.EndMinuteUTC {
			return invalid("daily_price_band_window")
		}
		if band.StartMinuteUTC < expectedStart {
			return invalid("daily_price_band_overlap")
		}
		if band.StartMinuteUTC > expectedStart {
			return invalid("daily_price_band_gap")
		}
		if len(band.Components) == 0 {
			return invalid("daily_price_band_components")
		}
		seenMeters := make(map[string]struct{}, len(band.Components))
		for _, component := range band.Components {
			if !meterPattern.MatchString(component.Meter) || !isPositiveInteger(component.UnitQuantity) {
				return invalid("daily_price_band_component")
			}
			if err := validateNonNegativeAmount(component.Amount); err != nil {
				return err
			}
			if _, exists := seenMeters[component.Meter]; exists {
				return invalid("duplicate_daily_price_band_meter")
			}
			seenMeters[component.Meter] = struct{}{}
		}
		expectedStart = band.EndMinuteUTC
	}
	if expectedStart != minutesPerUTCDay {
		return invalid("daily_price_band_gap")
	}
	return nil
}

func canonicalDailyPriceBands(bands []DailyPriceBand) []DailyPriceBand {
	ordered := make([]DailyPriceBand, len(bands))
	for i, band := range bands {
		ordered[i] = band
		ordered[i].BandRef = strings.TrimSpace(band.BandRef)
		ordered[i].Components = append([]RateComponent(nil), band.Components...)
		sort.Slice(ordered[i].Components, func(left, right int) bool {
			return ordered[i].Components[left].Meter < ordered[i].Components[right].Meter
		})
	}
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].StartMinuteUTC != ordered[right].StartMinuteUTC {
			return ordered[left].StartMinuteUTC < ordered[right].StartMinuteUTC
		}
		if ordered[left].EndMinuteUTC != ordered[right].EndMinuteUTC {
			return ordered[left].EndMinuteUTC < ordered[right].EndMinuteUTC
		}
		return ordered[left].BandRef < ordered[right].BandRef
	})
	return ordered
}

func marshalDailyPriceBands(bands []DailyPriceBand) ([]byte, error) {
	wire, err := json.Marshal(canonicalDailyPriceBands(bands))
	if err != nil {
		return nil, fmt.Errorf("marshal daily price bands: %w", err)
	}
	return wire, nil
}

func unmarshalDailyPriceBands(wire []byte) ([]DailyPriceBand, error) {
	if len(wire) == 0 {
		return []DailyPriceBand{}, nil
	}
	var bands []DailyPriceBand
	if err := json.Unmarshal(wire, &bands); err != nil {
		return nil, fmt.Errorf("unmarshal daily price bands: %w", err)
	}
	if bands == nil {
		bands = []DailyPriceBand{}
	}
	return canonicalDailyPriceBands(bands), nil
}

func selectDailyPriceBand(bands []DailyPriceBand, pricedAt time.Time) (DailyPriceBand, bool, error) {
	if len(bands) == 0 {
		return DailyPriceBand{}, false, nil
	}
	if err := ValidateDailyPriceBands(bands); err != nil {
		return DailyPriceBand{}, false, err
	}
	minute := pricedAt.UTC().Hour()*60 + pricedAt.UTC().Minute()
	for _, band := range canonicalDailyPriceBands(bands) {
		if minute >= band.StartMinuteUTC && minute < band.EndMinuteUTC {
			return band, true, nil
		}
	}
	return DailyPriceBand{}, false, invalid("daily_price_band_selection")
}
