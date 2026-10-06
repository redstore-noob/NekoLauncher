package bindings

// 最小 LZ4 块格式(block format)解压器。
//
// Wallpaper Engine 的 .tex mipmap 用 LZ4 **块格式**压缩(不是帧格式,没有魔数头),
// Go 标准库没有对应实现,这里按官方规范实现解码:
//
//	块 = 若干序列 + 末尾纯字面量段
//	序列 = token + [扩展字面量长度] + 字面量 + 2字节偏移 + [扩展匹配长度]
//	token 高 4 位 = 字面量长度(15 表示后面还有扩展字节,每个 255 续接)
//	       低 4 位 = 匹配长度 - 4(同样 15 表示扩展)
//	匹配复制必须逐字节进行:匹配长度可超过偏移(重叠复制,相当于 RLE)
//
// 只实现解码;编码不在本项目的需求内。

// lz4UncompressBlock 把 LZ4 块格式的 src 解压成 dstLen 字节。
// dstLen 来自文件头里记录的解压后长度,因此可以预先分配。
func lz4UncompressBlock(src []byte, dstLen int) ([]byte, error) {
	if dstLen < 0 || dstLen > 1<<30 {
		return nil, errLZ4Invalid("目标长度异常")
	}
	dst := make([]byte, 0, dstLen)
	pos := 0
	for {
		if pos == len(src) {
			// 输入在上一序列(通常是匹配)之后干净耗尽:合法结束。
			// 规范要求编码器以纯字面量段收尾,但解码器宽容一点不吃亏。
			break
		}
		if pos > len(src) {
			return nil, errLZ4Invalid("输入在序列中途结束")
		}
		token := int(src[pos])
		pos++

		// ---- 字面量长度 ----
		literalLen := token >> 4
		if literalLen == 15 {
			for {
				if pos >= len(src) {
					return nil, errLZ4Invalid("字面量长度扩展越界")
				}
				b := int(src[pos])
				pos++
				literalLen += b
				if b != 255 {
					break
				}
			}
		}
		if pos+literalLen > len(src) || len(dst)+literalLen > dstLen {
			return nil, errLZ4Invalid("字面量段越界")
		}
		dst = append(dst, src[pos:pos+literalLen]...)
		pos += literalLen

		// 块尾:最后一段只有字面量,没有匹配
		if pos == len(src) {
			break
		}

		// ---- 匹配偏移与长度 ----
		if pos+2 > len(src) {
			return nil, errLZ4Invalid("匹配偏移越界")
		}
		offset := int(src[pos]) | int(src[pos+1])<<8
		pos += 2
		if offset == 0 || offset > len(dst) {
			return nil, errLZ4Invalid("匹配偏移非法")
		}
		matchLen := token & 0x0F
		if matchLen == 15 {
			for {
				if pos >= len(src) {
					return nil, errLZ4Invalid("匹配长度扩展越界")
				}
				b := int(src[pos])
				pos++
				matchLen += b
				if b != 255 {
					break
				}
			}
		}
		matchLen += 4
		if len(dst)+matchLen > dstLen {
			return nil, errLZ4Invalid("匹配段超出目标长度")
		}
		// 逐字节复制以正确处理重叠(WE 的平滑图像数据大量依赖这一点)
		start := len(dst) - offset
		for i := 0; i < matchLen; i++ {
			dst = append(dst, dst[start+i])
		}
	}
	if len(dst) != dstLen {
		return nil, errLZ4Invalid("解压结果长度与预期不符")
	}
	return dst, nil
}

// errLZ4Invalid 统一的解压失败描述,方便调用方日志定位。
type errLZ4Invalid string

func (e errLZ4Invalid) Error() string { return "LZ4 块解压失败:" + string(e) }
