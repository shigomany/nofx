package market

import (
	"context"
	"fmt"
	"math"
	"nofx/provider/hyperliquid"
	"strconv"
	"strings"
)

const (
	hyperliquidWarmupCandles = 200
	hyperliquidMinWarmup     = 80
	hyperliquidMaxSeriesBars = 30
)

type hyperliquidKlineFetcher func(symbol, timeframe string, limit int) ([]Kline, error)

// GetHyperliquidWithTimeframes returns native Hyperliquid candle data for the
// indicator-strategy market universe. It deliberately does not enrich the
// response with CoinAnk, Binance, open-interest, or funding-rate data.
func GetHyperliquidWithTimeframes(
	symbol string,
	timeframes []string,
	primary string,
	klineCount int,
) (*Data, error) {
	return getHyperliquidWithTimeframes(symbol, timeframes, primary, klineCount, getKlinesFromHyperliquid)
}

func getHyperliquidWithTimeframes(
	symbol string,
	timeframes []string,
	primary string,
	klineCount int,
	fetch hyperliquidKlineFetcher,
) (*Data, error) {
	normalizedSymbol, coin, err := normalizeHyperliquidStrategySymbol(symbol)
	if err != nil {
		return nil, err
	}
	if len(timeframes) == 0 {
		return nil, fmt.Errorf("at least one Hyperliquid timeframe is required")
	}
	if klineCount <= 0 {
		klineCount = hyperliquidMaxSeriesBars
	}
	if klineCount > hyperliquidMaxSeriesBars {
		return nil, fmt.Errorf("Hyperliquid kline count %d exceeds maximum %d", klineCount, hyperliquidMaxSeriesBars)
	}

	primary = strings.ToLower(strings.TrimSpace(primary))
	if primary == "" {
		primary = strings.ToLower(strings.TrimSpace(timeframes[0]))
	}

	requested := make([]string, 0, len(timeframes))
	seen := make(map[string]struct{}, len(timeframes))
	hasPrimary := false
	for _, raw := range timeframes {
		tf := strings.ToLower(strings.TrimSpace(raw))
		if tf != "4h" && tf != "1d" {
			return nil, fmt.Errorf("unsupported Hyperliquid strategy timeframe %q: only 4h and 1d are allowed", raw)
		}
		if _, exists := seen[tf]; exists {
			return nil, fmt.Errorf("duplicate Hyperliquid timeframe %q", tf)
		}
		seen[tf] = struct{}{}
		requested = append(requested, tf)
		if tf == primary {
			hasPrimary = true
		}
	}
	if !hasPrimary {
		return nil, fmt.Errorf("primary timeframe %q must be requested", primary)
	}

	timeframeData := make(map[string]*TimeframeSeriesData, len(requested))
	var primaryKlines []Kline
	for _, tf := range requested {
		klines, fetchErr := fetch(coin, tf, hyperliquidWarmupCandles)
		if fetchErr != nil {
			return nil, fmt.Errorf("get native Hyperliquid %s %s candles: %w", normalizedSymbol, tf, fetchErr)
		}
		if validateErr := validateHyperliquidKlines(klines, tf, klineCount); validateErr != nil {
			return nil, fmt.Errorf("invalid native Hyperliquid %s %s candles: %w", normalizedSymbol, tf, validateErr)
		}

		series := calculateTimeframeSeries(klines, tf, klineCount)
		if err := validateHyperliquidSeries(series, klineCount); err != nil {
			return nil, fmt.Errorf("invalid calculated Hyperliquid %s %s series: %w", normalizedSymbol, tf, err)
		}
		timeframeData[tf] = series
		if tf == primary {
			primaryKlines = klines
		}
	}

	currentPrice := primaryKlines[len(primaryKlines)-1].Close
	return &Data{
		Symbol:        normalizedSymbol,
		CurrentPrice:  currentPrice,
		PriceChange1h: calculatePriceChangeByBars(primaryKlines, primary, 60),
		PriceChange4h: calculatePriceChangeByBars(primaryKlines, primary, 240),
		CurrentEMA20:  calculateEMA(primaryKlines, 20),
		CurrentMACD:   calculateMACD(primaryKlines),
		CurrentRSI7:   calculateRSI(primaryKlines, 7),
		OpenInterest:  &OIData{},
		TimeframeData: timeframeData,
	}, nil
}

