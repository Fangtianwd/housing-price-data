---
name: housing-price-data
description: Fetch and analyze Chinese residential property price index data from
  the National Bureau of Statistics (国家统计局). Use when users ask about housing
  price trends, index values, 环比 (month-over-month) or 同比 (year-over-year) changes,
  新建商品住宅 (new residential) or 二手住宅 (second-hand residential) data for any
  of the 70 major Chinese cities (e.g., 武汉, 北京, 上海, 广州, 深圳). Also useful
  when users ask about 房价指数, 住宅价格, or 楼市走势.
compatibility: Requires Python 3 with requests and beautifulsoup4 packages and internet access to stats.gov.cn.
metadata:
  author: jiangshengcheng
  version: "1.0"
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

## 调用流程

1. 先告知用户："正在从国家统计局获取数据，请稍候…"
2. 确定用户意图中的城市名和指标（未提及则用默认值）
3. 运行脚本：
   ```
   python scripts/fetch_data.py --city <城市> --metrics <指标> --limit 100
   ```
4. 解析 JSON 输出（见下方格式）
5. 以 **Markdown 表格 + 文字摘要** 呈现结果

## 脚本输出格式

```json
{
  "city": "武汉",
  "metrics": ["环比", "同比"],
  "records": [
    {
      "period": "2025-01",
      "indicator": "新建商品住宅销售价格指数",
      "metrics": {"环比": 99.8, "同比": 95.2}
    },
    {
      "period": "2025-01",
      "indicator": "二手住宅销售价格指数",
      "metrics": {"环比": 99.3, "同比": 93.1}
    }
  ],
  "items_scanned": 12
}
```

若脚本输出 `"error"` 字段，向用户说明原因（网络问题、城市名称不在统计局列表等）。

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
- 环比列注释：上月=100；同比列注释：上年同月=100

### 文字摘要

在表格后提供最新一期的简短解读：
- 最新期次和数值
- 近期趋势（连续上涨/下跌期数，若数据足够）
- 新建与二手对比（如两者都有）

## 示例

### 示例 1：默认查询

**用户**：武汉最近房价怎么样？

**操作**：运行 `python scripts/fetch_data.py --city 武汉 --metrics 环比,同比 --limit 100`

**呈现**：展示最近100期表格 + 趋势摘要

### 示例 2：指定城市和指标

**用户**：上海新建住宅的同比数据

**操作**：运行 `python scripts/fetch_data.py --city 上海 --metrics 同比 --limit 100`

### 示例 3：城市不在列表

若用户指定的城市在统计局数据中找不到，说明该城市不在 70 个大中城市范围内，并推荐查看 `references/REFERENCE.md` 中的城市列表。

## 注意事项

- 数据来源为国家统计局官方网站，存在网络访问失败的可能性（需从中国大陆或能访问该站的网络环境运行）
- 统计局一般在每月中旬发布上月数据
- 指数基准：100 = 上月（环比）或上年同月（同比），不是绝对价格
- 更多城市列表和指标说明见 [references/REFERENCE.md](references/REFERENCE.md)
