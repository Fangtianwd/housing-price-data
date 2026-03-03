# housing-price-data

使用 Go 抓取国家统计局 RSS 中“70个大中城市商品住宅销售价格变动情况”各期数据，提取武汉市两个指标：
- 新建商品住宅销售价格指数
- 二手住宅销售价格指数

说明：仅保留上述两个指标，排除“分类指数”相关表。

## 运行

```bash
go mod tidy
go run .
```

## 输出

- output/wuhan_two_indices_all.csv
- output/wuhan_two_indices_charts.html
