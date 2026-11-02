#!/usr/bin/env python3
"""Rebuilds internal/persona/data/reference_portfolios.json from public filings.

  python3 scripts/refresh_reference_portfolios.py "Your Name you@example.com"

The argument is the contact the SEC asks every automated client to send in its
User-Agent. Standard library only. Sources:
  - SEC EDGAR 13F-HR filings (the latest and the one before, for changes)
  - ARK's published daily holdings file for ARKK

13F lists CUSIPs, not tickers, so the CUSIP_TICKERS table below maps the
positions that matter; an unmapped position keeps its name and no symbol.
A 13F covers long US-listed equity only, is filed up to 45 days after the
quarter ends and says nothing about shorts, bonds, cash or non-US holdings.
"""
import csv, io, json, re, sys, urllib.request, xml.etree.ElementTree as ET
from datetime import datetime, timezone

OUT = "internal/persona/data/reference_portfolios.json"
TOP = 30

# persona id -> filer. BlackRock, Inc. (CIK 2012383) replaced the older BlackRock filers in 2024.
FILERS = {
    "owner": {"filer": "Berkshire Hathaway Inc.", "cik": 1067983},
    "quant": {"filer": "Renaissance Technologies LLC", "cik": 1037389},
    "allocator": {"filer": "BlackRock, Inc.", "cik": 2012383},
}
ARKK_URL = "https://assets.ark-funds.com/fund-documents/funds-etf-csv/ARK_INNOVATION_ETF_ARKK_HOLDINGS.csv"

CUSIP_TICKERS = {
    # Berkshire
    "037833100": "AAPL", "025816109": "AXP", "191216100": "KO", "02079K305": "GOOGL", "02079K107": "GOOGL", "060505104": "BAC",
    "166764100": "CVX", "674599105": "OXY", "H1467J104": "CB", "615369105": "MCO", "500754106": "KHC", "23918K108": "DVA",
    "247361702": "DAL", "829933100": "SIRI", "92343E102": "VRSN", "501044101": "KR", "02005N100": "ALLY", "526057104": "LEN",
    "526057302": "LEN", "650111107": "NYT", "14040H105": "COF", "546347105": "LPX", "670346105": "NUE", "55616P104": "M",
    "62944T105": "NVR", "47233W109": "JEF", "23331A109": "DHI",
    # Renaissance and BlackRock
    "67066G104": "NVDA", "30303M102": "META", "458140100": "INTC", "91307C102": "UTHR", "69608A108": "PLTR", "30161Q104": "EXEL",
    "496902404": "KGC", "22788C105": "CRWD", "859241101": "STRL", "023135106": "AMZN", "04010E109": "AGX", "64110L106": "NFLX",
    "88160R101": "TSLA", "007903107": "AMD", "218352102": "CORT", "29786A106": "ETSY", "G7997R103": "STX", "351858105": "FNV",
    "882508104": "TXN", "94419L101": "W", "874039100": "TSM", "45337C102": "INCY", "64125C109": "NBIX", "771049103": "RBLX",
    "958102105": "WDC", "G01767105": "ALKS", "146869102": "CVNA", "11135F101": "AVGO", "375558103": "GILD", "24703L202": "DELL",
    "58155Q103": "MCK", "053332102": "AZO", "872540109": "TJX", "75734B100": "RDDT", "594918104": "MSFT", "595112103": "MU",
    "532457108": "LLY", "46625H100": "JPM", "464287200": "IVV", "084670702": "BRK-B", "038222105": "AMAT", "512807306": "LRCX",
    "478160104": "JNJ", "92826C839": "V", "30231G102": "XOM", "17275R102": "CSCO", "149123101": "CAT", "931142103": "WMT",
    "482480100": "KLAC", "00287Y109": "ABBV", "57636Q104": "MA", "369604301": "GE", "22160K105": "COST", "91324P102": "UNH",
}
NOTES = {
    "owner": [
        "Berkshire's 13F is its listed US equity portfolio only: not its operating businesses, its cash and Treasury bills (over $370 billion at the end of 2025, per the 2025 letter), its Japanese trading-house stakes or its equity-method holdings.",
        "Concentration is the point: the few largest positions dominate, and the 2025 letter says this concentrated approach will continue with limited activity in core holdings.",
    ],
    "quant": [
        "This is Renaissance Technologies' whole 13F, thousands of positions that turn over quickly. A quarter-end snapshot says little about the strategy, and no single position is large.",
        "Renaissance's methods are secret and its founder died in 2024; the snapshot shows what the firm held on one date, not why.",
    ],
    "allocator": [
        "BlackRock's 13F aggregates holdings managed for clients, mostly index funds and ETFs, so it looks like the market itself. It is not a statement of the firm's investment views.",
        "The firm's own stated views come from the BlackRock Investment Institute (the five 'mega forces' and its asset-class tilts), not from this table.",
    ],
    "innovator": [
        "ARKK's holdings as ARK publishes them each day, including private companies and a cash-like Treasury fund. Positions change often; this is one day.",
        "ARKK is the firm's flagship concentrated fund. ARK's other funds hold different mixes.",
    ],
}


def get(url, ua):
    req = urllib.request.Request(url, headers={"User-Agent": ua})
    return urllib.request.urlopen(req, timeout=90).read().decode("utf8", "replace")


def latest_13f(cik, ua, count=2):
    sub = json.loads(get(f"https://data.sec.gov/submissions/CIK{cik:010d}.json", ua))
    r = sub["filings"]["recent"]
    out = []
    for i, form in enumerate(r["form"]):
        if form == "13F-HR":
            out.append({"accession": r["accessionNumber"][i], "filed": r["filingDate"][i], "period": r["reportDate"][i]})
            if len(out) == count:
                break
    return out


