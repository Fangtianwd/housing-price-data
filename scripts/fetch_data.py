#!/usr/bin/env python3
"""
fetch_data.py - 抓取国家统计局70城住宅价格指数数据

用法：
  python fetch_data.py [--city <城市>] [--metrics <环比,同比>] [--limit <N>]

输出：JSON 格式数据到 stdout
"""
import argparse
import json
import re
import sys
import time
import xml.etree.ElementTree as ET

try:
    import requests
    from bs4 import BeautifulSoup
except ImportError:
    print(json.dumps({"error": "缺少依赖包，请运行: pip install requests beautifulsoup4"}))
    sys.exit(1)

RSS_URL = "https://www.stats.gov.cn/sj/zxfb/rss.xml"
TITLE_KEY = "70个大中城市商品住宅销售价格变动情况"
INDICATOR_NEW = "新建商品住宅销售价格指数"
INDICATOR_USED = "二手住宅销售价格指数"
INDICATOR_NEW_CAT = "新建商品住宅销售价格分类指数"
INDICATOR_USED_CAT = "二手住宅销售价格分类指数"

HEADERS = {
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
    "Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
    "Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
    "Referer": "https://www.stats.gov.cn/",
}

PERIOD_RE = re.compile(r"(\d{4})\s*年\s*(\d{1,2})\s*月(?:份)?")
MAX_ATTEMPTS = 3
RETRY_DELAYS = [0.4, 0.8]  # seconds between retries


def fetch_url(url, timeout=20):
    """Fetch URL with retry logic, returns bytes or raises."""
    last_err = None
    for attempt in range(MAX_ATTEMPTS):
        try:
            resp = requests.get(url.strip(), headers=HEADERS, timeout=timeout)
            resp.raise_for_status()
            return resp.content
        except Exception as e:
            last_err = e
            if attempt < len(RETRY_DELAYS):
                time.sleep(RETRY_DELAYS[attempt])
    raise last_err


def normalize(s):
    """Normalize whitespace and HTML entities."""
    import html as html_mod
    s = html_mod.unescape(str(s))
    s = s.replace("\u00a0", " ").replace("\u3000", " ").replace("\t", " ").replace("\n", " ").replace("\r", " ")
    return " ".join(s.split())


def compact(s):
    """Remove all whitespace."""
    return "".join(normalize(s).split())


def parse_number(s):
    """Parse a float from a cell value (tolerates %, commas, Chinese commas)."""
    s = normalize(s).rstrip("%").replace("，", "").replace(",", "").strip()
    if not s or s in ("-", "--"):
        return None
    try:
        return float(s)
    except ValueError:
        return None


def parse_period(title):
    """Extract (year, month) from title string, returns (YYYY-MM label, sort_key) or None."""
    m = PERIOD_RE.search(title)
    if m:
        y, mo = int(m.group(1)), int(m.group(2))
        return f"{y:04d}-{mo:02d}"
    return None


def fetch_rss_items():
    """Fetch RSS and return list of (period_label, url) for matching titles."""
    content = fetch_url(RSS_URL)
    root = ET.fromstring(content)
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
    items.sort(key=lambda x: x[0])
    return items


def looks_like_header(cells):
    """True if this row appears to be a table header."""
    for c in cells:
        n = normalize(c)
        if any(kw in n for kw in ("城市", "环比", "同比", "定基")):
            return True
    return False


def contains_category(cells):
    return any("分类" in normalize(c) for c in cells)


def detect_indicator(table, preceding_text):
    """Determine which indicator (新建/二手) this table belongs to."""
    for text in reversed(preceding_text):
        t = compact(text)
        if compact(INDICATOR_NEW) in t and compact(INDICATOR_NEW_CAT) not in t:
            return INDICATOR_NEW
        if compact(INDICATOR_USED) in t and compact(INDICATOR_USED_CAT) not in t:
            return INDICATOR_USED
    # Fallback: look at first two rows of table
    rows = table.find_all("tr")[:2]
    head_text = compact(" ".join(r.get_text() for r in rows))
    if compact(INDICATOR_NEW) in head_text and compact(INDICATOR_NEW_CAT) not in head_text:
        return INDICATOR_NEW
    if compact(INDICATOR_USED) in head_text and compact(INDICATOR_USED_CAT) not in head_text:
        return INDICATOR_USED
    return None


