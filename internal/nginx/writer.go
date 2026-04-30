package nginx

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteAtomic は contents を outDir/<name> に atomic に書き出す。
//
// アルゴリズム:
//
//  1. tmp file (outDir/.<name>.tmp) に O_CREATE|O_TRUNC|O_WRONLY で書き込む
//  2. f.Sync() で data + meta を flush
//  3. rename(tmp, outDir/<name>) — POSIX 規約で同一 fs 内 rename は atomic
//
// 中断時の状態:
//
//   - step 1〜2 中で死亡: outDir/<name> は変更されない (旧バージョンが残る)
//   - step 3 後に死亡  : outDir/<name> は新内容に切り替わっている
//
// nginx container 側の inotify sidecar は MOVED_TO のみを watch するため
// (3-5 spike 確認済 — `nginx/spike/README.md`)、CREATE / WRITE 中の半端な
// 状態を nginx が読みに行くことはない。
//
// tmp file 名の leading dot (`.<name>.tmp`) は nginx の
// `include /etc/nginx/cf-local/*.conf;` glob に拾われないようにするための
// 一重保険。仮に inotify watch を通り抜けても include は dotfile を skip する。
//
// 入力契約:
//   - outDir は事前に存在する dir であること (本関数は MkdirAll しない)
//   - name は base name (`cf-local.conf` 等)。`/` を含む場合は error
//
// stale tmp file (前回失敗で残ったもの) は O_TRUNC で上書きされるため
// 自動で回復する。明示的な事前削除は不要。
func WriteAtomic(outDir, name string, contents []byte) error {
	if name == "" {
		return fmt.Errorf("WriteAtomic: name is empty")
	}
	if filepath.Base(name) != name {
		return fmt.Errorf("WriteAtomic: name %q must be a base name (no path separators)", name)
	}

	finalPath := filepath.Join(outDir, name)
	tmpPath := filepath.Join(outDir, "."+name+".tmp")

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("WriteAtomic: open %s: %w", tmpPath, err)
	}
	// クローズ漏れ + tmpfile 残置を確実に防ぐ。本関数の正常系では下で f.Close() が
	// 呼ばれるが、Write/Sync で途中失敗した場合のための保険。
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(tmpPath)
	}

	if _, err := f.Write(contents); err != nil {
		cleanup()
		return fmt.Errorf("WriteAtomic: write %s: %w", tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("WriteAtomic: fsync %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("WriteAtomic: close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("WriteAtomic: rename %s -> %s: %w", tmpPath, finalPath, err)
	}
	return nil
}
