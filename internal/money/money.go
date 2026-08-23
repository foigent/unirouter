// Package money contains the only arithmetic primitives allowed to produce
// customer ledger amounts. Canonical amounts are signed micro-CNY integers;
// prices are micro-CNY per one million tokens.
package money

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
)

const (
	MicrosPerCNY       int64 = 1_000_000
	MicrosPerCent      int64 = 10_000
	PriceBasisTokens   int64 = 1_000_000
	internalPriceScale int64 = 10_000_000
)

var (
	ErrInvalidDecimal = errors.New("invalid exact decimal")
	ErrNegativeValue  = errors.New("money value must be nonnegative")
	ErrOverflow       = errors.New("money value overflows int64")
)

// Component is one token class in an authoritative customer charge. Order is
// significant: it is the stable tie-breaker when fractional remainders match.
type Component struct {
	Tokens                int64
	PriceMicrosPerMillion int64
}

// UnitComponent prices a non-token meter with the exact Rate Card rational.
// AmountMicros is the CNY-micro amount for UnitQuantity meter units. Keeping
// the denominator until the aggregate calculation avoids per-call rounding
// drift for prices such as 3 CNY per 100 calls.
type UnitComponent struct {
	Quantity     int64
	AmountMicros int64
	UnitQuantity int64
}

// Calculation is an exactly conserved terminal charge. ComponentMicros has
// the same order as the input components and always sums to TotalMicros.
type Calculation struct {
	TotalMicros     int64
	ComponentMicros []int64
}

// CentsToMicros converts an explicitly cents-denominated configuration value
// at a checked system boundary.
func CentsToMicros(cents int64) (int64, error) {
	if cents > math.MaxInt64/MicrosPerCent || cents < math.MinInt64/MicrosPerCent {
		return 0, ErrOverflow
	}
	return cents * MicrosPerCent, nil
}

// DecimalCentsToMicros converts legacy NUMERIC/JSON cent evidence into the
// canonical integer unit with one half-up rounding at the micro-CNY boundary.
func DecimalCentsToMicros(cents string) (int64, error) {
	value, err := ParseDecimal(cents)
	if err != nil {
		return 0, fmt.Errorf("cent amount: %w", err)
	}
	if value.Sign() < 0 {
		return 0, ErrNegativeValue
	}
	value.Mul(value, new(big.Rat).SetInt64(MicrosPerCent))
	return roundHalfUpNonnegative(value)
}

// Add returns a checked signed ledger sum.
func Add(left, right int64) (int64, error) {
	if (right > 0 && left > math.MaxInt64-right) || (right < 0 && left < math.MinInt64-right) {
		return 0, ErrOverflow
	}
	return left + right, nil
}

// Sub returns a checked signed ledger difference.
func Sub(left, right int64) (int64, error) {
	if right == math.MinInt64 {
		if left >= 0 {
			return 0, ErrOverflow
		}
		return left - right, nil
	}
	return Add(left, -right)
}

// MicrosToCentsExact converts only losslessly representable legacy values.
func MicrosToCentsExact(micros int64) (int64, error) {
	if micros%MicrosPerCent != 0 {
		return 0, fmt.Errorf("micro-CNY amount %d is not exactly representable in cents", micros)
	}
	return micros / MicrosPerCent, nil
}

// ParseDecimal parses a base-10 decimal without binary floating point. It
// deliberately rejects exponent notation so configuration text is canonical.
func ParseDecimal(value string) (*big.Rat, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return nil, ErrInvalidDecimal
	}
	start := 0
	if value[0] == '+' || value[0] == '-' {
		start = 1
		if len(value) == 1 {
			return nil, ErrInvalidDecimal
		}
	}
	dot := -1
	digits := 0
	for i := start; i < len(value); i++ {
		switch {
		case value[i] == '.' && dot < 0:
			dot = i
		case value[i] >= '0' && value[i] <= '9':
			digits++
		default:
			return nil, ErrInvalidDecimal
		}
	}
	if digits == 0 || dot == start && dot == len(value)-1 {
		return nil, ErrInvalidDecimal
	}
	if dot == start {
		value = value[:start] + "0" + value[start:]
	}
	if strings.HasSuffix(value, ".") {
		value += "0"
	}
	rat, ok := new(big.Rat).SetString(value)
	if !ok {
		return nil, ErrInvalidDecimal
	}
	return rat, nil
}

