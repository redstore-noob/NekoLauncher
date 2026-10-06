package world

// level.dat 的 NBT 解析：只为了读 Data.LastPlayed（Unix 毫秒时间戳）。
// 存档的"最近游玩"排序用 NBT 里的真实时间戳，而不是文件修改时间——
// 复制存档、备份同步、网盘拉取都会刷新 mtime，把排序搅乱。
//
// 只实现走到目标字段所需的最小 NBT 读取（其余类型的载荷按长度跳过），
// 不引入第三方 NBT 库。任何解析异常都返回"没读到"，调用方回落 mtime。

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"time"
)

// NBT 标签类型（wiki.vg NBT 规范）。
const (
	nbtTagEnd       byte = 0
	nbtTagByte      byte = 1
	nbtTagShort     byte = 2
	nbtTagInt       byte = 3
	nbtTagLong      byte = 4
	nbtTagFloat     byte = 5
	nbtTagDouble    byte = 6
	nbtTagByteArray byte = 7
	nbtTagString    byte = 8
	nbtTagList      byte = 9
	nbtTagCompound  byte = 10
	nbtTagIntArray  byte = 11
	nbtTagLongArray byte = 12
)

// levelDatMaxDecompressedSize 解压上限：原版 level.dat 通常几十 KB，
// 上限只为挡住损坏/伪造文件的解压炸弹。
const levelDatMaxDecompressedSize = 8 << 20

type nbtReader struct {
	data []byte
	pos  int
}

func (r *nbtReader) need(n int) error {
	if n < 0 || r.pos+n > len(r.data) {
		return errors.New("NBT 数据越界")
	}
	return nil
}

func (r *nbtReader) u8() (byte, error) {
	if err := r.need(1); err != nil {
		return 0, err
	}
	value := r.data[r.pos]
	r.pos++

	return value, nil
}

func (r *nbtReader) u16() (uint16, error) {
	if err := r.need(2); err != nil {
		return 0, err
	}
	value := binary.BigEndian.Uint16(r.data[r.pos:])
	r.pos += 2

	return value, nil
}

func (r *nbtReader) i32() (int32, error) {
	if err := r.need(4); err != nil {
		return 0, err
	}
	value := int32(binary.BigEndian.Uint32(r.data[r.pos:]))
	r.pos += 4

	return value, nil
}

func (r *nbtReader) i64() (int64, error) {
	if err := r.need(8); err != nil {
		return 0, err
	}
	value := int64(binary.BigEndian.Uint64(r.data[r.pos:]))
	r.pos += 8

	return value, nil
}

// nbtString 读一个 NBT 字符串（u16 长度 + UTF-8 字节）。
func (r *nbtReader) nbtString() (string, error) {
	length, err := r.u16()
	if err != nil {
		return "", err
	}
	if err := r.need(int(length)); err != nil {
		return "", err
	}
	value := string(r.data[r.pos : r.pos+int(length)])
	r.pos += int(length)

	return value, nil
}

