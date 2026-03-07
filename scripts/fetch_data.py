#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
fetch_data.py - 抓取国家统计局70城住宅价格指数数据

用法：
  python fetch_data.py [--city <城市>] [--metrics <环比,同比>] [--limit <N>] [--chart]

输出：JSON 格式数据到 stdout，或生成图表文件
"""
import argparse
import json
import re
import sys
import time
import xml.etree.ElementTree as ET
from typing import List, Dict, Optional, Tuple
from pathlib import Path

try:
    import requests
    from bs4 import BeautifulSoup
except ImportError:
    print(json.dumps({"error": "缺少依赖包，请运行: pip install -r requirements.txt"}))
    sys.exit(1)

from config import (
    RSS_URL, TITLE_KEY, INDICATORS, REQUEST_CONFIG, 
    CACHE_CONFIG, DEFAULTS, VALID_METRICS
)
from exceptions import (
    NetworkError, ParseError, CityNotFoundError, 
    RSSFeedError, DataUnavailableError
)
from cache import SimpleCache

# 初始化缓存
_cache = SimpleCache(
    max_size=CACHE_CONFIG["max_size"],
    ttl_seconds=CACHE_CONFIG["ttl_seconds"],
) if CACHE_CONFIG["enabled"] else None

# 正则表达式
PERIOD_RE = re.compile(r"(\d{4})\s*年\s*(\d{1,2})\s*月(?:份)?")


def fetch_url(url: str, timeout: int = None) -> bytes:
    """获取 URL 内容，支持重试"""
    if timeout is None:
        timeout = REQUEST_CONFIG["timeout"]
    
    if _cache:
        cached = _cache.get(url)
        if cached:
            return cached
    
    last_err = None
    for attempt in range(REQUEST_CONFIG["max_attempts"]):
        try:
            resp = requests.get(
                url.strip(), 
                headers=REQUEST_CONFIG["headers"], 
                timeout=timeout
            )
            resp.raise_for_status()
            content = resp.content
            
            if _cache:
                _cache.set(url, content)
            
            return content
        except Exception as e:
            last_err = e
            if attempt < len(REQUEST_CONFIG["retry_delays"]):
                time.sleep(REQUEST_CONFIG["retry_delays"][attempt])
    
    raise NetworkError(f"网络请求失败: {last_err}", url=url)


def normalize(s: str) -> str:
    """规范化字符串"""
    import html as html_mod
    s = html_mod.unescape(str(s))
    s = s.replace("\u00a0", " ").replace("\u3000", " ")
    s = s.replace("\t", " ").replace("\n", " ").replace("\r", " ")
    return " ".join(s.split())


def compact(s: str) -> str:
    """移除所有空白"""
    return "".join(normalize(s).split())


def parse_number(s: str) -> Optional[float]:
    """解析数字"""
    s = normalize(s).rstrip("%").replace("，", "").replace(",", "").strip()
    if not s or s in ("-", "--"):
        return None
    try:
        return float(s)
    except ValueError:
        return None


def parse_period(title: str) -> Optional[str]:
    """从标题提取期次"""
    m = PERIOD_RE.search(title)
    if m:
        y, mo = int(m.group(1)), int(m.group(2))
        return f"{y:04d}-{mo:02d}"
    return None


def fetch_rss_items() -> List[Tuple[str, str]]:
    """获取 RSS 条目"""
    try:
        content = fetch_url(RSS_URL)
        root = ET.fromstring(content)
    except Exception as e:
        raise RSSFeedError(f"RSS 获取失败: {e}", feed_url=RSS_URL)
    
    items = []
    for item in root.iter("item"):
        title_el = item.find("title")
        link_el = item.find("link")
        if title_el is None or link_el is None:
            continue
        
        title = normalize(title_el.text or "")
        link = (link_el.text or "").strip()
        
        if TITLE_KEY not in title:
            continue
        
        label = parse_period(title)
        if label:
            items.append((label, link))
    
    if not items:
        raise RSSFeedError("RSS 中未找到匹配条目", feed_url=RSS_URL)
    
    items.sort(key=lambda x: x[0])
    return items


def looks_like_header(cells: List[str]) -> bool:
    """判断是否为表头行"""
    for c in cells:
        n = normalize(c)
        if any(kw in n for kw in ("城市", "环比", "同比", "定基")):
            return True
    return False


def contains_category(cells: List[str]) -> bool:
    """判断是否包含分类"""
    return any("分类" in normalize(c) for c in cells)


def detect_indicator(table, preceding_text: List[str]) -> Optional[str]:
    """检测表格对应的指标类型"""
    for text in reversed(preceding_text):
        t = compact(text)
        if compact(INDICATORS["new"]) in t and compact(INDICATORS["new_cat"]) not in t:
            return INDICATORS["new"]
        if compact(INDICATORS["used"]) in t and compact(INDICATORS["used_cat"]) not in t:
            return INDICATORS["used"]
    
    rows = table.find_all("tr")[:2]
    head_text = compact(" ".join(r.get_text() for r in rows))
    if compact(INDICATORS["new"]) in head_text and compact(INDICATORS["new_cat"]) not in head_text:
        return INDICATORS["new"]
    if compact(INDICATORS["used"]) in head_text and compact(INDICATORS["used_cat"]) not in head_text:
        return INDICATORS["used"]
    
    return None


def extract_row(tr) -> List[str]:
    """提取表格行的文本"""
    return [normalize(td.get_text()) for td in tr.find_all(["th", "td"]) if normalize(td.get_text())]


def find_header(table) -> List[str]:
    """查找表头行"""
    for tr in table.find_all("tr"):
        row = extract_row(tr)
        if row and looks_like_header(row):
            return row
    return []


def pick_city_segment(
    row: List[str], 
    header: List[str], 
    city: str
) -> Tuple[Optional[List[str]], Optional[List[str]]]:
    """在可能包含多城市的行中找到目标城市的段落"""
    if not row:
        return None, None
    
    if header:
        city_cols = [i for i, h in enumerate(header) if "城市" in normalize(h)]
        if city_cols:
            for idx, start in enumerate(city_cols):
                end = city_cols[idx + 1] if idx + 1 < len(city_cols) else len(header)
                seg_row = row[start:min(end, len(row))]
                if any(compact(city) in compact(c) for c in seg_row):
                    return header[start:end], seg_row
    
    h = len(header)
    if h > 0 and len(row) % h == 0:
        for i in range(0, len(row), h):
            seg = row[i:i + h]
            if any(compact(city) in compact(c) for c in seg):
                return header, seg
    
    if any(compact(city) in compact(c) for c in row):
        return header, row
    
    return None, None


def extract_city_metrics(
    seg_header: List[str],
    seg_row: List[str],
    target_city: str,
    target_metrics: List[str]
) -> Tuple[str, Dict[str, Optional[float]]]:
    """提取城市指标数据"""
    n = min(len(seg_header), len(seg_row))
    city = ""
    metrics = {m: None for m in target_metrics}
    
    for i in range(n):
        k = normalize(seg_header[i])
        v = normalize(seg_row[i])
        if not k or not v:
            continue
        
        if any(kw in k for kw in ("城市", "地区", "城市名称")):
            if not city or compact(target_city) in compact(v):
                city = v
            continue
        
        if "分类" in k:
            continue
        
        matched = [m for m in target_metrics if m in k]
        if not matched:
            continue
        
        val = parse_number(v)
        for m in matched:
            metrics[m] = val
    
    if not city:
        for c in seg_row:
            if compact(target_city) in compact(c):
                city = c
                break
    
    return city, metrics


def parse_page(
    content: bytes,
    period_label: str,
    target_city: str,
    target_metrics: List[str],
    source_url: str = ""
) -> List[Dict]:
    """解析文章页面"""
    soup = BeautifulSoup(content, "html.parser")
    
    tables = soup.select(".detail-text-content .txt-content .trs_editor_view table")
    if not tables:
        tables = soup.select(".trs_editor_view table")
    if not tables:
        tables = soup.find_all("table")
    
    records = {}
    
    for table in tables:
        preceding = []
        node = table
        for _ in range(4):
            count = 0
            for sib in node.find_previous_siblings():
                text = normalize(sib.get_text())
                if text:
                    preceding.insert(0, text)
                    count += 1
                    if count >= 4:
                        break
            node = node.parent
            if node is None:
                break
        
        indicator = detect_indicator(table, preceding)
        if not indicator:
            continue
        
        header = find_header(table)
        if not header or contains_category(header):
            continue
        
        for tr in table.find_all("tr"):
            row = extract_row(tr)
            if not row or looks_like_header(row):
                continue
            
            seg_header, seg_row = pick_city_segment(row, header, target_city)
            if not seg_row or not seg_header:
                continue
            
            city, metrics = extract_city_metrics(seg_header, seg_row, target_city, target_metrics)
            if compact(target_city) not in compact(city):
                continue
            if not any(v is not None for v in metrics.values()):
                continue
            
            existing = records.get(indicator)
            non_null = sum(1 for v in metrics.values() if v is not None)
            if existing is None or non_null > sum(1 for v in existing["metrics"].values() if v is not None):
                records[indicator] = {
                    "period": period_label,
                    "indicator": indicator,
                    "metrics": metrics,
                    "source_url": source_url,
                }
    
    return list(records.values())


def validate_params(city: str, metrics: List[str]) -> Tuple[str, List[str], Optional[str]]:
    """验证参数，返回规范化后的参数和错误信息"""
    # 验证城市名
    city = city.strip()
    if not city:
        return city, metrics, "城市名不能为空"
    
    # 验证指标
    valid = []
    invalid = []
    for m in metrics:
        m = m.strip()
        if m in VALID_METRICS:
            valid.append(m)
        else:
            invalid.append(m)
    
    if invalid:
        return city, valid, f"无效指标: {', '.join(invalid)}。有效指标: {', '.join(VALID_METRICS)}"
    
    if not valid:
        return city, metrics, f"至少需要一个有效指标。有效指标: {', '.join(VALID_METRICS)}"
    
    return city, valid, None


def generate_chart(records: List[Dict], city: str, output_path: str = None) -> str:
    """生成分析图表"""
    try:
        import matplotlib.pyplot as plt
        import numpy as np
    except ImportError:
        print(json.dumps({"error": "缺少 matplotlib，请运行: pip install matplotlib"}))
        return None
    
    # 设置中文字体
    plt.rcParams['font.sans-serif'] = ['Arial Unicode MS', 'SimHei', 'STHeiti', 'PingFang SC']
    plt.rcParams['axes.unicode_minus'] = False
    
    # 整理数据
    periods = sorted(set(r['period'] for r in records))
    
    new_huanbi = []
    new_tongbi = []
    used_huanbi = []
    used_tongbi = []
    
    for p in periods:
        for r in records:
            if r['period'] == p:
                if r['indicator'] == INDICATORS['new']:
                    new_huanbi.append(r['metrics'].get('环比'))
                    new_tongbi.append(r['metrics'].get('同比'))
                elif r['indicator'] == INDICATORS['used']:
                    used_huanbi.append(r['metrics'].get('环比'))
                    used_tongbi.append(r['metrics'].get('同比'))
    
    # 创建图表
    fig, axes = plt.subplots(2, 2, figsize=(16, 12))
    fig.suptitle(f'{city}住宅销售价格指数分析', fontsize=16, fontweight='bold')
    
    x = range(len(periods))
    x_labels = [p for p in periods]
    
    # 图1：环比指数趋势
    ax1 = axes[0, 0]
    # 过滤 None 值用于绑图
    new_huanbi_valid = [(i, v) for i, v in enumerate(new_huanbi) if v is not None]
    used_huanbi_valid = [(i, v) for i, v in enumerate(used_huanbi) if v is not None]
    
    if new_huanbi_valid:
        x_new, y_new = zip(*new_huanbi_valid)
        ax1.plot(x_new, y_new, 'o-', color='#2E86AB', label='新建商品住宅', linewidth=2, markersize=4)
    if used_huanbi_valid:
        x_used, y_used = zip(*used_huanbi_valid)
        ax1.plot(x_used, y_used, 's-', color='#E94F37', label='二手住宅', linewidth=2, markersize=4)
    ax1.axhline(y=100, color='gray', linestyle='--', alpha=0.5, label='基准线(100)')
    ax1.set_title('环比指数趋势 (上月=100)', fontsize=12)
    ax1.set_xlabel('期次')
    ax1.set_ylabel('指数')
    ax1.legend(loc='lower left')
    ax1.set_ylim(97, 102)
    ax1.set_xticks(x[::3])
    ax1.set_xticklabels(x_labels[::3], rotation=45, ha='right')
    ax1.grid(True, alpha=0.3)
    
    # 图2：同比指数趋势
    ax2 = axes[0, 1]
    new_tongbi_valid = [(i, v) for i, v in enumerate(new_tongbi) if v is not None]
    used_tongbi_valid = [(i, v) for i, v in enumerate(used_tongbi) if v is not None]
    
    if new_tongbi_valid:
        x_new, y_new = zip(*new_tongbi_valid)
        ax2.plot(x_new, y_new, 'o-', color='#2E86AB', label='新建商品住宅', linewidth=2, markersize=4)
    if used_tongbi_valid:
        x_used, y_used = zip(*used_tongbi_valid)
        ax2.plot(x_used, y_used, 's-', color='#E94F37', label='二手住宅', linewidth=2, markersize=4)
    ax2.axhline(y=100, color='gray', linestyle='--', alpha=0.5, label='基准线(100)')
    ax2.set_title('同比指数趋势 (上年同月=100)', fontsize=12)
    ax2.set_xlabel('期次')
    ax2.set_ylabel('指数')
    ax2.legend(loc='lower left')
    ax2.set_ylim(85, 105)
    ax2.set_xticks(x[::3])
    ax2.set_xticklabels(x_labels[::3], rotation=45, ha='right')
    ax2.grid(True, alpha=0.3)
    
    # 图3：同比涨跌幅
    ax3 = axes[1, 0]
    width = 0.35
    new_change = [(v - 100) if v is not None else 0 for v in new_tongbi]
    used_change = [(v - 100) if v is not None else 0 for v in used_tongbi]
    ax3.bar([i - width/2 for i in x], new_change, width, color='#2E86AB', label='新建商品住宅', alpha=0.8)
    ax3.bar([i + width/2 for i in x], used_change, width, color='#E94F37', label='二手住宅', alpha=0.8)
    ax3.axhline(y=0, color='gray', linestyle='-', alpha=0.5)
    ax3.set_title('同比涨跌幅 (%)', fontsize=12)
    ax3.set_xlabel('期次')
    ax3.set_ylabel('涨跌幅 (%)')
    ax3.legend(loc='lower left')
    ax3.set_xticks(x[::3])
    ax3.set_xticklabels(x_labels[::3], rotation=45, ha='right')
    ax3.grid(True, alpha=0.3, axis='y')
    
    # 图4：新房vs二手房差距
    ax4 = axes[1, 1]
    gap = [(n - u) if n is not None and u is not None else 0 for n, u in zip(new_tongbi, used_tongbi)]
    colors = ['#2E86AB' if g > 0 else '#E94F37' for g in gap]
    ax4.bar(x, gap, color=colors, alpha=0.8)
    ax4.axhline(y=0, color='gray', linestyle='-', alpha=0.5)
    ax4.set_title('新房与二手房同比指数差值', fontsize=12)
    ax4.set_xlabel('期次')
    ax4.set_ylabel('差值 (新房 - 二手房)')
    ax4.set_xticks(x[::3])
    ax4.set_xticklabels(x_labels[::3], rotation=45, ha='right')
    ax4.grid(True, alpha=0.3, axis='y')
    
    plt.tight_layout()
    
    # 确定输出路径
    if output_path is None:
        # 默认保存到工作区
        workspace = Path(__file__).parent.parent.parent
        output_path = workspace / f'{city}_housing_analysis.png'
    
    plt.savefig(str(output_path), dpi=150, bbox_inches='tight', facecolor='white')
    plt.close()
    
    return str(output_path)


def main():
    parser = argparse.ArgumentParser(
        description="获取中国70城住宅价格指数数据",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
示例:
  python fetch_data.py
  python fetch_data.py --city 上海 --metrics 同比
  python fetch_data.py --city 北京 --metrics 环比,同比 --limit 50
  python fetch_data.py --city 武汉 --chart
        """
    )
    parser.add_argument(
        "--city", 
        default=DEFAULTS["city"],
        help=f"目标城市名称 (默认: {DEFAULTS['city']})"
    )
    parser.add_argument(
        "--metrics", 
        default=DEFAULTS["metrics"],
        help=f"指标，逗号分隔 (默认: {DEFAULTS['metrics']})"
    )
    parser.add_argument(
        "--limit", 
        type=int, 
        default=DEFAULTS["limit"],
        help=f"最多返回期数 (默认: {DEFAULTS['limit']})"
    )
    parser.add_argument(
        "--no-cache",
        action="store_true",
        help="禁用缓存"
    )
    parser.add_argument(
        "--chart",
        action="store_true",
        help="生成分析图表"
    )
    parser.add_argument(
        "--output",
        type=str,
        default=None,
        help="图表输出路径 (仅与 --chart 配合使用)"
    )
    args = parser.parse_args()
    
    target_city = args.city.strip()
    target_metrics = [m.strip() for m in args.metrics.split(",") if m.strip()]
    
    # 参数验证
    target_city, target_metrics, error = validate_params(target_city, target_metrics)
    if error:
        print(json.dumps({"error": error, "city": target_city, "metrics": target_metrics}, ensure_ascii=False))
        sys.exit(1)
    
    global _cache
    if args.no_cache:
        _cache = None
    
    try:
        rss_items = fetch_rss_items()
    except RSSFeedError as e:
        print(json.dumps({"error": str(e)}, ensure_ascii=False))
        sys.exit(1)
    except NetworkError as e:
        print(json.dumps({"error": str(e)}, ensure_ascii=False))
        sys.exit(1)
    
    selected = rss_items[-args.limit:]
    all_records = []
    
    for period_label, url in selected:
        try:
            content = fetch_url(url)
        except NetworkError:
            continue
        
        records = parse_page(content, period_label, target_city, target_metrics, source_url=url)
        all_records.extend(records)
    
    if not all_records:
        err = CityNotFoundError(target_city)
        print(json.dumps({
            "error": str(err),
            "city": target_city,
            "metrics": target_metrics,
            "hint": "参考 references/REFERENCE.md 中的城市列表",
        }, ensure_ascii=False))
        sys.exit(1)
    
    all_records.sort(key=lambda r: (r["period"], r["indicator"]))
    
    # 生成图表
    if args.chart:
        chart_path = generate_chart(all_records, target_city, args.output)
        if chart_path:
            output = {
                "city": target_city,
                "metrics": target_metrics,
                "records": all_records,
                "items_scanned": len(selected),
                "chart_path": chart_path,
            }
            print(json.dumps(output, ensure_ascii=False, indent=2))
        else:
            sys.exit(1)
    else:
        output = {
            "city": target_city,
            "metrics": target_metrics,
            "records": all_records,
            "items_scanned": len(selected),
        }
        if _cache:
            output["cache_size"] = _cache.size()
        print(json.dumps(output, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
