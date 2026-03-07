# Housing Price Data Skill

从国家统计局获取中国70个大中城市住宅销售价格指数数据。

## 功能

- 获取新建商品住宅和二手住宅价格指数
- 支持环比、同比、定基三种指标
- 支持70个大中城市
- JSON 格式输出，易于解析

## 安装

```bash
pip install -r requirements.txt
```

## 使用

```bash
# 查询武汉房价指数（默认）
python scripts/fetch_data.py

# 指定城市和指标
python scripts/fetch_data.py --city 上海 --metrics 同比 --limit 50

# 查询多个指标
python scripts/fetch_data.py --city 北京 --metrics 环比,同比,定基
```

## 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--city` | 武汉 | 目标城市名称 |
| `--metrics` | 环比,同比 | 指标（逗号分隔） |
| `--limit` | 100 | 最多返回期数 |

## 输出示例

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
  "items_scanned": 12
}
```

## 支持70城列表

详见 [references/REFERENCE.md](references/REFERENCE.md)

## 数据来源

- 国家统计局: https://www.stats.gov.cn
- RSS: https://www.stats.gov.cn/sj/zxfb/rss.xml
