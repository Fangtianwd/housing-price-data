---
name: housing-price-data
description: 'Fetch and analyze Chinese residential property price index data from the National Bureau of Statistics (国家统计局). Use when users ask about housing price trends, index values, 环比 (month-over-month) or 同比 (year-over-year) changes, 新建商品住宅 (new residential) or 二手住宅 (second-hand residential) data for any of the 70 major Chinese cities (e.g., 武汉, 北京, 上海, 广州, 深圳). Also useful when users ask about 房价指数, 住宅价格, or 楼市走势.'
compatibility: Requires Python 3 with requests, beautifulsoup4, and matplotlib packages and internet access to stats.gov.cn.
metadata:
  author: jiangshengcheng
  version: "2.1"
---

# 中国70城住宅价格指数数据技能

本技能通过抓取国家统计局官方 RSS，获取《70个大中城市商品住宅销售价格变动情况》数据，并解析各城市的新建商品住宅与二手住宅价格指数。

## 参数说明

运行脚本 `scripts/fetch_data.py` 时支持以下参数：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--city` | `武汉` | 目标城市名称（需与统计局表格中的城市名一致） |
| `--metrics` | `环比,同比` | 指标，逗号分隔。可选：`环比`、`同比`、`定基` |
| `--limit` | `100` | 最多返回最近 N 期数据 |
| `--no-cache` | - | 禁用缓存 |
| `--chart` | - | 生成分析图表 |
| `--output` | - | 图表输出路径（仅与 --chart 配合） |

## 调用流程

### 模式1：获取数据

1. 先告知用户："正在从国家统计局获取数据，请稍候…"
2. 确定用户意图中的城市名和指标（未提及则用默认值）
3. 运行脚本：
   ```
   python scripts/fetch_data.py --city <城市> --metrics <指标> --limit 100
   ```
4. 解析 JSON 输出
5. 以 **Markdown 表格 + 文字摘要** 呈现结果

### 模式2：生成图表

当用户要求"图表"、"可视化"、"分析"时：

1. 运行脚本：
   ```
   python scripts/fetch_data.py --city <城市> --chart
   ```
2. 输出中会包含 `chart_path` 字段
3. 使用 message 工具发送图片：
   ```
   message(action="send", channel="telegram", filePath=chart_path, ...)
   ```
4. 附带文字分析和建议

## 脚本输出格式

### 数据模式

```json
{
  "city": "武汉",
  "metrics": ["环比", "同比"],
  "records": [
    {
      "period": "2025-01",
      "indicator": "新建商品住宅销售价格指数",
      "metrics": {"环比": 99.8, "同比": 95.2},
      "source_url": "https://..."
    }
  ],
  "items_scanned": 12,
  "cache_size": 5
}
```

### 图表模式

```json
{
  "city": "武汉",
  "metrics": ["环比", "同比"],
  "records": [...],
  "items_scanned": 29,
  "chart_path": "/Users/xxx/.openclaw/workspace/武汉_housing_analysis.png"
}
```

## 图表说明

`--chart` 参数生成的图表包含 4 个子图：

1. **环比指数趋势** - 月度变化，100 为基准线
2. **同比指数趋势** - 年度对比，100 为基准线
3. **同比涨跌幅** - 柱状图显示涨跌幅度
4. **新房vs二手房差值** - 两者的价格差距

## 输出格式模板

### Markdown 表格

```
## {城市}住宅销售价格指数

| 期次 | 指标 | 环比 | 同比 |
|------|------|------|------|
| 2025-01 | 新建商品住宅 | 99.8 | 95.2 |
| 2025-01 | 二手住宅 | 99.3 | 93.1 |
```

- 指标列名简写：`新建商品住宅销售价格指数` → `新建商品住宅`；`二手住宅销售价格指数` → `二手住宅`
- 指数以上月/上年同月=100 为基准，**低于 100 表示下降，高于 100 表示上涨**

### 文字摘要

在表格后提供最新一期的简短解读：
- 最新期次和数值
- 近期趋势（连续上涨/下跌期数）
- 新建与二手对比
- 购房/投资建议

## 示例

### 示例 1：默认查询

**用户**：武汉最近房价怎么样？

**操作**：运行 `python scripts/fetch_data.py --city 武汉 --metrics 环比,同比 --limit 100`

**呈现**：展示最近数据表格 + 趋势摘要

### 示例 2：生成图表

**用户**：帮我分析一下上海的房价走势，要图表

**操作**：
1. 运行 `python scripts/fetch_data.py --city 上海 --chart`
2. 发送 `chart_path` 指定的图片
3. 附带分析建议

### 示例 3：城市不在列表

若用户指定的城市在统计局数据中找不到，说明该城市不在 70 个大中城市范围内，并推荐查看 `references/REFERENCE.md` 中的城市列表。

## 功能列表（v2.1）

- **数据获取** - 从国家统计局 RSS 抓取官方数据
- **缓存机制** - 减少重复网络请求，缓存有效期 1 小时
- **图表生成** - 一键生成分析图表（--chart）
- **自定义异常** - 更清晰的错误分类和处理
- **配置分离** - 常量配置独立到 `scripts/config.py`

## 注意事项

- 数据来源为国家统计局官方网站，存在网络访问失败的可能性
- 统计局一般在每月中旬发布上月数据
- 指数基准：100 = 上月（环比）或上年同月（同比），不是绝对价格
- 图表默认保存到工作区，可通过 `--output` 指定路径
- 更多城市列表和指标说明见 [references/REFERENCE.md](references/REFERENCE.md)