def infotable(cik, accession, ua):
    base = f"https://www.sec.gov/Archives/edgar/data/{cik}/{accession.replace('-', '')}/"
    for name in re.findall(r'href="[^"]*?([^/"]+\.xml)"', get(base, ua)):
        if name == "primary_doc.xml":
            continue
        text = get(base + name, ua)
        if "infoTable" not in text:
            continue
        text = re.sub(r'xmlns(:\w+)?="[^"]+"', "", text)
        text = re.sub(r'\s\w+:\w+="[^"]*"', "", text)
        text = re.sub(r"<(/?)\w+:", r"<\1", text)
        rows = {}
        for it in ET.fromstring(text).iter("infoTable"):
            g = lambda t: (it.find(t).text or "") if it.find(t) is not None else ""
            if g("putCall"):
                continue  # options are not shares
            cusip = g("cusip")
            sh = it.find("shrsOrPrnAmt")
            r = rows.setdefault(cusip, {"cusip": cusip, "name": g("nameOfIssuer"), "value": 0, "shares": 0})
            r["value"] += int(g("value") or 0)
            r["shares"] += int(sh.find("sshPrnamt").text) if sh is not None else 0
        return base, list(rows.values())
    raise SystemExit(f"no information table in {base}")


def by_company(rows):
    """Merges share classes the table maps to one symbol; unmapped CUSIPs stay apart."""
    out = {}
    for r in rows:
        symbol = CUSIP_TICKERS.get(r["cusip"], "")
        key = symbol or r["cusip"]
        c = out.setdefault(key, {"symbol": symbol, "name": r["name"].title(), "value": 0, "shares": 0})
        c["value"] += r["value"]
        c["shares"] += r["shares"]
    return out


def stats(weights):
    w = sorted(weights, reverse=True)
    return {"positions": len(w), "top1_pct": round(sum(w[:1]), 2), "top5_pct": round(sum(w[:5]), 2), "top10_pct": round(sum(w[:10]), 2),
            "hhi": round(sum((x / 100) ** 2 for x in w) * 10000)}


def thirteen_f(persona, f, ua):
    filings = latest_13f(f["cik"], ua)
    base, rows = infotable(f["cik"], filings[0]["accession"], ua)
    now = by_company(rows)
    prev = by_company(infotable(f["cik"], filings[1]["accession"], ua)[1]) if len(filings) > 1 else {}
    total = sum(c["value"] for c in now.values())
    ranked = sorted(now.items(), key=lambda kv: -kv[1]["value"])
    positions = []
    for key, c in ranked[:TOP]:
        p = {"symbol": c["symbol"], "name": c["name"], "weight_pct": round(c["value"] / total * 100, 2), "shares": c["shares"]}
        if key in prev and prev[key]["shares"]:
            p["shares_change_pct"] = round((c["shares"] / prev[key]["shares"] - 1) * 100, 1)
        elif prev:
            p["shares_change_pct"] = None  # new since the previous quarter
        positions.append(p)
    # Only a concentrated portfolio says anything by what it sold out of; in a book of
    # thousands of positions a name dropping out is noise.
    exited = [c["name"] for k, c in prev.items() if k not in now] if persona == "owner" else []
    return {
        "persona": persona, "kind": "13f", "filer": f["filer"], "form": "13F-HR", "period": filings[0]["period"], "filed": filings[0]["filed"],
        "accession": filings[0]["accession"], "source_url": base, "total_value_usd": total,
        "stats": stats([c["value"] / total * 100 for c in now.values()]), "positions": positions,
        "exited_since_previous": exited[:10], "notes": NOTES[persona],
    }


def arkk(ua):
    text = get(ARKK_URL, ua)
    rows = [r for r in csv.DictReader(io.StringIO(text)) if r.get("fund") == "ARKK"]
    num = lambda s: float(re.sub(r"[^0-9.\-]", "", s) or 0)
    total = sum(num(r["market value ($)"]) for r in rows)
    positions = []
    for r in sorted(rows, key=lambda r: -num(r["market value ($)"]))[:TOP]:
        positions.append({"symbol": r["ticker"].strip(), "name": r["company"].title(), "weight_pct": round(num(r["market value ($)"]) / total * 100, 2), "shares": int(num(r["shares"]))})
    d = datetime.strptime(rows[0]["date"], "%m/%d/%Y").date().isoformat()
    return {
        "persona": "innovator", "kind": "etf_daily", "filer": "ARK Innovation ETF (ARKK)", "period": d, "filed": d, "source_url": ARKK_URL,
        "total_value_usd": round(total), "stats": stats([num(r["market value ($)"]) / total * 100 for r in rows]),
        "positions": positions, "notes": NOTES["innovator"],
    }


def main():
    if len(sys.argv) < 2 or "@" not in sys.argv[1]:
        raise SystemExit(__doc__)
    ua = sys.argv[1]
    snaps = [thirteen_f(p, f, ua) for p, f in FILERS.items()] + [arkk(ua)]
    doc = {"generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"), "portfolios": snaps}
    with open(OUT, "w") as fh:
        json.dump(doc, fh, indent=1)
        fh.write("\n")
    for s in snaps:
        print(f"{s['persona']:10} {s['filer']:32} as of {s['period']} {s['stats']['positions']} positions, top 5 = {s['stats']['top5_pct']}%")


if __name__ == "__main__":
    main()
