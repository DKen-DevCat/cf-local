package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAtomic_NewFile(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAtomic(dir, "cf-local.conf", []byte("hello\n")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "cf-local.conf"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("contents = %q, want %q", got, "hello\n")
	}
	// tmp file は rename で消えているはず。
	if _, err := os.Stat(filepath.Join(dir, ".cf-local.conf.tmp")); !os.IsNotExist(err) {
		t.Errorf("tmp file should not exist after WriteAtomic, stat err = %v", err)
	}
}

func TestWriteAtomic_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "policies.json")
	if err := os.WriteFile(target, []byte("old contents"), 0o644); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := WriteAtomic(dir, "policies.json", []byte("new contents")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new contents" {
		t.Errorf("contents = %q, want %q", got, "new contents")
	}
}

func TestWriteAtomic_RecoversFromStaleTmp(t *testing.T) {
	// 前回 fsync で死んだ等で残った .<name>.tmp があっても、O_TRUNC で
	// 上書きされて WriteAtomic は成功する。
	dir := t.TempDir()
	stalePath := filepath.Join(dir, ".cf-local.conf.tmp")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if err := WriteAtomic(dir, "cf-local.conf", []byte("fresh")); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "cf-local.conf"))
	if string(got) != "fresh" {
		t.Errorf("contents = %q, want %q", got, "fresh")
	}
}

func TestWriteAtomic_RejectsPathInName(t *testing.T) {
	dir := t.TempDir()
	cases := []string{
		"sub/x.conf",
		"../escape.conf",
		"/abs.conf",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			err := WriteAtomic(dir, name, []byte("x"))
			if err == nil {
				t.Fatalf("WriteAtomic(%q) should reject path separators", name)
			}
			if !strings.Contains(err.Error(), "must be a base name") {
				t.Errorf("err = %v, want substring 'must be a base name'", err)
			}
		})
	}
}

func TestWriteAtomic_EmptyName(t *testing.T) {
	dir := t.TempDir()
	if err := WriteAtomic(dir, "", []byte("x")); err == nil {
		t.Fatal("WriteAtomic with empty name should error")
	}
}

func TestWriteAtomic_NonexistentDir(t *testing.T) {
	// outDir が存在しない場合は os.OpenFile が ENOENT を返すはず。
	if err := WriteAtomic("/no/such/dir", "x.conf", []byte("x")); err == nil {
		t.Fatal("WriteAtomic to nonexistent dir should error")
	}
}
