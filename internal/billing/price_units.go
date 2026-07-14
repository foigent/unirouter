package billing

import (
	"math"
	"strconv"
	"strings"
)

const (
	// PublicPriceBasisTokens is the only token basis exposed by the V1 admin API
	// and UI. It matches the convention used by upstream model vendors.
	PublicPriceBasisTokens = 1_000_000

	// The database retains NUMERIC(18,6) CNY-cent-per-thousand-token source
	// prices. Public CNY-per-million prices therefore support five decimal
	// places: converting between the two exact decimal units shifts the decimal
	// point by one place and never requires binary floating point.
	publicPriceDecimalPlaces   = 5
	internalPriceDecimalPlaces = 6
	priceWholeDigits           = 13
	internalPriceWholeDigits   = 12
	cacheRatioDecimalPlaces    = 6
	orgMultiplierDecimalPlaces = 3
	// Public prices are scaled by 1e5 here and by another factor of ten when
	// converted to micro-CNY per million tokens.
	maxPublicPriceScaled = math.MaxInt64 / 10
)

// NormalizeCNYPerMillionPrice validates and canonicalizes an exact public
// CNY-per-million-token price. Scientific notation, signs and silent rounding
// are deliberately rejected.
func NormalizeCNYPerMillionPrice(value string) (string, bool) {
	canonical, scaled, ok := normalizeFixedDecimal(value, publicPriceDecimalPlaces, priceWholeDigits)
	if !ok || scaled > maxPublicPriceScaled {
		return "", false
	}
	return canonical, ok
}

// CNYPerMillionToInternalPriceCents converts exact public decimal text to the
// database's exact CNY-cent-per-thousand-token source unit.
func CNYPerMillionToInternalPriceCents(value string) (string, bool) {
	_, scaled, ok := normalizeFixedDecimal(value, publicPriceDecimalPlaces, priceWholeDigits)
	if !ok || scaled > maxPublicPriceScaled {
		return "", false
	}
	return formatScaledDecimal(scaled, internalPriceDecimalPlaces), true
}

// InternalPriceCentsToCNYPerMillion converts exact database decimal text to
// the canonical public CNY-per-million-token contract.
func InternalPriceCentsToCNYPerMillion(value string) (string, bool) {
	_, scaled, ok := normalizeFixedDecimal(value, internalPriceDecimalPlaces, internalPriceWholeDigits)
	if !ok || scaled > maxPublicPriceScaled {
		return "", false
	}
	return formatScaledDecimal(scaled, publicPriceDecimalPlaces), true
}

// NormalizeCachePriceRatio validates the NUMERIC(10,6) cache-price ratio
// contract without losing the supplied decimal to float64.
func NormalizeCachePriceRatio(value string) (string, bool) {
	canonical, scaled, ok := normalizeFixedDecimal(value, cacheRatioDecimalPlaces, 3)
	if !ok || scaled > 100_000_000 {
		return "", false
	}
	return canonical, true
}

// NormalizeOrgModelPriceMultiplier validates the NUMERIC(5,3) organization
// multiplier contract without binary floating point or database rounding.
func NormalizeOrgModelPriceMultiplier(value string) (string, bool) {
	canonical, scaled, ok := normalizeFixedDecimal(value, orgMultiplierDecimalPlaces, 2)
	if !ok || scaled < 1 || scaled > 99_999 {
		return "", false
	}
	return canonical, true
}

func normalizeFixedDecimal(value string, decimalPlaces, maxWholeDigits int) (string, int64, bool) {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 64 {
		return "", 0, false
	}
	whole, fraction, hasDot := strings.Cut(value, ".")
	if whole == "" || hasDot && fraction == "" || strings.Contains(fraction, ".") {
		return "", 0, false
	}
	for _, part := range []string{whole, fraction} {
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return "", 0, false
			}
		}
	}
	if len(fraction) > decimalPlaces {
		return "", 0, false
	}
	canonicalWhole := strings.TrimLeft(whole, "0")
	if canonicalWhole == "" {
		canonicalWhole = "0"
	}
	if len(canonicalWhole) > maxWholeDigits {
		return "", 0, false
	}
	wholeValue, err := strconv.ParseInt(canonicalWhole, 10, 64)
	if err != nil {
		return "", 0, false
	}
	scale := pow10(decimalPlaces)
	paddedFraction := fraction + strings.Repeat("0", decimalPlaces-len(fraction))
	var fractionValue int64
	if paddedFraction != "" {
		fractionValue, err = strconv.ParseInt(paddedFraction, 10, 64)
		if err != nil {
			return "", 0, false
		}
	}
	if wholeValue > (math.MaxInt64-fractionValue)/scale {
		return "", 0, false
	}
	scaled := wholeValue*scale + fractionValue
	return formatScaledDecimal(scaled, decimalPlaces), scaled, true
}

func formatScaledDecimal(value int64, decimalPlaces int) string {
	if decimalPlaces == 0 {
		return strconv.FormatInt(value, 10)
	}
	scale := pow10(decimalPlaces)
	whole := value / scale
	fraction := value % scale
	if fraction == 0 {
		return strconv.FormatInt(whole, 10)
	}
	fractionText := strconv.FormatInt(fraction+scale, 10)[1:]
	fractionText = strings.TrimRight(fractionText, "0")
	return strconv.FormatInt(whole, 10) + "." + fractionText
}

func pow10(decimalPlaces int) int64 {
	value := int64(1)
	for i := 0; i < decimalPlaces; i++ {
		value *= 10
	}
	return value
}
