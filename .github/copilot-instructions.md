# Copilot 使用说明

## 构建 / 测试 / Lint
- Go 1.23 模块（`housing-price-data`）；依赖同步用 `go mod tidy`（goquery、go-echarts/v2、testify）。
- 运行：`go run . [-city <name>] [-metrics metric1,metric2]`（默认城市武汉，指标环比、同比）；输出 `output/{city}_{metrics}.csv` 与 `output/{city}_{metrics}_charts.html`。
- 单元测试（离线）：`go test ./...`；单测示例：`go test ./... -run TestFetchBytesRetriesWithHeaders`。
- 集成测试（联网抓取 RSS）：`go test -tags=integration ./...`；单例：`go test -tags=integration ./... -run TestFetchRSS`。
- 未配置专用 linter；按需使用 `go test` / `go vet`。

## 架构概览
- CLI 抓取国家统计局 RSS（`rssURL`），筛选标题含 `titleKey` 的条目，优先从标题正则解析 PeriodTime/PeriodLabel，缺失则用 `pubDate`（再缺失回退当前时间），并按时间排序。
- 每篇匹配文章带重试+退避（`maxFetchAttempts`、`retryDelay`、`defaultUserAgent` 及 Accept/Language/Referer 头）抓取后用 goquery 解析：定位表格，依据附近文本推断指标（`indicatorNew` / `indicatorUsed`），通过 `looksLikeHeader` 找表头，跳过含“分类”的表。
- 行解析在列组重复场景通过 `pickCitySegment` 找到目标城市片段，先 `normalizeText`/`compact`，按需求指标过滤并解析数值（容忍 %、逗号与中文标点）；`containsCity`/`containsAnyMetric` 控制是否保留。
- 记录按指标+期次去重保留信息最全者（`dedupeByIndicatorPeriod`），按期次再指标排序，生成 CSV 时按优先级排指标列（`metricPriority` / `collectMetricColumns`），并用 go-echarts 为每个指标生成折线图 HTML，强制关闭 x 轴 boundary gap。
- 默认城市武汉，默认指标环比/同比；输出目录 `output` 会被创建，日志打印每期指标摘要。

## 约定与测试注意
- 匹配表头/城市/指标前先做 normalize/compact 以处理空白与 HTML 实体。
- 保持 HTTP 头与重试策略；测试通过覆盖 `retrySleep` 控制重试时间。
- 忽略含“分类”的列；含 城市/地区/城市名称 的列视为城市列，仅提取表头包含所需指标子串的列。
- 集成测试访问真实 RSS/页面，校验指标区间 85–115 与武汉的 新建/二手 指标同时存在，使用 30s 超时的 `newIntegrationClient`。
- 新增指标时同步调整 `metricPriority` 排序，并保持按指标+期次去重逻辑以保留最完整记录。
