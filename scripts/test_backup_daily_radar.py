"""验证日报资产与 SQLite 的成套备份，以及失败时不留下可误用的备份目录。"""

import hashlib
from pathlib import Path
import sqlite3
import tempfile
import unittest

from backup_daily_radar import backup


class BackupDailyRadarTests(unittest.TestCase):
    """仅使用临时目录，覆盖 WAL、孤儿资产、hash 校验与现有备份保护。"""

    def test_snapshot_matches_assets_and_preserves_existing_output(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            database = root / "sharing.db"
            assets = root / "assets"
            (assets / "fixture").mkdir(parents=True)
            image = b"immutable-asset"
            (assets / "fixture/hero.png").write_bytes(image)
            (assets / "orphan.png").write_bytes(b"not referenced")
            connection = sqlite3.connect(database)
            self.addCleanup(connection.close)
            connection.execute("PRAGMA journal_mode=WAL")
            connection.execute("CREATE TABLE daily_radar_share_assets (share_id TEXT, kind TEXT, path TEXT, content_hash TEXT, bytes INTEGER)")
            connection.execute(
                "INSERT INTO daily_radar_share_assets VALUES (?, ?, ?, ?, ?)",
                ("fixture", "hero-png", "fixture/hero.png", hashlib.sha256(image).hexdigest(), len(image)),
            )
            connection.commit()
            output = root / "backup"
            result = backup(database, assets, output)
            self.assertEqual(len(result["assets"]), 1)
            self.assertEqual((output / "daily-radar/fixture/hero.png").read_bytes(), image)
            self.assertFalse((output / "daily-radar/orphan.png").exists())
            self.assertFalse((output / "sharing.db-wal").exists())
            with self.assertRaises(FileExistsError):
                backup(database, assets, output)
            self.assertTrue((output / "manifest.json").exists())

            (assets / "fixture/hero.png").write_bytes(b"corrupt")
            with self.assertRaisesRegex(ValueError, "checksum mismatch"):
                backup(database, assets, root / "bad")
            self.assertFalse((root / "bad").exists())

            (assets / "fixture/hero.png").unlink()
            with self.assertRaises(FileNotFoundError):
                backup(database, assets, root / "missing")
            self.assertFalse((root / "missing").exists())


if __name__ == "__main__":
    unittest.main()
