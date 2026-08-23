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
// every UTC weekday. An empty weekdays_utc applies to all seven days; a
// cross-midnight interval must be supplied as two bands.
func ValidateDailyPriceBands(bands []DailyPriceBand) error {
	if len(bands) == 0 {
		return nil
	}
	if len(bands) > 64 {
		return invalid("daily_price_band_count")
	}
	ordered := canonicalDailyPriceBands(bands)
	seenRefs := make(map[string]struct{}, len(ordered))
	for _, band := range bands {
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
		seenWeekdays := make(map[int]struct{}, len(band.WeekdaysUTC))
		for _, weekday := range band.WeekdaysUTC {
			if weekday < 0 || weekday > 6 {
				return invalid("daily_price_band_weekday")
			}
			if _, exists := seenWeekdays[weekday]; exists {
				return invalid("duplicate_daily_price_band_weekday")
			}
			seenWeekdays[weekday] = struct{}{}
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
	}
	for weekday := 0; weekday < 7; weekday++ {
		applicable := make([]DailyPriceBand, 0, len(ordered))
		for _, band := range ordered {
			if dailyPriceBandAppliesToWeekday(band, weekday) {
				applicable = append(applicable, band)
			}
		}
		sort.Slice(applicable, func(left, right int) bool {
			if applicable[left].StartMinuteUTC != applicable[right].StartMinuteUTC {
				return applicable[left].StartMinuteUTC < applicable[right].StartMinuteUTC
			}
			if applicable[left].EndMinuteUTC != applicable[right].EndMinuteUTC {
				return applicable[left].EndMinuteUTC < applicable[right].EndMinuteUTC
			}
			return applicable[left].BandRef < applicable[right].BandRef
		})
		expectedStart := 0
		for _, band := range applicable {
			if band.StartMinuteUTC < expectedStart {
				return invalid("daily_price_band_overlap")
			}
			if band.StartMinuteUTC > expectedStart {
				return invalid("daily_price_band_gap")
			}
			expectedStart = band.EndMinuteUTC
		}
		if expectedStart != minutesPerUTCDay {
			return invalid("daily_price_band_gap")
		}
	}
	return nil
}

func canonicalDailyPriceBands(bands []DailyPriceBand) []DailyPriceBand {
	ordered := make([]DailyPriceBand, len(bands))
	for i, band := range bands {
		ordered[i] = band
		ordered[i].BandRef = strings.TrimSpace(band.BandRef)
		ordered[i].WeekdaysUTC = canonicalWeekdaysUTC(band.WeekdaysUTC)
		ordered[i].Components = append([]RateComponent(nil), band.Components...)
		sort.Slice(ordered[i].Components, func(left, right int) bool {
			return ordered[i].Components[left].Meter < ordered[i].Components[right].Meter
		})
	}
	sort.Slice(ordered, func(left, right int) bool {
		if compared := compareWeekdaysUTC(ordered[left].WeekdaysUTC, ordered[right].WeekdaysUTC); compared != 0 {
			return compared < 0
		}
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

func canonicalWeekdaysUTC(weekdays []int) []int {
	if len(weekdays) == 0 {
		return nil
	}
	canonical := append([]int(nil), weekdays...)
	sort.Ints(canonical)
	return canonical
}

func compareWeekdaysUTC(left, right []int) int {
	limit := len(left)
	if len(right) < limit {
		limit = len(right)
	}
	for index := 0; index < limit; index++ {
		if left[index] < right[index] {
			return -1
		}
		if left[index] > right[index] {
			return 1
		}
	}
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return 0
}

func dailyPriceBandAppliesToWeekday(band DailyPriceBand, weekday int) bool {
	if len(band.WeekdaysUTC) == 0 {
		return true
	}
	for _, candidate := range band.WeekdaysUTC {
		if candidate == weekday {
			return true
		}
	}
	return false
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
	weekday := int(pricedAt.UTC().Weekday())
	for _, band := range canonicalDailyPriceBands(bands) {
		if dailyPriceBandAppliesToWeekday(band, weekday) && minute >= band.StartMinuteUTC && minute < band.EndMinuteUTC {
			return band, true, nil
		}
	}
	return DailyPriceBand{}, false, invalid("daily_price_band_selection")
}
