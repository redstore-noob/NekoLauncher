package solo

// 尾标编码/解析与载荷定位的单元测试。

import (
	"bytes"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
)

func TestTrailerRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	if err := AppendTrailer(&buffer, 123456, 7890, 0xDEADBEEF); err != nil {
		t.Fatalf("AppendTrailer 失败：%v", err)
	}
	if buffer.Len() != trailerSize {
		t.Fatalf("尾标长度 %d，应为 %d", buffer.Len(), trailerSize)
	}
	trailer, err := ParseTrailer(buffer.Bytes())
	if err != nil {
		t.Fatalf("ParseTrailer 失败：%v", err)
	}
	if trailer.Offset != 123456 || trailer.Length != 7890 || trailer.CRC32 != 0xDEADBEEF {
		t.Fatalf("尾标字段不符：%+v", trailer)
	}
}

func TestParseTrailerRejectsBadMagic(t *testing.T) {
	data := make([]byte, trailerSize)
	if _, err := ParseTrailer(data); err == nil {
		t.Fatal("全零数据不应通过魔数校验")
	}
	if _, err := ParseTrailer(make([]byte, 8)); err == nil {
		t.Fatal("长度不足时不应解析成功")
	}
}

func TestReadTrailerFromFile(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "Setup.exe")

	stub := bytes.Repeat([]byte{0x4D, 0x5A}, 64) // 假 PE 头
	payload := []byte("payload-zip-bytes")
	crc := crc32.ChecksumIEEE(payload)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("创建测试文件失败：%v", err)
	}
	if _, err := file.Write(stub); err != nil {
		t.Fatalf("写入 stub 失败：%v", err)
	}
	if _, err := file.Write(payload); err != nil {
		t.Fatalf("写入载荷失败：%v", err)
	}
	if err := AppendTrailer(file, int64(len(stub)), int64(len(payload)), crc); err != nil {
		t.Fatalf("写尾标失败：%v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("关闭文件失败：%v", err)
	}

	trailer, err := ReadTrailerFromFile(path)
	if err != nil {
		t.Fatalf("ReadTrailerFromFile 失败：%v", err)
	}
	if trailer.Offset != int64(len(stub)) || trailer.Length != int64(len(payload)) || trailer.CRC32 != crc {
		t.Fatalf("尾标字段不符：%+v", trailer)
	}

	// 载荷定位读回应与原载荷逐字节一致，且 CRC 对得上
	reader, err := OpenPayloadRange(path, trailer)
	if err != nil {
		t.Fatalf("OpenPayloadRange 失败：%v", err)
	}
	defer reader.Close()
	got := make([]byte, 0, len(payload))
	buf := make([]byte, 7)
	for {
		n, err := reader.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("载荷读回不符：%q", got)
	}
	if crc32.ChecksumIEEE(got) != trailer.CRC32 {
		t.Fatal("载荷 CRC 校验不一致")
	}
}
