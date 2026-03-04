//go:build integration

package main

import (
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var periodLabelRe = regexp.MustCompile(`^\d{4}-\d{2}$`)

func newIntegrationClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// TestFetchRSS 验证能正常拉取 RSS，且至少包含一条目标标题的条目，PeriodLabel 格式正确。
func TestFetchRSS(t *testing.T) {
	client := newIntegrationClient()
	items, err := fetchMatchedItems(client)
	require.NoError(t, err, "fetchMatchedItems 不应返回错误")
	require.NotEmpty(t, items, "RSS 中应至少包含一条「70个大中城市商品住宅销售价格变动情况」的条目")

	for _, it := range items {
		assert.Regexp(t, periodLabelRe, it.PeriodLabel,
			"PeriodLabel 应为 YYYY-MM 格式，实际：%s（标题：%s）", it.PeriodLabel, it.Item.Title)
	}
	t.Logf("共获取 %d 条匹配 RSS 条目", len(items))
}

// TestFetchAndParseLatestArticle 验证最新一篇文章能解析出武汉记录，且字段合理。
func TestFetchAndParseLatestArticle(t *testing.T) {
	client := newIntegrationClient()
	items, err := fetchMatchedItems(client)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	// 取时间最新的一条
	latest := items[len(items)-1]
	t.Logf("测试文章：%s  链接：%s", latest.Item.Title, latest.Item.Link)

	page, err := fetchBytes(client, latest.Item.Link)
	require.NoError(t, err, "文章页面应能正常获取")

	targetCity := "武汉"
	targetMetrics := []string{"环比", "同比"}
	records := parseItemPage(page, latest, targetCity, targetMetrics)
	require.NotEmpty(t, records, "应从文章中解析出至少一条记录")

	for _, r := range records {
		assert.Equal(t, targetCity, r.City, "城市应为武汉")
		assert.Contains(t, []string{indicatorNew, indicatorUsed}, r.Indicator,
			"指标应为新建或二手，实际：%s", r.Indicator)
		assert.Regexp(t, periodLabelRe, r.PeriodLabel)

		if v, ok := r.Metrics["环比"]; ok {
			assert.True(t, v >= 85 && v <= 115,
				"环比值应在合理范围 [85, 115]，实际：%.1f", v)
		}
		if v, ok := r.Metrics["同比"]; ok {
			assert.True(t, v >= 85 && v <= 115,
				"同比值应在合理范围 [85, 115]，实际：%.1f", v)
		}
		assert.NotEmpty(t, r.Metrics, "Metrics 不应为空")
	}
	t.Logf("解析到 %d 条记录，指标：%v", len(records), func() []string {
		seen := map[string]struct{}{}
		var out []string
		for _, r := range records {
			if _, ok := seen[r.Indicator]; !ok {
				seen[r.Indicator] = struct{}{}
				out = append(out, r.Indicator)
			}
		}
		return out
	}())
}

// TestBothIndicatorsPresent 验证全量数据中同时包含新建和二手两种指标。
// 若失败，说明二手住宅数据在 RSS 文章中缺失或未能被解析。
func TestBothIndicatorsPresent(t *testing.T) {
	client := newIntegrationClient()
	items, err := fetchMatchedItems(client)
	require.NoError(t, err)
	require.NotEmpty(t, items)

	hasNew := false
	hasUsed := false
	targetCity := "武汉"
	targetMetrics := []string{"环比", "同比"}

	for _, it := range items {
		page, err := fetchBytes(client, it.Item.Link)
		if err != nil {
			t.Logf("跳过（获取失败）：%s — %v", it.Item.Link, err)
			continue
		}
		for _, r := range parseItemPage(page, it, targetCity, targetMetrics) {
			switch r.Indicator {
			case indicatorNew:
				hasNew = true
			case indicatorUsed:
				hasUsed = true
			}
		}
		if hasNew && hasUsed {
			break
		}
	}

	assert.True(t, hasNew, "全量数据中应包含「%s」", indicatorNew)
	assert.True(t, hasUsed, "全量数据中应包含「%s」（当前 CSV 中缺失，需排查）", indicatorUsed)
}
