package market

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestGetHyperliquidWithTimeframesBuildsAlignedNativeSeries(t *testing.T) {
	requested := make([]string, 0, 2)
	fetch := func(symbol, timeframe string, limit int) ([]Kline, error) {
		requested = append(requested, symbol+":"+timeframe)
		if limit != hyperliquidWarmupCandles {
			t.Fatalf("limit = %d, want %d", limit, hyperliquidWarmupCandles)
		}
		return hyperliquidTestKlines(timeframe, hyperliquidWarmupCandles), nil
	}

	data, err := getHyperliquidWithTimeframes("btc", []string{"4h", "1d"}, "4h", 30, fetch)
	if err != nil {
		t.Fatalf("getHyperliquidWithTimeframes() error = %v", err)
	}
	if data.Symbol != "BTCUSDT" || data.CurrentPrice != 300 {
		t.Fatalf("unexpected identity/price: symbol=%s price=%v", data.Symbol, data.CurrentPrice)
	}
	if strings.Join(requested, ",") != "BTC:4h,BTC:1d" {
		t.Fatalf("native fetches = %v", requested)
	}
	for _, tf := range []string{"4h", "1d"} {
		series := data.TimeframeData[tf]
		if series == nil {
			t.Fatalf("missing %s series", tf)
		}
		for name, got := range map[string]int{
			"klines": len(series.Klines), "ema20": len(series.EMA20Values),
			"ema50": len(series.EMA50Values), "rsi14": len(series.RSI14Values),
		} {
			if got != 30 {
				t.Errorf("%s %s length = %d, want 30", tf, name, got)
			}
		}
	}
}

func TestGetHyperliquidWithTimeframesRejectsInvalidInputs(t *testing.T) {
	goodFetch := func(_, timeframe string, _ int) ([]Kline, error) {
		return hyperliquidTestKlines(timeframe, hyperliquidWarmupCandles), nil
	}
	tests := []struct {
		name       string
		symbol     string
		timeframes []string
		primary    string
		count      int
		want       string
	}{
		{name: "symbol", symbol: "SOLUSDT", timeframes: []string{"4h"}, primary: "4h", count: 30, want: "only BTCUSDT and ETHUSDT"},
		{name: "timeframe", symbol: "BTCUSDT", timeframes: []string{"1h"}, primary: "1h", count: 30, want: "only 4h and 1d"},
		{name: "missing primary", symbol: "BTCUSDT", timeframes: []string{"1d"}, primary: "4h", count: 30, want: "must be requested"},
		{name: "too many bars", symbol: "BTCUSDT", timeframes: []string{"4h"}, primary: "4h", count: 31, want: "exceeds maximum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getHyperliquidWithTimeframes(tt.symbol, tt.timeframes, tt.primary, tt.count, goodFetch)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestGetHyperliquidWithTimeframesRequiresEveryValidFrame(t *testing.T) {
	fetch := func(_, timeframe string, _ int) ([]Kline, error) {
		if timeframe == "1d" {
			return nil, errors.New("upstream unavailable")
		}
		return hyperliquidTestKlines(timeframe, hyperliquidWarmupCandles), nil
	}
	_, err := getHyperliquidWithTimeframes("ETHUSDT", []string{"4h", "1d"}, "4h", 30, fetch)
	if err == nil || !strings.Contains(err.Error(), "1d") {
		t.Fatalf("error = %v, want required 1d failure", err)
	}
}

func TestValidateHyperliquidKlinesRejectsInsufficientAndMalformedData(t *testing.T) {
	short := hyperliquidTestKlines("4h", hyperliquidMinWarmup-1)
	if err := validateHyperliquidKlines(short, "4h", 30); err == nil || !strings.Contains(err.Error(), "insufficient warmup") {
		t.Fatalf("short-data error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func([]Kline)
	}{
		{name: "nan", mutate: func(b []Kline) { b[20].Close = math.NaN() }},
		{name: "zero", mutate: func(b []Kline) { b[20].Low = 0 }},
		{name: "unordered", mutate: func(b []Kline) { b[20].OpenTime = b[19].OpenTime }},
		{name: "unaligned", mutate: func(b []Kline) { b[20].OpenTime++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bars := hyperliquidTestKlines("4h", hyperliquidWarmupCandles)
			tt.mutate(bars)
			if err := validateHyperliquidKlines(bars, "4h", 30); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func hyperliquidTestKlines(timeframe string, count int) []Kline {
	duration, _ := TFDuration(timeframe)
	step := duration.Milliseconds()
	start := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	bars := make([]Kline, count)
	for i := range bars {
		price := 100 + float64(i)
		bars[i] = Kline{
			OpenTime: start + int64(i)*step,
			Open:     price, High: price + 2, Low: price - 1, Close: price + 1,
			Volume: float64(i), CloseTime: start + int64(i+1)*step - 1,
		}
	}
	return bars
}
