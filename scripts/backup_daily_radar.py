#!/usr/bin/env python3
"""将 SQLite 一致性快照与其引用的不可变日报资产备份到全新目录，不连接远程主机。"""

import argparse
import hashlib
import json
from pathlib import Path
import shutil
import sqlite3


def backup(database: Path, assets: Path, output: Path) -> dict:
    """先固定数据库视图，再复制其中引用的资产；缺失或 hash 不符时拒绝半份备份。"""
    database = database.resolve(strict=True)
    assets = assets.resolve(strict=True)
    output.mkdir(parents=True, exist_ok=False)
    try:
        source = sqlite3.connect(database.as_uri() + "?mode=ro", uri=True)
        snapshot = sqlite3.connect(output / "sharing.db")
        try:
            source.backup(snapshot)
            # backup 会保留源库 WAL 模式；目标改为 DELETE，确保单个 .db 可独立恢复。
            snapshot.execute("PRAGMA journal_mode=DELETE")
            if snapshot.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
                raise ValueError("SQLite integrity check failed")
            rows = snapshot.execute(
                "SELECT share_id, kind, path, content_hash, bytes FROM daily_radar_share_assets"
            ).fetchall()
        finally:
            source.close()
            snapshot.close()
        manifest = {"schema_version": 1, "database": "sharing.db", "assets": []}
        for share_id, kind, relative, expected_hash, expected_size in rows:
            filename = {"hero-webp": "hero.webp", "hero-png": "hero.png"}[kind]
            path = Path(relative)
            if not share_id or Path(share_id).name != share_id or path != Path(share_id) / filename:
                raise ValueError("Invalid stored asset path")
            destination = output / "daily-radar" / path
            destination.parent.mkdir(parents=True, exist_ok=True)
            # 资产发布后不可改写，因此在固定 DB 之后复制也属于同一个内容快照。
            shutil.copyfile(assets / path, destination)
            digest = hashlib.sha256()
            with destination.open("rb") as stream:
                for chunk in iter(lambda: stream.read(65536), b""):
                    digest.update(chunk)
            if destination.stat().st_size != expected_size or digest.hexdigest() != expected_hash:
                raise ValueError("Asset checksum mismatch: " + relative)
            manifest["assets"].append({"path": str(Path("daily-radar") / path), "sha256": expected_hash, "bytes": expected_size})
        (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
        return manifest
    except BaseException:
        # 只移除本次新建的 output；已存在目录在 mkdir 时即被拒绝，不会进入清理分支。
        shutil.rmtree(output)
        raise


def main():
    """路径必须显式传入，避免误读默认目录或覆盖既有备份。"""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--database", required=True, type=Path)
    parser.add_argument("--assets", required=True, type=Path)
    parser.add_argument("--output", required=True, type=Path)
    arguments = parser.parse_args()
    result = backup(arguments.database, arguments.assets, arguments.output)
    print(f"Backup complete: {len(result['assets'])} verified assets")


if __name__ == "__main__":
    main()
