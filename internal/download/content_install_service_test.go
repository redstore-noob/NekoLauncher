package download

import (
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestMatchesDeclaredHashes 重试跳过重下的判定：哈希相符跳过、不符不跳、
// 清单没声明（或写的是占位符）时退化为"文件非空即认"。
func TestMatchesDeclaredHashes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mod.jar")
	content := []byte("fake mod jar content for hash matching test")
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("写测试文件失败：%v", err)
	}

	sum1 := sha1.Sum(content)
	hex1 := hex.EncodeToString(sum1[:])
	sum512 := sha512.Sum512(content)
	hex512 := hex.EncodeToString(sum512[:])

	cases := []struct {
		name               string
		expectedSHA1       string
		expectedSHA512     string
		want               bool
		writeDifferentFile bool
	}{
		{"SHA-1 相符", hex1, "", true, false},
		{"SHA-512 相符", "", hex512, true, false},
		{"双哈希相符", hex1, hex512, true, false},
		{"SHA-1 不符", hex1[:39] + "0", "", false, false},
		{"SHA-512 不符", "", hex512[:127] + "0", false, false},
		{"未声明哈希视为可用", "", "", true, false},
		{"占位符哈希视为未声明", "placeholder", "deadbeef", true, false},
		{"内容被改动后不符", hex1, hex512, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := path
			if tc.writeDifferentFile {
				target = filepath.Join(dir, "tampered.jar")
				if err := os.WriteFile(target, []byte("tampered!"), 0o644); err != nil {
					t.Fatalf("写篡改文件失败：%v", err)
				}
			}
			if got := matchesDeclaredHashes(target, tc.expectedSHA1, tc.expectedSHA512); got != tc.want {
				t.Fatalf("matchesDeclaredHashes = %v，期望 %v", got, tc.want)
			}
		})
	}
}
