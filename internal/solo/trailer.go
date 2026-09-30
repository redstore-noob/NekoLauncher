// NekoSolo 安装包（Windows 专用）：
//
//	最终 exe = 安装器模板（stub，C# WPF）+ 载荷 zip + 32 字节尾标。
//	载荷 zip 内含启动器本体、portable.flag、整合包（版本目录 + 内容）、
//	可选的捆绑 Java 运行时与 manifest.json 元数据。
//	安装器只读自身文件尾部即可定位载荷并流式解压（不用把整个 exe 读进内存），
//	安装完成后写 portable.flag 与 neko-solo.json，启动器首启时由
//	ApplyStartupDefaults 消费标记（开 S 模式、注册游戏目录、绑定捆绑 Java）。
//
// 尾标结构（全部小端）：
//
//	[0:8]   magic "NKSOLO\x01"
//	[8:16]  载荷在最终文件中的偏移
//	[16:24] 载荷长度
//	[24:28] 载荷 CRC32（IEEE，校验下载损坏）
//	[28:32] 保留（写 0）
package solo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// soloMagic 尾标魔数（8 字节）。带版本字节：格式破坏性变更时递增。
const soloMagic = "NKSOLO1\x01"

// soloMagicV2 在线安装包（v2）的尾标魔数：offset/length 指向 exe 内嵌的
// remote-manifest.json（载荷 zip 改为发布在 GitHub Releases 等外部地址，
// 安装时由安装器下载），CRC32 为该清单 JSON 的校验。
const soloMagicV2 = "NKSOLO2\x02"

// trailerSize 尾标总长（字节）。
const trailerSize = 32

// Trailer 从最终安装包尾部解析出的载荷寻址信息。
type Trailer struct {
	Offset int64
	Length int64
	CRC32  uint32
	// V2 为 true 表示在线安装包：Offset/Length 指向内嵌远程清单而非载荷 zip
	V2 bool
}

// AppendTrailer 把尾标写到 w（导出与测试共用）。
func AppendTrailer(w io.Writer, payloadOffset, payloadLength int64, crc uint32) error {
	return appendTrailer(w, soloMagic, payloadOffset, payloadLength, crc)
}

// AppendTrailerV2 写 v2（在线安装包）尾标：Offset/Length/CRC 描述内嵌的远程清单。
func AppendTrailerV2(w io.Writer, manifestOffset, manifestLength int64, crc uint32) error {
	return appendTrailer(w, soloMagicV2, manifestOffset, manifestLength, crc)
}

func appendTrailer(w io.Writer, magic string, offset, length int64, crc uint32) error {
	buf := make([]byte, trailerSize)
	copy(buf[0:8], magic)
	binary.LittleEndian.PutUint64(buf[8:16], uint64(offset))
	binary.LittleEndian.PutUint64(buf[16:24], uint64(length))
	binary.LittleEndian.PutUint32(buf[24:28], crc)
	// [28:32] 保留位保持 0
	_, err := w.Write(buf)
	return err
}

// ParseTrailer 从 32 字节尾标数据解析；魔数不符返回错误。v1/v2 均可解析，
// 是否为在线安装包由 V2 字段区分。
func ParseTrailer(data []byte) (Trailer, error) {
	if len(data) != trailerSize {
		return Trailer{}, fmt.Errorf("尾标长度异常：%d（应为 %d）", len(data), trailerSize)
	}
	magic := string(data[0:8])
	if magic != soloMagic && magic != soloMagicV2 {
		return Trailer{}, errors.New("不是有效的 NekoSolo 安装包（尾标魔数不符）")
	}
	return Trailer{
		Offset: int64(binary.LittleEndian.Uint64(data[8:16])),
		Length: int64(binary.LittleEndian.Uint64(data[16:24])),
		CRC32:  binary.LittleEndian.Uint32(data[24:28]),
		V2:     magic == soloMagicV2,
	}, nil
}

// ReadTrailerFromFile 读取文件尾部的 32 字节并解析。
//
// 保留原因：它是导出侧的唯一校验入口 —— export_test.go 用它从产出的 exe
// 反查载荷位置，从而验证 AppendTrailer 真正写出的字节是对的。删掉它，
// 导出格式就再没有任何东西把关（C# 安装器在另一个仓库/语言里，CI 跑不到）。
func ReadTrailerFromFile(path string) (Trailer, error) {
	file, err := os.Open(path)
	if err != nil {
		return Trailer{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return Trailer{}, err
	}
	if info.Size() < int64(trailerSize) {
		return Trailer{}, errors.New("文件太小，不可能是 NekoSolo 安装包")
	}
	if _, err := file.Seek(-int64(trailerSize), io.SeekEnd); err != nil {
		return Trailer{}, err
	}
	data := make([]byte, trailerSize)
	if _, err := io.ReadFull(file, data); err != nil {
		return Trailer{}, err
	}
	return ParseTrailer(data)
}

// OpenPayloadRange 打开 path 并把读取位置定位到载荷起点；
// 返回的 reader 从载荷开头开始、恰好可读 Length 字节。调用方负责 Close。
//
// 与 ReadTrailerFromFile 同理保留：export_test.go 靠它把产出的 exe 里的载荷
// 区间当作 zip 打开，验证导出布局（files/ / minecraft/ / jre/）真的写对了。
func OpenPayloadRange(path string, trailer Trailer) (io.ReadCloser, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(trailer.Offset, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	return &limitedCloser{Reader: io.LimitReader(file, trailer.Length), inner: file}, nil
}

// limitedCloser 让 io.LimitReader 具备关闭底层文件的能力。
type limitedCloser struct {
	io.Reader
	inner io.Closer
}

func (l *limitedCloser) Close() error { return l.inner.Close() }
