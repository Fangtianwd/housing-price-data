# -*- coding: utf-8 -*-
"""
配置文件 - 住宅价格指数数据获取
"""

# 国家统计局 RSS 地址
RSS_URL = "https://www.stats.gov.cn/sj/zxfb/rss.xml"

# 公告标题关键字
TITLE_KEY = "70个大中城市商品住宅销售价格变动情况"

# 指标名称
INDICATORS = {
    "new": "新建商品住宅销售价格指数",
    "used": "二手住宅销售价格指数",
    "new_cat": "新建商品住宅销售价格分类指数",
    "used_cat": "二手住宅销售价格分类指数",
}

# 指标别名（用于命令行参数）
METRIC_ALIASES = {
    "环比": ["环比", "环比指数", "月环比", "mom", "MoM"],
    "同比": ["同比", "同比指数", "年同比", "yoy", "YoY"],
    "定基": ["定基", "定基指数", "fixed-base"],
}

# 有效指标列表（用于验证）
VALID_METRICS = list(METRIC_ALIASES.keys())

# 请求配置
REQUEST_CONFIG = {
    "timeout": 20,
    "max_attempts": 3,
    "retry_delays": [0.4, 0.8, 1.5],
    "headers": {
        "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
        "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
        "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
        "Referer": "https://www.stats.gov.cn/",
    },
}

# 缓存配置
CACHE_CONFIG = {
    "enabled": True,
    "ttl_seconds": 3600,  # 1小时缓存
    "max_size": 100,
}

# 默认参数
DEFAULTS = {
    "city": "武汉",
    "metrics": "环比,同比",
    "limit": 100,
}