func normalizeHyperliquidStrategySymbol(symbol string) (normalized string, coin string, err error) {
	normalized = Normalize(strings.TrimSpace(symbol))
	switch normalized {
	case "BTCUSDT":
		return normalized, "BTC", nil
	case "ETHUSDT":
		return normalized, "ETH", nil
	default:
		return "", "", fmt.Errorf("unsupported Hyperliquid strategy symbol %q: only BTCUSDT and ETHUSDT are allowed", symbol)
	}
}

func validateHyperliquidKlines(klines []Kline, timeframe string, requested int) error {
	if len(klines) < hyperliquidMinWarmup {
		return fmt.Errorf("insufficient warmup: got %d candles, need at least %d", len(klines), hyperliquidMinWarmup)
	}
	if len(klines) < requested {
		return fmt.Errorf("insufficient output candles: got %d, need %d", len(klines), requested)
	}
	duration, err := TFDuration(timeframe)
	if err != nil {
		return err
	}
	intervalMillis := duration.Milliseconds()
	for i, bar := range klines {
		if bar.OpenTime <= 0 || bar.OpenTime%intervalMillis != 0 {
			return fmt.Errorf("bar %d has unaligned open timestamp %d", i, bar.OpenTime)
		}
		if bar.CloseTime < bar.OpenTime || bar.CloseTime >= bar.OpenTime+intervalMillis {
			return fmt.Errorf("bar %d has invalid close timestamp %d", i, bar.CloseTime)
		}
		if i > 0 && bar.OpenTime <= klines[i-1].OpenTime {
			return fmt.Errorf("bar %d timestamp %d is not strictly increasing", i, bar.OpenTime)
		}
		if !positiveFinite(bar.Open) || !positiveFinite(bar.High) ||
			!positiveFinite(bar.Low) || !positiveFinite(bar.Close) {
			return fmt.Errorf("bar %d contains non-positive or non-finite OHLC", i)
		}
		if bar.High < math.Max(bar.Open, bar.Close) || bar.Low > math.Min(bar.Open, bar.Close) || bar.Low > bar.High {
			return fmt.Errorf("bar %d contains inconsistent OHLC", i)
		}
		if math.IsNaN(bar.Volume) || math.IsInf(bar.Volume, 0) || bar.Volume < 0 {
			return fmt.Errorf("bar %d contains invalid volume", i)
		}
	}
	return nil
}

func validateHyperliquidSeries(series *TimeframeSeriesData, expected int) error {
	if series == nil || len(series.Klines) != expected {
		return fmt.Errorf("expected %d output bars", expected)
	}
	for name, length := range map[string]int{
		"EMA20": len(series.EMA20Values),
		"EMA50": len(series.EMA50Values),
		"RSI14": len(series.RSI14Values),
	} {
		if length != len(series.Klines) {
			return fmt.Errorf("%s length %d does not align with %d klines", name, length, len(series.Klines))
		}
	}
	return nil
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// GetHyperliquidPrice returns the current native Hyperliquid mid price.
func GetHyperliquidPrice(symbol string) (float64, error) {
	_, coin, err := normalizeHyperliquidStrategySymbol(symbol)
	if err != nil {
		return 0, err
	}
	mids, err := hyperliquid.NewClient().GetAllMids(context.Background())
	if err != nil {
		return 0, fmt.Errorf("get native Hyperliquid mids: %w", err)
	}
	priceText, ok := mids[coin]
	if !ok {
		return 0, fmt.Errorf("native Hyperliquid mid is missing for %s", coin)
	}
	price, err := strconv.ParseFloat(priceText, 64)
	if err != nil || !positiveFinite(price) {
		return 0, fmt.Errorf("invalid native Hyperliquid mid for %s: %q", coin, priceText)
	}
	return price, nil
}
