package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ── normalizeText ──────────────────────────────────────────────────────────────

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"leading/trailing spaces", "  hello  ", "hello"},
		{"multiple spaces", "a  b   c", "a b c"},
		{"nbsp", "a\u00a0b", "a b"},
		{"ideographic space", "a\u3000b", "a b"},
		{"tab", "a\tb", "a b"},
		{"newline", "a\nb", "a b"},
		{"carriage return", "a\rb", "a b"},
		{"html entity", "&amp;lt;", "&lt;"},
		{"mixed whitespace", " \t hello \n world \r ", "hello world"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeText(tt.input))
		})
	}
}

// ── compact ───────────────────────────────────────────────────────────────────

func TestCompact(t *testing.T) {
	assert.Equal(t, "武汉", compact("  武  汉  "))
	assert.Equal(t, "环比", compact("环 比"))
	assert.Equal(t, "", compact("   "))
	assert.Equal(t, "abc", compact(" a b c "))
}

// ── parseNumber ───────────────────────────────────────────────────────────────

func TestParseNumber(t *testing.T) {
	tests := []struct {
		input   string
		wantVal float64
		wantOK  bool
	}{
		{"100.5", 100.5, true},
		{"100.5%", 100.5, true},
		{" 99 ", 99, true},
		{"1,234.5", 1234.5, true},
		{"1，234", 1234, true},
		{"-", 0, false},
		{"--", 0, false},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := parseNumber(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.InDelta(t, tt.wantVal, got, 1e-9)
			}
		})
	}
}

// ── parsePubDate ──────────────────────────────────────────────────────────────

func TestParsePubDate(t *testing.T) {
	t.Run("RFC1123Z", func(t *testing.T) {
		got := parsePubDate("Mon, 02 Jan 2006 15:04:05 -0700")
		assert.Equal(t, 2006, got.Year())
		assert.Equal(t, time.January, got.Month())
	})
	t.Run("RFC1123", func(t *testing.T) {
		got := parsePubDate("Mon, 02 Jan 2006 15:04:05 MST")
		assert.Equal(t, 2006, got.Year())
	})
	t.Run("empty returns zero", func(t *testing.T) {
		assert.True(t, parsePubDate("").IsZero())
	})
	t.Run("garbage returns zero", func(t *testing.T) {
		assert.True(t, parsePubDate("not-a-date").IsZero())
	})
}

// ── parsePeriod ───────────────────────────────────────────────────────────────

func TestParsePeriod(t *testing.T) {
	t.Run("full year-month in title", func(t *testing.T) {
		got, label := parsePeriod("2024年1月份70个大中城市商品住宅销售价格变动情况", "")
		assert.Equal(t, "2024-01", label)
		assert.Equal(t, 2024, got.Year())
		assert.Equal(t, time.January, got.Month())
	})
	t.Run("two-digit month", func(t *testing.T) {
		got, label := parsePeriod("2023年11月70个大中城市数据", "")
		assert.Equal(t, "2023-11", label)
		assert.Equal(t, time.November, got.Month())
	})
	t.Run("fallback to pubDate when no year-month in title", func(t *testing.T) {
		_, label := parsePeriod("无日期标题", "Mon, 02 Jan 2006 15:04:05 -0700")
		assert.Equal(t, "2006-01", label)
	})
	t.Run("fallback to now when both empty", func(t *testing.T) {
		got, _ := parsePeriod("无日期", "")
		assert.WithinDuration(t, time.Now(), got, time.Minute)
	})
}

// ── looksLikeHeader ───────────────────────────────────────────────────────────

func TestLooksLikeHeader(t *testing.T) {
	assert.True(t, looksLikeHeader([]string{"城市", "环比", "同比"}))
	assert.True(t, looksLikeHeader([]string{"地区", "同比涨跌幅"}))
	assert.True(t, looksLikeHeader([]string{"定基价格指数"}))
	assert.False(t, looksLikeHeader([]string{"武汉", "101.2", "99.8"}))
	assert.False(t, looksLikeHeader([]string{}))
}

// ── containsCategory ─────────────────────────────────────────────────────────

func TestContainsCategory(t *testing.T) {
	assert.True(t, containsCategory([]string{"城市", "分类", "环比"}))
	assert.False(t, containsCategory([]string{"城市", "环比", "同比"}))
	assert.False(t, containsCategory([]string{}))
}

// ── containsCity ─────────────────────────────────────────────────────────────

func TestContainsCity(t *testing.T) {
	assert.True(t, containsCity("武汉", "武汉"))
	assert.True(t, containsCity(" 武 汉 ", "武汉"))
	assert.False(t, containsCity("北京", "武汉"))
	assert.False(t, containsCity("", "武汉"))
}

// ── segmentContainsCity ───────────────────────────────────────────────────────

func TestSegmentContainsCity(t *testing.T) {
	assert.True(t, segmentContainsCity([]string{"武汉", "101.2", "99.8"}, "武汉"))
	assert.True(t, segmentContainsCity([]string{" 武 汉 ", "100"}, "武汉"))
	assert.False(t, segmentContainsCity([]string{"北京", "101.2"}, "武汉"))
	assert.False(t, segmentContainsCity([]string{}, "武汉"))
}

// ── pickCitySegment ───────────────────────────────────────────────────────────