def extract_row(tr):
    return [normalize(td.get_text()) for td in tr.find_all(["th", "td"]) if normalize(td.get_text())]


def find_header(table):
    for tr in table.find_all("tr"):
        row = extract_row(tr)
        if row and looks_like_header(row):
            return row
    return []


def pick_city_segment(row, header, city):
    """Find the segment in a (possibly multi-city) row that contains the target city."""
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


def extract_city_metrics(seg_header, seg_row, target_city, target_metrics):
    """Extract city name and metric values from a matched segment.

    Metrics that are queried but not found in the row are returned as None
    rather than being omitted, so callers can distinguish "not present in
    source" from "not requested".
    """
    n = min(len(seg_header), len(seg_row))
    city = ""
    # Pre-fill all requested metrics with None
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
            metrics[m] = val  # None if unparseable, float otherwise
    if not city:
        for c in seg_row:
            if compact(target_city) in compact(c):
                city = c
                break
    return city, metrics


def parse_page(content, period_label, target_city, target_metrics, source_url=""):
    """Parse an article page and return list of records."""
    soup = BeautifulSoup(content, "html.parser")

    # Try progressively broader table selectors
    tables = soup.select(".detail-text-content .txt-content .trs_editor_view table")
    if not tables:
        tables = soup.select(".trs_editor_view table")
    if not tables:
        tables = soup.find_all("table")

    records = {}  # key: indicator → record (keep one with most metrics)

    for table in tables:
        # Gather preceding text for indicator detection.
        # Start from the table itself then walk up parent chain, so that
        # inline title elements (siblings of the table) are also captured.
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
            if compact(target_city) not in compact(city) or not any(v is not None for v in metrics.values()):
                continue

            # Keep record with most non-null metrics for this indicator
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


def main():
    parser = argparse.ArgumentParser(description="Fetch Chinese housing price index data")
    parser.add_argument("--city", default="武汉", help="Target city name")
    parser.add_argument("--metrics", default="环比,同比", help="Comma-separated metrics")
    parser.add_argument("--limit", type=int, default=100, help="Max number of periods to return")
    args = parser.parse_args()

    target_city = args.city.strip()
    target_metrics = [m.strip() for m in args.metrics.split(",") if m.strip()]

    try:
        rss_items = fetch_rss_items()
    except Exception as e:
        print(json.dumps({"error": f"RSS 获取失败: {e}"}, ensure_ascii=False))
        sys.exit(1)

    if not rss_items:
        print(json.dumps({"error": "RSS 中未找到匹配条目"}, ensure_ascii=False))
        sys.exit(1)

    # Take the most recent `limit` periods
    selected = rss_items[-args.limit:]
    all_records = []

    for period_label, url in selected:
        try:
            content = fetch_url(url)
        except Exception:
            continue
        records = parse_page(content, period_label, target_city, target_metrics, source_url=url)
        all_records.extend(records)

    if not all_records:
        print(json.dumps({
            "error": f"未找到城市「{target_city}」的数据。请检查城市名称是否正确，或参考 references/REFERENCE.md 中的城市列表。",
            "city": target_city,
            "metrics": target_metrics,
        }, ensure_ascii=False))
        sys.exit(1)

    # Sort by period then indicator
    all_records.sort(key=lambda r: (r["period"], r["indicator"]))

    print(json.dumps({
        "city": target_city,
        "metrics": target_metrics,
        "records": all_records,
        "items_scanned": len(selected),
    }, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
