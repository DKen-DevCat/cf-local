// Package main is the cf-local Control Plane entry point.
//
// Phase 3 A.3a (skeleton): --config-dir フラグを受け取り、ディレクトリの
// 存在を確認して起動メッセージを stdout に書き出すだけ。設定 loader は
// A.3b で internal/config パッケージとして追加し、nginx renderer は A.4
// で internal/nginx に追加する。
//
// 起動例:
//
//	cf-local --config-dir ./cf-local
//
// `./cf-local/cache-policies/*.json` と `./cf-local/distributions/main.json`
// が配置されている前提 (A.3b 以降で実際に読み込む)。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
)

const (
	defaultConfigDir = "./cf-local"
	version          = "0.0.0-phase3-a3a"
)

func main() {
	configDir := flag.String("config-dir", defaultConfigDir, "directory containing cache-policies/ and distributions/ JSON files")
	flag.Parse()

	if err := run(*configDir, os.Stdout); err != nil {
		log.SetFlags(0)
		log.Fatalf("cf-local: %v", err)
	}
}

func run(configDir string, stdout *os.File) error {
	info, err := os.Stat(configDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("config dir %q does not exist", configDir)
		}
		return fmt.Errorf("config dir %q: %w", configDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("config dir %q is not a directory", configDir)
	}

	fmt.Fprintf(stdout, "cf-local %s — config dir: %s\n", version, configDir)
	fmt.Fprintln(stdout, "(phase 3 A.3a: skeleton; loader / renderer not yet wired)")
	return nil
}
