#!/usr/bin/env python3
"""One-time offline, read-only source SQLite -> EMPTY MySQL trading tables import."""
import argparse
import os
import sqlite3
import sys
from urllib.parse import quote

try:
    import pymysql
except ImportError:
    sys.exit("Offline migration requires: python3 -m pip install pymysql")

COLUMNS = {
    "trading_history": ("id", "timestamp", "symbol", "provider", "price",
                        "stop_loss", "fast_ema", "slow_ema", "currency", "status", "rawJson"),
    "orders": ("id", "symbol", "side", "type", "tif", "amount", "quantity",
               "price", "limit_price", "stop_price", "fill_price", "fee",
               "timestamp", "filled_at", "status", "reason"),
    "forum_posts": ("id", "author", "title", "content", "symbol", "timestamp", "upvotes"),
}
DEFAULTS = {"provider": "YAHOO", "type": "MARKET", "tif": "GTC", "status": "OPEN",
            "reason": "", "quantity": 0, "amount": 0, "price": 0, "stop_loss": 0,
            "fast_ema": 0, "slow_ema": 0, "fee": 0, "limit_price": 0,
            "stop_price": 0, "fill_price": 0, "filled_at": 0, "upvotes": 0,
            "timestamp": 0, "rawJson": "{}"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sqlite_backup", help="Offline COPY of old .db, not live WAL file")
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()
    old = os.path.abspath(args.sqlite_backup)
    if not os.path.isfile(old):
        sys.exit("SQLite backup does not exist")
    source = sqlite3.connect("file:" + quote(old, safe="/") + "?mode=ro", uri=True)
    source.row_factory = sqlite3.Row
    counts = {}
    for table in COLUMNS:
        counts[table] = source.execute("SELECT COUNT(*) FROM " + table).fetchone()[0]
    print("Old SQLite records:", counts)
    if args.dry_run:
        source.close()
        return
    username = os.environ.get("MIGRATE_MYSQL_USER", "")
    password = os.environ.get("MIGRATE_MYSQL_PASSWORD", "")
    if not username or not password:
        sys.exit("Set MIGRATE_MYSQL_USER and MIGRATE_MYSQL_PASSWORD")
    mysql = pymysql.connect(
        host=os.environ.get("MIGRATE_MYSQL_HOST", "127.0.0.1"),
        port=int(os.environ.get("MIGRATE_MYSQL_PORT", "3306")),
        user=username, password=password,
        database=os.environ.get("MIGRATE_MYSQL_DATABASE", "floweysales_auth"),
        charset="utf8mb4", autocommit=False,
    )
    try:
        with mysql.cursor() as cursor:
            for table in COLUMNS:
                cursor.execute("SELECT COUNT(*) FROM " + table)
                if cursor.fetchone()[0] != 0:
                    sys.exit("MySQL " + table + " is not empty; refusing to overwrite existing data")
        mysql.begin()
        with mysql.cursor() as cursor:
            for table, cols in COLUMNS.items():
                available = [c for c in cols if c in {
                    row[1] for row in source.execute("PRAGMA table_info(" + table + ")")
                }]
                projection = ",".join('"' + c + '"' for c in available)
                rows = source.execute("SELECT " + projection + " FROM " + table + " ORDER BY id")
                statement = "INSERT INTO " + table + " (" + ",".join(cols) + ") VALUES (" + ",".join(["%s"] * len(cols)) + ")"
                imported = 0
                while True:
                    batch = rows.fetchmany(250)
                    if not batch:
                        break
                    converted = []
                    for row in batch:
                        item = dict(zip(available, row))
                        if table == "orders" and not item.get("quantity"):
                            item["quantity"] = item.get("amount", 0)
                        if table == "orders" and item.get("amount") is None:
                            item["amount"] = item["quantity"]
                        converted.append(tuple(item.get(col, DEFAULTS.get(col)) for col in cols))
                    cursor.executemany(statement, converted)
                    imported += len(batch)
                if imported != counts[table]:
                    raise RuntimeError("Import count mismatch: " + table)
            for table in COLUMNS:
                cursor.execute("SELECT COUNT(*) FROM " + table)
                if cursor.fetchone()[0] != counts[table]:
                    raise RuntimeError("MySQL verification mismatch: " + table)
        mysql.commit()
        print("Import complete. Retain backups until values are independently verified.")
    except Exception:
        mysql.rollback()
        raise
    finally:
        mysql.close()
        source.close()


if __name__ == "__main__":
    main()