// PriceMicrosPerMillionFromInternal converts the legacy database source unit
// (CNY cents per 1,000 tokens), an optional cache ratio and an organization
// multiplier into one immutable integer price snapshot. The conversion rounds
// half-up once to the nearest micro-CNY per million tokens.
func PriceMicrosPerMillionFromInternal(baseCentsPerThousand, ratio, multiplier string) (int64, error) {
	base, err := ParseDecimal(baseCentsPerThousand)
	if err != nil {
		return 0, fmt.Errorf("base price: %w", err)
	}
	ratioValue, err := ParseDecimal(ratio)
	if err != nil {
		return 0, fmt.Errorf("price ratio: %w", err)
	}
	multiplierValue, err := ParseDecimal(multiplier)
	if err != nil {
		return 0, fmt.Errorf("price multiplier: %w", err)
	}
	if base.Sign() < 0 || ratioValue.Sign() < 0 || multiplierValue.Sign() < 0 {
		return 0, ErrNegativeValue
	}
	value := new(big.Rat).Mul(base, ratioValue)
	value.Mul(value, multiplierValue)
	value.Mul(value, new(big.Rat).SetInt64(internalPriceScale))
	return roundHalfUpNonnegative(value)
}

// Calculate rounds the sum of exact component numerators once, then assigns
// the rounding remainder by largest fractional remainder. This ensures the
// persisted/displayed breakdown exactly conserves the authoritative total.
func Calculate(components ...Component) (Calculation, error) {
	numerators := make([]*big.Int, len(components))
	totalNumerator := new(big.Int)
	basis := big.NewInt(PriceBasisTokens)
	for i, component := range components {
		if component.Tokens < 0 || component.PriceMicrosPerMillion < 0 {
			return Calculation{}, ErrNegativeValue
		}
		numerator := new(big.Int).Mul(big.NewInt(component.Tokens), big.NewInt(component.PriceMicrosPerMillion))
		numerators[i] = numerator
		totalNumerator.Add(totalNumerator, numerator)
	}
	total, err := roundHalfUpFractionNonnegative(totalNumerator, basis)
	if err != nil {
		return Calculation{}, err
	}

	allocations := make([]int64, len(components))
	type remainder struct {
		index int
		value *big.Int
	}
	remainders := make([]remainder, len(components))
	var floorSum int64
	for i, numerator := range numerators {
		quotient, rem := new(big.Int), new(big.Int)
		quotient.QuoRem(numerator, basis, rem)
		if !quotient.IsInt64() {
			return Calculation{}, ErrOverflow
		}
		allocations[i] = quotient.Int64()
		if allocations[i] > math.MaxInt64-floorSum {
			return Calculation{}, ErrOverflow
		}
		floorSum += allocations[i]
		remainders[i] = remainder{index: i, value: rem}
	}
	toAllocate := total - floorSum
	if toAllocate < 0 || toAllocate > int64(len(components)) {
		return Calculation{}, fmt.Errorf("component conservation invariant violated")
	}
	sort.SliceStable(remainders, func(i, j int) bool {
		return remainders[i].value.Cmp(remainders[j].value) > 0
	})
	for i := int64(0); i < toAllocate; i++ {
		allocations[remainders[i].index]++
	}
	return Calculation{TotalMicros: total, ComponentMicros: allocations}, nil
}