// skipPayload 按类型跳过一个载荷（复合/列表递归跳过），只在读 LastPlayed
// 的路上扫过无关字段时使用。
func (r *nbtReader) skipPayload(tagType byte) error {
	switch tagType {
	case nbtTagByte:
		return r.skipBytes(1)
	case nbtTagShort, nbtTagFloat:
		return r.skipBytes(2)
	case nbtTagInt, nbtTagDouble:
		return r.skipBytes(4)
	case nbtTagLong:
		return r.skipBytes(8)
	case nbtTagByteArray:
		length, err := r.i32()
		if err != nil {
			return err
		}
		if length < 0 {
			return errors.New("NBT 数组长度为负")
		}
		return r.skipBytes(int(length))
	case nbtTagIntArray:
		length, err := r.i32()
		if err != nil {
			return err
		}
		if length < 0 {
			return errors.New("NBT 数组长度为负")
		}
		return r.skipBytes(int(length) * 4)
	case nbtTagLongArray:
		length, err := r.i32()
		if err != nil {
			return err
		}
		if length < 0 {
			return errors.New("NBT 数组长度为负")
		}
		return r.skipBytes(int(length) * 8)
	case nbtTagString:
		length, err := r.u16()
		if err != nil {
			return err
		}
		return r.skipBytes(int(length))
	case nbtTagList:
		elemType, err := r.u8()
		if err != nil {
			return err
		}
		count, err := r.i32()
		if err != nil {
			return err
		}
		if count < 0 {
			return errors.New("NBT 列表长度为负")
		}
		for i := 0; i < int(count); i++ {
			if err := r.skipPayload(elemType); err != nil {
				return err
			}
		}
		return nil
	case nbtTagCompound:
		for {
			childType, err := r.u8()
			if err != nil {
				return err
			}
			if childType == nbtTagEnd {
				return nil
			}
			if _, err := r.nbtString(); err != nil {
				return err
			}
			if err := r.skipPayload(childType); err != nil {
				return err
			}
		}
	default:
		return errors.New("未知 NBT 标签类型")
	}
}

func (r *nbtReader) skipBytes(n int) error {
	if err := r.need(n); err != nil {
		return err
	}
	r.pos += n

	return nil
}

// readLevelDatLastPlayed 读 level.dat（gzip 压缩的 NBT）里的 Data.LastPlayed。
// 读不到（文件缺失、格式不符、字段不存在、时间戳非法）一律返回 false，
// 由调用方回落到文件修改时间。
func readLevelDatLastPlayed(path string) (time.Time, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, false
	}
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return time.Time{}, false
	}
	defer gz.Close()

	data, err := io.ReadAll(io.LimitReader(gz, levelDatMaxDecompressedSize+1))
	if err != nil || len(data) > levelDatMaxDecompressedSize {
		return time.Time{}, false
	}

	millis, err := (&nbtReader{data: data}).lastPlayedMillis()
	if err != nil || millis <= 0 {
		return time.Time{}, false
	}

	return time.UnixMilli(millis), true
}

// lastPlayedMillis 解析根复合结构，定位 Data.LastPlayed（TAG_Long，Unix 毫秒）。
// 根下的其他字段全部跳过；Data 的子复合结构（如 WorldGenSettings）整体跳过，
// 里面即便有同名字段也不会误读。
func (r *nbtReader) lastPlayedMillis() (int64, error) {
	tagType, err := r.u8()
	if err != nil {
		return 0, err
	}
	if tagType != nbtTagCompound {
		return 0, errors.New("level.dat 根不是 TAG_Compound")
	}
	if _, err := r.nbtString(); err != nil { // 根名（ vanilla 为空串）
		return 0, err
	}

	for {
		childType, err := r.u8()
		if err != nil {
			return 0, err
		}
		if childType == nbtTagEnd {
			return 0, errors.New("level.dat 里没有 Data.LastPlayed")
		}
		name, err := r.nbtString()
		if err != nil {
			return 0, err
		}
		if childType == nbtTagCompound && name == "Data" {
			return r.lastPlayedFromDataCompound()
		}
		if err := r.skipPayload(childType); err != nil {
			return 0, err
		}
	}
}

// lastPlayedFromDataCompound 在 Data 复合结构的直接子级里找 LastPlayed。
func (r *nbtReader) lastPlayedFromDataCompound() (int64, error) {
	for {
		childType, err := r.u8()
		if err != nil {
			return 0, err
		}
		if childType == nbtTagEnd {
			return 0, errors.New("Data 里没有 LastPlayed")
		}
		name, err := r.nbtString()
		if err != nil {
			return 0, err
		}
		if childType == nbtTagLong && name == "LastPlayed" {
			return r.i64()
		}
		if err := r.skipPayload(childType); err != nil {
			return 0, err
		}
	}
}
