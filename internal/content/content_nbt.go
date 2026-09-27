package content

import (
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// level.dat（gzip + NBT）读取
// ---------------------------------------------------------------------------

// levelDatValues level.dat 中提取的目标字段。
type levelDatValues struct {
	LevelName   string
	GameVersion string
	LastPlayed  *time.Time
}

// readLevelDat 最小的 NBT 只读解析器：只提取 LevelName / Version.Name / LastPlayed，
// 其余跳过。level.dat = gzip 压缩的 NBT。
func readLevelDat(path string) (levelDatValues, error) {
	var values levelDatValues
	file, err := os.Open(path)
	if err != nil {
		return values, err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return values, err
	}
	defer gzipReader.Close()

	reader := &nbtReader{r: gzipReader}
	rootType, err := reader.readByte()
	if err != nil {
		return values, err
	}
	if rootType != 10 {
		return values, fmt.Errorf("level.dat root is not a compound tag.")
	}
	if _, err := reader.readNbtString(); err != nil {
		return values, err
	}
	var lastPlayed int64
	if err := readNbtCompound(reader, "", &values.LevelName, &values.GameVersion, &lastPlayed, 0); err != nil {
		return values, err
	}
	if lastPlayed > 0 {
		timestamp := time.UnixMilli(lastPlayed)
		values.LastPlayed = &timestamp
	}
	return values, nil
}

// nbtReader 大端序二进制读取器。
type nbtReader struct {
	r io.Reader
}

func (n *nbtReader) readFull(size int) ([]byte, error) {
	buffer := make([]byte, size)
	if _, err := io.ReadFull(n.r, buffer); err != nil {
		return nil, err
	}
	return buffer, nil
}

func (n *nbtReader) readByte() (byte, error) {
	buffer, err := n.readFull(1)
	if err != nil {
		return 0, err
	}
	return buffer[0], nil
}

func (n *nbtReader) readNbtString() (string, error) {
	lengthBytes, err := n.readFull(2)
	if err != nil {
		return "", err
	}
	length := binary.BigEndian.Uint16(lengthBytes)
	buffer, err := n.readFull(int(length))
	if err != nil {
		return "", err
	}
	return string(buffer), nil
}

func (n *nbtReader) readInt32() (int32, error) {
	buffer, err := n.readFull(4)
	if err != nil {
		return 0, err
	}
	return int32(binary.BigEndian.Uint32(buffer)), nil
}

func (n *nbtReader) readInt64() (int64, error) {
	buffer, err := n.readFull(8)
	if err != nil {
		return 0, err
	}
	return int64(binary.BigEndian.Uint64(buffer)), nil
}

// skipBytes 跳过指定字节数（上限 512 MB，防构造文件）。
func (n *nbtReader) skipBytes(count int64) error {
	if count < 0 || count > 512*1024*1024 {
		return fmt.Errorf("NBT payload is too large.")
	}
	if _, err := io.CopyN(io.Discard, n.r, count); err != nil {
		return fmt.Errorf("unexpected end of NBT payload")
	}
	return nil
}

// readNbtCompound 遍历 compound；目标字段按路径后缀（Data.LevelName 等）识别，其余标签跳过。
func readNbtCompound(reader *nbtReader, path string, levelName, gameVersion *string, lastPlayed *int64, depth int) error {
	if depth > 32 {
		return fmt.Errorf("NBT nesting is too deep.")
	}
	for {
		tagType, err := reader.readByte()
		if err != nil {
			return err
		}
		if tagType == 0 {
			return nil
		}
		name, err := reader.readNbtString()
		if err != nil {
			return err
		}
		currentPath := name
		if path != "" {
			currentPath = path + "." + name
		}
		switch {
		case tagType == 8 && strings.HasSuffix(currentPath, "Data.LevelName"):
			text, err := reader.readNbtString()
			if err != nil {
				return err
			}
			*levelName = text
		case tagType == 8 && strings.HasSuffix(currentPath, "Data.Version.Name"):
			text, err := reader.readNbtString()
			if err != nil {
				return err
			}
			*gameVersion = text
		case tagType == 4 && strings.HasSuffix(currentPath, "Data.LastPlayed"):
			value, err := reader.readInt64()
			if err != nil {
				return err
			}
			*lastPlayed = value
		case tagType == 10:
			if err := readNbtCompound(reader, currentPath, levelName, gameVersion, lastPlayed, depth+1); err != nil {
				return err
			}
		default:
			if err := skipNbtPayload(reader, tagType, depth+1); err != nil {
				return err
			}
		}
	}
}

// skipNbtPayload 跳过非目标标签的负载。
// 列表可嵌套列表：与 readNbtCompound 共用同一深度上限，
// 否则构造的 level.dat 会以不可恢复的栈溢出杀死进程。
func skipNbtPayload(reader *nbtReader, tagType byte, depth int) error {
	if depth > 32 {
		return fmt.Errorf("NBT nesting is too deep.")
	}
	switch tagType {
	case 1:
		return reader.skipBytes(1)
	case 2:
		return reader.skipBytes(2)
	case 3, 5:
		return reader.skipBytes(4)
	case 4, 6:
		return reader.skipBytes(8)
	case 7:
		length, err := readNbtArrayLength(reader)
		if err != nil {
			return err
		}
		return reader.skipBytes(int64(length))
	case 8:
		_, err := reader.readNbtString()
		return err
	case 9:
		elementType, err := reader.readByte()
		if err != nil {
			return err
		}
		count, err := reader.readInt32()
		if err != nil {
			return err
		}
		if count < 0 || count > 10_000_000 {
			return fmt.Errorf("Invalid NBT list size.")
		}
		for i := int32(0); i < count; i++ {
			if err := skipNbtPayload(reader, elementType, depth+1); err != nil {
				return err
			}
		}
		return nil
	case 10:
		var unusedName, unusedVersion string
		var unusedTime int64
		return readNbtCompound(reader, "", &unusedName, &unusedVersion, &unusedTime, depth+1)
	case 11:
		length, err := readNbtArrayLength(reader)
		if err != nil {
			return err
		}
		return reader.skipBytes(int64(length) * 4)
	case 12:
		length, err := readNbtArrayLength(reader)
		if err != nil {
			return err
		}
		return reader.skipBytes(int64(length) * 8)
	default:
		return fmt.Errorf("Unsupported NBT tag %d.", tagType)
	}
}

// readNbtArrayLength 读取 NBT 数组长度（带合理上限校验）。
func readNbtArrayLength(reader *nbtReader) (int32, error) {
	length, err := reader.readInt32()
	if err != nil {
		return 0, err
	}
	if length < 0 || length > 100_000_000 {
		return 0, fmt.Errorf("Invalid NBT array size.")
	}
	return length, nil
}
