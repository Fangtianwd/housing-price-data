package main

import (
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
)

const (
	rssURL        = "https://www.stats.gov.cn/sj/zxfb/rss.xml"
	titleKey      = "70个大中城市商品住宅销售价格变动情况"
	targetCity    = "武汉"
	indicatorNew  = "新建商品住宅销售价格指数"
	indicatorUsed = "二手住宅销售价格指数"
)

var periodRe = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月(?:份)?`)

type rssFeed struct {
	Channel struct {
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rssItem struct {
	Title   string `xml:"title"`
	Link    string `xml:"link"`
	PubDate string `xml:"pubDate"`
}

type itemWithPeriod struct {
	Item        rssItem
	PeriodTime  time.Time
	PeriodLabel string
}

type record struct {
	Period      time.Time
	PeriodLabel string
	Indicator   string
	City        string
	Metrics     map[string]float64
}

func main() {
	client := &http.Client{Timeout: 20 * time.Second}

	log.Println("fetching RSS feed...")
	items, err := fetchMatchedItems(client)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("matched %d items from RSS", len(items))

	var records []record
	for i, it := range items {
		log.Printf("[%d/%d] fetching %s (%s)", i+1, len(items), it.PeriodLabel, it.Item.Link)
		page, err := fetchBytes(client, it.Item.Link)
		if err != nil {
			log.Printf("  skip: fetch failed: %v", err)
			continue
		}
		parsed := parseItemPage(page, it)
		for _, r := range parsed {
			log.Printf("  [%s] %s  环比=%.1f  同比=%.1f",
				r.PeriodLabel, r.Indicator,
				r.Metrics["环比"], r.Metrics["同比"])
		}
		records = append(records, parsed...)
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].Period.Equal(records[j].Period) {
			return records[i].Indicator < records[j].Indicator
		}
		return records[i].Period.Before(records[j].Period)
	})

	if err := os.MkdirAll("output", 0o755); err != nil {
		log.Fatal(err)
	}

	csvPath := filepath.Join("output", "wuhan_two_indices_all.csv")
	log.Printf("writing CSV: %s", csvPath)
	metricOrder, err := writeCSV(csvPath, records)
	if err != nil {
		log.Fatal(err)
	}

	htmlPath := filepath.Join("output", "wuhan_two_indices_charts.html")
	log.Printf("writing HTML: %s", htmlPath)
	if err := writeChartsHTML(htmlPath, records, metricOrder); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("items scanned: %d\nrecords extracted: %d\nCSV: %s\nHTML: %s\n", len(items), len(records), csvPath, htmlPath)
}

func fetchMatchedItems(client *http.Client) ([]itemWithPeriod, error) {
	log.Printf("fetching %s", rssURL)
	body, err := fetchBytes(client, rssURL)
	if err != nil {
		return nil, fmt.Errorf("fetch rss failed: %w", err)
	}
	log.Printf("RSS fetched (%d bytes), parsing...", len(body))

	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("parse rss failed: %w", err)
	}
	log.Printf("RSS total items: %d, filtering by title key...", len(feed.Channel.Items))

	items := make([]itemWithPeriod, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		title := normalizeText(it.Title)
		if !strings.Contains(title, titleKey) {
			continue
		}
		periodTime, label := parsePeriod(title, it.PubDate)
		items = append(items, itemWithPeriod{Item: it, PeriodTime: periodTime, PeriodLabel: label})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PeriodTime.Equal(items[j].PeriodTime) {
			return items[i].PeriodLabel < items[j].PeriodLabel
		}
		return items[i].PeriodTime.Before(items[j].PeriodTime)
	})
	return items, nil
}

func fetchBytes(client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(rawURL), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func parsePeriod(title, pubDate string) (time.Time, string) {
	if m := periodRe.FindStringSubmatch(title); len(m) == 3 {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		t := time.Date(y, time.Month(mo), 1, 0, 0, 0, 0, time.Local)
		return t, fmt.Sprintf("%04d-%02d", y, mo)
	}

	fallback := parsePubDate(pubDate)
	if fallback.IsZero() {
		fallback = time.Now()
	}
	return fallback, fallback.Format("2006-01")
}

func parsePubDate(s string) time.Time {
	s = strings.TrimSpace(s)
	layouts := []string{time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseItemPage(content []byte, it itemWithPeriod) []record {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(content)))
	if err != nil {
		return nil
	}

	tables := doc.Find(".detail-text-content .txt-content .trs_editor_view table")
	if tables.Length() == 0 {
		tables = doc.Find(".trs_editor_view table")
	}

	var out []record
	tables.Each(func(_ int, table *goquery.Selection) {
		indicator := detectIndicator(table)
		if indicator == "" {
			return
		}

		header := findHeader(table)
		if len(header) == 0 || containsCategory(header) {
			return
		}

			table.Find("tr").Each(func(_ int, tr *goquery.Selection) {
			row := extractRow(tr)
			if len(row) == 0 || looksLikeHeader(row) {
				return
			}

				segHeader, segRow := pickCitySegment(row, header, targetCity)
				if len(segRow) == 0 || len(segHeader) == 0 {
				return
			}

				city, metrics := extractCityAndMetrics(segHeader, segRow)
			if !containsCity(city, targetCity) || len(metrics) == 0 {
				return
			}

			out = append(out, record{
				Period:      it.PeriodTime,
				PeriodLabel: it.PeriodLabel,
				Indicator:   indicator,
				City:        targetCity,
				Metrics:     metrics,
			})
		})
	})

	return dedupeByIndicatorPeriod(out)
}

// indicatorNewCat / indicatorUsedCat are the "分类指数" variants that should be skipped.
const (
	indicatorNewCat  = "新建商品住宅销售价格分类指数"
	indicatorUsedCat = "二手住宅销售价格分类指数"
)

func detectIndicator(table *goquery.Selection) string {
	// nearbyTexts returns texts ordered furthest-to-closest; iterate closest-first
	// so the heading immediately before this table wins over a more distant one.
	texts := nearbyTexts(table)
	for i := len(texts) - 1; i >= 0; i-- {
		t := compact(texts[i])
		if strings.Contains(t, compact(indicatorNew)) && !strings.Contains(t, compact(indicatorNewCat)) {
			return indicatorNew
		}
		if strings.Contains(t, compact(indicatorUsed)) && !strings.Contains(t, compact(indicatorUsedCat)) {
			return indicatorUsed
		}
	}

	headRows := normalizeText(strings.Join(firstNRowsText(table, 2), " "))
	compactHead := compact(headRows)
	if strings.Contains(compactHead, compact(indicatorNew)) && !strings.Contains(compactHead, compact(indicatorNewCat)) {
		return indicatorNew
	}
	if strings.Contains(compactHead, compact(indicatorUsed)) && !strings.Contains(compactHead, compact(indicatorUsedCat)) {
		return indicatorUsed
	}
	return ""
}

func nearbyTexts(table *goquery.Selection) []string {
	var out []string
	for node, depth := table, 0; depth < 3 && node.Length() > 0; depth, node = depth+1, node.Parent() {
		count := 0
		node.PrevAll().EachWithBreak(func(_ int, s *goquery.Selection) bool {
			text := normalizeText(s.Text())
			if text == "" {
				return true
			}
			out = append([]string{text}, out...)
			count++
			return count < 4
		})
	}
	return out
}

func firstNRowsText(table *goquery.Selection, n int) []string {
	var out []string
	table.Find("tr").EachWithBreak(func(i int, tr *goquery.Selection) bool {
		if i >= n {
			return false
		}
		text := normalizeText(tr.Text())
		if text != "" {
			out = append(out, text)
		}
		return true
	})
	return out
}

func findHeader(table *goquery.Selection) []string {
	var header []string
	table.Find("tr").EachWithBreak(func(_ int, tr *goquery.Selection) bool {
		row := extractRow(tr)
		if looksLikeHeader(row) {
			header = row
			return false
		}
		return true
	})
	return header
}

func extractRow(tr *goquery.Selection) []string {
	row := make([]string, 0, 16)
	tr.Find("th,td").Each(func(_ int, c *goquery.Selection) {
		text := normalizeText(c.Text())
		if text != "" {
			row = append(row, text)
		}
	})
	return row
}

func looksLikeHeader(row []string) bool {
	for _, cell := range row {
		n := normalizeText(cell)
		if strings.Contains(n, "城市") || strings.Contains(n, "环比") || strings.Contains(n, "同比") || strings.Contains(n, "定基") {
			return true
		}
	}
	return false
}

func containsCategory(header []string) bool {
	for _, h := range header {
		if strings.Contains(normalizeText(h), "分类") {
			return true
		}
	}
	return false
}

func pickCitySegment(row, header []string, city string) (segHeader []string, segRow []string) {
	if len(row) == 0 {
		return nil, nil
	}

	if len(header) > 0 {
		cityCols := make([]int, 0)
		for i, h := range header {
			hn := normalizeText(h)
			if strings.Contains(hn, "城市") {
				cityCols = append(cityCols, i)
			}
		}

		if len(cityCols) > 0 {
			for idx, start := range cityCols {
				end := len(header)
				if idx+1 < len(cityCols) {
					end = cityCols[idx+1]
				}
				if start < 0 || start >= end || start >= len(row) {
					continue
				}
				rowEnd := end
				if rowEnd > len(row) {
					rowEnd = len(row)
				}
				if rowEnd <= start {
					continue
				}
				blockRow := row[start:rowEnd]
				if segmentContainsCity(blockRow, city) {
					return header[start:end], blockRow
				}
			}
		}
	}

	h := len(header)
	if h > 0 && len(row)%h == 0 {
		for i := 0; i < len(row); i += h {
			seg := row[i : i+h]
			if segmentContainsCity(seg, city) {
				return header, seg
			}
		}
	}

	if segmentContainsCity(row, city) {
		return header, row
	}
	return nil, nil
}

func segmentContainsCity(seg []string, city string) bool {
	needle := compact(city)
	for _, cell := range seg {
		if strings.Contains(compact(cell), needle) {
			return true
		}
	}
	return false
}

func extractCityAndMetrics(header, row []string) (string, map[string]float64) {
	n := len(header)
	if len(row) < n {
		n = len(row)
	}
	if n == 0 {
		return "", nil
	}

	city := ""
	metrics := make(map[string]float64)
	for i := 0; i < n; i++ {
		k := normalizeText(header[i])
		v := normalizeText(row[i])
		if k == "" || v == "" {
			continue
		}
		if strings.Contains(k, "城市") || strings.Contains(k, "地区") || strings.Contains(k, "城市名称") {
			if city == "" {
				city = v
			}
			if containsCity(v, targetCity) {
				city = v
			}
			continue
		}
		if strings.Contains(k, "分类") {
			continue
		}
		if !strings.Contains(k, "环比") && !strings.Contains(k, "同比") {
			continue
		}
		if f, ok := parseNumber(v); ok {
			metrics[k] = f
		}
	}

	if city == "" {
		for _, cell := range row {
			if segmentContainsCity([]string{cell}, targetCity) {
				city = cell
				break
			}
		}
	}
	return city, metrics
}

func parseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	s = strings.ReplaceAll(s, "，", "")
	s = strings.ReplaceAll(s, ",", "")
	if s == "" || s == "-" || s == "--" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func containsCity(src, city string) bool {
	return strings.Contains(compact(src), compact(city))
}

func normalizeText(s string) string {
	s = html.UnescapeString(s)
	replacer := strings.NewReplacer("\u00a0", " ", "\u3000", " ", "\t", " ", "\n", " ", "\r", " ")
	s = replacer.Replace(strings.TrimSpace(s))
	return strings.Join(strings.Fields(s), " ")
}

func compact(s string) string {
	return strings.Join(strings.Fields(normalizeText(s)), "")
}

func dedupeByIndicatorPeriod(in []record) []record {
	best := make(map[string]record)
	for _, r := range in {
		key := r.PeriodLabel + "|" + r.Indicator
		existing, ok := best[key]
		if !ok || len(r.Metrics) > len(existing.Metrics) {
			best[key] = r
		}
	}
	out := make([]record, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	return out
}

func writeCSV(path string, records []record) ([]string, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	metricCols := collectMetricColumns(records)
	w := csv.NewWriter(f)
	defer w.Flush()

	header := append([]string{"period", "indicator", "city"}, metricCols...)
	if err := w.Write(header); err != nil {
		return nil, err
	}

	for _, r := range records {
		row := []string{r.PeriodLabel, r.Indicator, r.City}
		for _, m := range metricCols {
			if v, ok := r.Metrics[m]; ok {
				row = append(row, strconv.FormatFloat(v, 'f', -1, 64))
			} else {
				row = append(row, "")
			}
		}
		if err := w.Write(row); err != nil {
			return nil, err
		}
	}
	return metricCols, w.Error()
}

func collectMetricColumns(records []record) []string {
	set := map[string]struct{}{}
	for _, r := range records {
		for k := range r.Metrics {
			set[k] = struct{}{}
		}
	}
	cols := make([]string, 0, len(set))
	for k := range set {
		cols = append(cols, k)
	}
	sort.Slice(cols, func(i, j int) bool {
		pi := metricPriority(cols[i])
		pj := metricPriority(cols[j])
		if pi != pj {
			return pi < pj
		}
		return cols[i] < cols[j]
	})
	return cols
}

func metricPriority(name string) int {
	s := normalizeText(name)
	switch {
	case strings.Contains(s, "环比"):
		return 0
	case strings.Contains(s, "同比"):
		return 1
	case strings.Contains(s, "定基"):
		return 2
	default:
		return 3
	}
}

func writeChartsHTML(path string, records []record, _ []string) error {
	huanbiChart := buildMetricChart("环比", records)
	tongbiChart := buildMetricChart("同比", records)

	page := components.NewPage()
	page.PageTitle = "武汉住宅价格指数"
	page.AddCharts(huanbiChart, tongbiChart)

	// Render to buffer so we can inject boundaryGap (not exposed in opts.XAxis).
	var buf strings.Builder
	if err := page.Render(&buf); err != nil {
		return err
	}
	html := strings.Replace(buf.String(),
		`"xAxis":[{"data"`,
		`"xAxis":[{"boundaryGap":false,"data"`,
		-1)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(html)
	return err
}

// buildMetricChart builds a chart for one metric (e.g. "环比" or "同比"),
// with one series per indicator (新建 / 二手).
func buildMetricChart(metric string, records []record) *charts.Line {
	// Collect all periods across records and sort.
	periodSet := map[string]struct{}{}
	for _, r := range records {
		periodSet[r.PeriodLabel] = struct{}{}
	}
	periods := make([]string, 0, len(periodSet))
	for p := range periodSet {
		periods = append(periods, p)
	}
	sort.Strings(periods)

	yName := "指数（上年同月=100）"
	if metric == "环比" {
		yName = "指数（上月=100）"
	}

	line := charts.NewLine()
	line.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{Title: "武汉住宅销售价格指数 - " + metric}),
		charts.WithLegendOpts(opts.Legend{Show: opts.Bool(true)}),
		charts.WithInitializationOpts(opts.Initialization{Width: "1200px", Height: "500px"}),
		charts.WithXAxisOpts(opts.XAxis{
			Name:      "月份",
			AxisLabel: &opts.AxisLabel{Interval: "0", Rotate: 45},
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Name:  yName,
			Scale: opts.Bool(true),
			Min:   "dataMin",
			Max:   "dataMax",
		}),
		charts.WithTooltipOpts(opts.Tooltip{Show: opts.Bool(true), Trigger: "axis"}),
	)
	line.SetXAxis(periods)

	for _, indicator := range []string{indicatorNew, indicatorUsed} {
		byPeriod := map[string]record{}
		for _, r := range records {
			if r.Indicator == indicator {
				byPeriod[r.PeriodLabel] = r
			}
		}
		if len(byPeriod) == 0 {
			continue
		}
		prefix := "新建"
		if indicator == indicatorUsed {
			prefix = "二手"
		}

		series := make([]opts.LineData, 0, len(periods))
		for _, p := range periods {
			if v, ok := byPeriod[p].Metrics[metric]; ok {
				series = append(series, opts.LineData{Value: v})
			} else {
				series = append(series, opts.LineData{Value: nil})
			}
		}
		line.AddSeries(prefix, series)
	}

	return line
}

// indicatorSummary returns a short string listing unique indicators in records.
func indicatorSummary(records []record) string {
	seen := map[string]struct{}{}
	var names []string
	for _, r := range records {
		if _, ok := seen[r.Indicator]; !ok {
			seen[r.Indicator] = struct{}{}
			names = append(names, r.Indicator)
		}
	}
	if len(names) == 0 {
		return "(none)"
	}
	return strings.Join(names, ", ")
}