// CalculateUnits rounds the aggregate exact cost once at the micro-CNY
// boundary and returns a conserved per-meter breakdown.
func CalculateUnits(components ...UnitComponent) (Calculation, error) {
	exact := make([]*big.Rat, len(components))
	total := new(big.Rat)
	for index, component := range components {
		if component.Quantity < 0 || component.AmountMicros < 0 || component.UnitQuantity <= 0 {
			return Calculation{}, ErrNegativeValue
		}
		numerator := new(big.Int).Mul(big.NewInt(component.Quantity), big.NewInt(component.AmountMicros))
		exact[index] = new(big.Rat).SetFrac(numerator, big.NewInt(component.UnitQuantity))
		total.Add(total, exact[index])
	}
	totalMicros, err := roundHalfUpNonnegative(total)
	if err != nil {
		return Calculation{}, err
	}

	allocations := make([]int64, len(components))
	type remainder struct {
		index int
		value *big.Rat
	}
	remainders := make([]remainder, len(components))
	var floorSum int64
	for index, value := range exact {
		floor := new(big.Int).Quo(value.Num(), value.Denom())
		if !floor.IsInt64() {
			return Calculation{}, ErrOverflow
		}
		allocations[index] = floor.Int64()
		if allocations[index] > math.MaxInt64-floorSum {
			return Calculation{}, ErrOverflow
		}
		floorSum += allocations[index]
		fraction := new(big.Rat).Sub(value, new(big.Rat).SetInt(floor))
		remainders[index] = remainder{index: index, value: fraction}
	}
	toAllocate := totalMicros - floorSum
	if toAllocate < 0 || toAllocate > int64(len(components)) {
		return Calculation{}, fmt.Errorf("unit component conservation invariant violated")
	}
	sort.SliceStable(remainders, func(left, right int) bool {
		return remainders[left].value.Cmp(remainders[right].value) > 0
	})
	for index := int64(0); index < toAllocate; index++ {
		allocations[remainders[index].index]++
	}
	return Calculation{TotalMicros: totalMicros, ComponentMicros: allocations}, nil
}

// EstimateUnitHold applies the same exact decimal safety factor used for token
// reservations to one non-token rational component and ceilings the result.
func EstimateUnitHold(component UnitComponent, safetyFactor string) (int64, error) {
	if component.Quantity < 0 || component.AmountMicros < 0 || component.UnitQuantity <= 0 {
		return 0, ErrNegativeValue
	}
	safety, err := ParseDecimal(safetyFactor)
	if err != nil {
		return 0, fmt.Errorf("safety factor: %w", err)
	}
	if safety.Sign() < 0 {
		return 0, ErrNegativeValue
	}
	numerator := new(big.Int).Mul(big.NewInt(component.Quantity), big.NewInt(component.AmountMicros))
	exact := new(big.Rat).SetFrac(numerator, big.NewInt(component.UnitQuantity))
	exact.Mul(exact, safety)
	return ceilNonnegative(exact)
}

// EstimateHold computes a conservative reservation hold. It ceilings the
// exact token estimate after applying a decimal safety factor, then applies
// the explicitly cents-denominated minimum hold.
func EstimateHold(tokens, priceMicrosPerMillion int64, safetyFactor string, minFreezeCents int64) (int64, error) {
	if tokens < 0 || priceMicrosPerMillion < 0 || minFreezeCents < 0 {
		return 0, ErrNegativeValue
	}
	safety, err := ParseDecimal(safetyFactor)
	if err != nil {
		return 0, fmt.Errorf("safety factor: %w", err)
	}
	if safety.Sign() < 0 {
		return 0, ErrNegativeValue
	}
	numerator := new(big.Int).Mul(big.NewInt(tokens), big.NewInt(priceMicrosPerMillion))
	exact := new(big.Rat).SetFrac(numerator, big.NewInt(PriceBasisTokens))
	exact.Mul(exact, safety)
	hold, err := ceilNonnegative(exact)
	if err != nil {
		return 0, err
	}
	minimum, err := CentsToMicros(minFreezeCents)
	if err != nil {
		return 0, err
	}
	if hold < minimum {
		return minimum, nil
	}
	return hold, nil
}

func roundHalfUpNonnegative(value *big.Rat) (int64, error) {
	if value.Sign() < 0 {
		return 0, ErrNegativeValue
	}
	return roundHalfUpFractionNonnegative(value.Num(), value.Denom())
}

func roundHalfUpFractionNonnegative(numerator, denominator *big.Int) (int64, error) {
	if numerator.Sign() < 0 || denominator.Sign() <= 0 {
		return 0, ErrNegativeValue
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if new(big.Int).Lsh(remainder, 1).Cmp(denominator) >= 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, ErrOverflow
	}
	return quotient.Int64(), nil
}

func ceilNonnegative(value *big.Rat) (int64, error) {
	if value.Sign() < 0 {
		return 0, ErrNegativeValue
	}
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(value.Num(), value.Denom(), remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, ErrOverflow
	}
	return quotient.Int64(), nil
}