func TestPickCitySegment(t *testing.T) {
	t.Run("header with 城市 column, city present", func(t *testing.T) {
		header := []string{"城市", "环比", "同比"}
		row := []string{"武汉", "101.2", "99.8"}
		sh, sr := pickCitySegment(row, header, "武汉")
		assert.Equal(t, header, sh)
		assert.Equal(t, row, sr)
	})

	t.Run("city not in row returns nil", func(t *testing.T) {
		header := []string{"城市", "环比", "同比"}
		row := []string{"北京", "102.0", "98.0"}
		sh, sr := pickCitySegment(row, header, "武汉")
		assert.Nil(t, sh)
		assert.Nil(t, sr)
	})

	t.Run("multiple city columns — picks correct block", func(t *testing.T) {
		// header: [城市, 环比, 同比, 城市, 环比, 同比]
		header := []string{"城市", "环比", "同比", "城市", "环比", "同比"}
		row := []string{"北京", "101.0", "98.0", "武汉", "102.5", "100.1"}
		sh, sr := pickCitySegment(row, header, "武汉")
		assert.Equal(t, []string{"城市", "环比", "同比"}, sh)
		assert.Equal(t, []string{"武汉", "102.5", "100.1"}, sr)
	})

	t.Run("empty row returns nil", func(t *testing.T) {
		sh, sr := pickCitySegment([]string{}, []string{"城市", "环比"}, "武汉")
		assert.Nil(t, sh)
		assert.Nil(t, sr)
	})
}

// ── extractCityAndMetrics ─────────────────────────────────────────────────────

func TestExtractCityAndMetrics(t *testing.T) {
	t.Run("normal row", func(t *testing.T) {
		header := []string{"城市", "环比", "同比", "定基"}
		row := []string{"武汉", "101.2", "99.8", "110.5"}
		city, metrics := extractCityAndMetrics(header, row)
		assert.Equal(t, "武汉", city)
		assert.InDelta(t, 101.2, metrics["环比"], 1e-9)
		assert.InDelta(t, 99.8, metrics["同比"], 1e-9)
		assert.NotContains(t, metrics, "定基") // 非环比/同比列被过滤
	})

	t.Run("分类 column is skipped", func(t *testing.T) {
		header := []string{"城市", "分类", "环比"}
		row := []string{"武汉", "新建", "101.2"}
		city, metrics := extractCityAndMetrics(header, row)
		assert.Equal(t, "武汉", city)
		assert.NotContains(t, metrics, "分类")
	})

	t.Run("row shorter than header — truncate gracefully", func(t *testing.T) {
		header := []string{"城市", "环比", "同比"}
		row := []string{"武汉", "101.2"}
		city, metrics := extractCityAndMetrics(header, row)
		assert.Equal(t, "武汉", city)
		assert.InDelta(t, 101.2, metrics["环比"], 1e-9)
		assert.NotContains(t, metrics, "同比")
	})

	t.Run("empty inputs", func(t *testing.T) {
		city, metrics := extractCityAndMetrics([]string{}, []string{})
		assert.Empty(t, city)
		assert.Nil(t, metrics)
	})
}

// ── dedupeByIndicatorPeriod ───────────────────────────────────────────────────

func TestDedupeByIndicatorPeriod(t *testing.T) {
	r1 := record{PeriodLabel: "2024-01", Indicator: "新建", Metrics: map[string]float64{"环比": 101.0}}
	r2 := record{PeriodLabel: "2024-01", Indicator: "新建", Metrics: map[string]float64{"环比": 101.0, "同比": 99.5}}
	r3 := record{PeriodLabel: "2024-01", Indicator: "二手", Metrics: map[string]float64{"环比": 100.5}}

	result := dedupeByIndicatorPeriod([]record{r1, r2, r3})
	assert.Len(t, result, 2)

	byKey := map[string]record{}
	for _, r := range result {
		byKey[r.PeriodLabel+"|"+r.Indicator] = r
	}
	// r2 has more metrics, should be kept over r1
	assert.Len(t, byKey["2024-01|新建"].Metrics, 2)
	assert.Len(t, byKey["2024-01|二手"].Metrics, 1)
}

func TestDedupeByIndicatorPeriodEmpty(t *testing.T) {
	assert.Empty(t, dedupeByIndicatorPeriod(nil))
}

// ── metricPriority ────────────────────────────────────────────────────────────

func TestMetricPriority(t *testing.T) {
	assert.Equal(t, 0, metricPriority("环比涨跌幅"))
	assert.Equal(t, 1, metricPriority("同比涨跌幅"))
	assert.Equal(t, 2, metricPriority("定基价格指数"))
	assert.Equal(t, 3, metricPriority("其他指标"))
}

// ── collectMetricColumns ──────────────────────────────────────────────────────

func TestCollectMetricColumns(t *testing.T) {
	records := []record{
		{Metrics: map[string]float64{"同比": 1, "环比": 2}},
		{Metrics: map[string]float64{"定基": 3, "环比": 2}},
	}
	cols := collectMetricColumns(records)
	assert.Equal(t, []string{"环比", "同比", "定基"}, cols)
}

func TestCollectMetricColumnsEmpty(t *testing.T) {
	assert.Empty(t, collectMetricColumns(nil))
}
