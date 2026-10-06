package bindings

// Wallpaper Engine .tex 纹理解码。
//
// .tex 是 WE 的私有纹理容器,布局(与 RePKG 逆向结论一致):
//
//	nstring "TEXV0005"          // 容器魔数(版本内嵌)
//	nstring "TEXI0001"          // 图像头魔数
//	int32   format              // 像素格式:0=RGBA8888 4=DXT5 6=DXT3 7=DXT1 8=RG88 9=R8
//	int32   flags               // 1=不插值 2=ClampUV 4=GIF 32=视频纹理
//	int32   textureWidth        // 纹理宽(按 4 对齐的填充尺寸,行距按它算)
//	int32   textureHeight       // 纹理高(填充)
//	int32   imageWidth          // 实际图像宽(解码后裁剪到它)
//	int32   imageHeight         // 实际图像高
//	int32   unk
//	nstring "TEXB000x"          // 图像体容器,0001..0004
//	int32   imageCount          // 帧数(GIF 多帧;静态图恒 1)
//	int32   fif                 // FreeImage 格式码:>=3 才有;-1=FIF_UNKNOWN 表示裸像素
//	int32   isVideoMp4          // 仅 0004:fif==-1 且为 1 时体数据是 MP4
//	每帧: int32 mipmapCount + 每个 mipmap:
//	  v1: w,h,byteCount,bytes
//	  v2/v3: w,h,isLZ4,decompressedCount,byteCount,bytes
//	  v4: 1,2,nstring,1,w,h,isLZ4,decompressedCount,byteCount,bytes
//
// 解码策略:嵌了 PNG/JPEG/GIF 的直接透传;MP4 透传给前端做 VideoTexture;
// 裸像素按 format 解码(DXT 解块 / RGBA 直读),从 textureWidth 行距裁剪到
// imageWidth×imageHeight,再编码成 PNG 交给浏览器。

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
)

// ---- 纹理格式码(TEXI 头 format 字段) ----
const (
	weTexFormatRGBA8888 = 0
	weTexFormatDXT5     = 4
	weTexFormatDXT3     = 6
	weTexFormatDXT1     = 7
	weTexFormatRG88     = 8
	weTexFormatR8       = 9
)

// TEXI 头 flags 位
const weTexFlagIsGif = 4

// FreeImage 格式码(TEXB 容器 fif 字段),只列实际会遇到且浏览器能直接吃的
const (
	weFIFUnknown = -1
	weFIFJPEG    = 2
	weFIFPNG     = 13
	weFIFTGA     = 17
	weFIFMP4     = 1000 // 非标准值:TEXB0004 的 isVideoMp4 标记折算出来的伪格式码
)

// weTexMime fif 码对应的浏览器可渲染 MIME 类型;false 表示不是内嵌图。
func weTexMime(fif int) (string, bool) {
	switch fif {
	case weFIFPNG:
		return "image/png", true
	case weFIFJPEG:
		return "image/jpeg", true
	case weFIFMP4:
		return "video/mp4", true
	default:
		return "", false
	}
}

// weTexCursor .tex 字节流游标。
type weTexCursor struct {
	data []byte
	pos  int
}

func (c *weTexCursor) readInt32() (int32, bool) {
	if c.pos+4 > len(c.data) {
		return 0, false
	}
	value := int32(binary.LittleEndian.Uint32(c.data[c.pos:]))
	c.pos += 4
	return value, true
}

// readNString 读 nul 结尾字符串(上限 16,魔数用)。
func (c *weTexCursor) readNString() (string, bool) {
	end := bytes.IndexByte(c.data[c.pos:], 0)
	if end < 0 || end > 16 {
		return "", false
	}
	value := string(c.data[c.pos : c.pos+end])
	c.pos += end + 1
	return value, true
}

func (c *weTexCursor) readBytes(count int) ([]byte, bool) {
	if count < 0 || c.pos+count > len(c.data) {
		return nil, false
	}
	value := c.data[c.pos : c.pos+count]
	c.pos += count
	return value, true
}

// weTexImage 解码结果:imageData 为可直接写给浏览器的字节(PNG/JPEG/MP4),
// mime 说明其类型;width/height 对视频与解码失败的情况可能为 0。
type weTexImage struct {
	ImageData []byte
	Mime      string
	Width     int
	Height    int
}

// weDecodeTex 解码 .tex 字节,只取第一帧(静态图 / GIF 首帧 / 视频本体)。
func weDecodeTex(data []byte) (*weTexImage, error) {
	cursor := &weTexCursor{data: data}
	magic1, ok := cursor.readNString()
	if !ok || magic1 != "TEXV0005" {
		return nil, fmt.Errorf("不是 WE 纹理:魔数 %q", magic1)
	}
	magic2, ok := cursor.readNString()
	if !ok || magic2 != "TEXI0001" {
		return nil, fmt.Errorf("TEXI 头异常:%q", magic2)
	}

	fields := make([]int32, 7, 7)
	for i := range fields {
		value, ok := cursor.readInt32()
		if !ok {
			return nil, fmt.Errorf("TEXI 头字段不足")
		}
		fields[i] = value
	}
	// fields: [0]=格式 [1]=flags [2]=纹理宽 [3]=纹理高 [4]=图像宽 [5]=图像高 [6]=保留
	format := int(fields[0])
	textureWidth := int(fields[2])
	imageWidth, imageHeight := int(fields[4]), int(fields[5])
	if imageWidth <= 0 || imageHeight <= 0 || imageWidth > 32768 || imageHeight > 32768 {
		return nil, fmt.Errorf("纹理尺寸异常:%dx%d", imageWidth, imageHeight)
	}

	bodyMagic, ok := cursor.readNString()
	if !ok || (bodyMagic != "TEXB0001" && bodyMagic != "TEXB0002" && bodyMagic != "TEXB0003" && bodyMagic != "TEXB0004") {
		return nil, fmt.Errorf("TEXB 魔数异常:%q", bodyMagic)
	}
	version := 0
	if _, err := fmt.Sscanf(bodyMagic[len(bodyMagic)-1:], "%d", &version); err != nil {
		return nil, fmt.Errorf("TEXB 版本解析失败:%q", bodyMagic)
	}
	if _, ok := cursor.readInt32(); !ok { // imageCount,只取第一帧
		return nil, fmt.Errorf("图像数读取失败")
	}

	fif := weFIFUnknown
	if version >= 3 {
		value, ok := cursor.readInt32()
		if !ok {
			return nil, fmt.Errorf("fif 读取失败")
		}
		fif = int(value)
		if version >= 4 {
			isVideo, ok := cursor.readInt32()
			if !ok {
				return nil, fmt.Errorf("视频标记读取失败")
			}
			if fif == weFIFUnknown && isVideo == 1 {
				fif = weFIFMP4
			}
		}
	}
	// 与 RePKG 一致:TEXB0004 只有承载 MP4 时才用 v4 的 mipmap 布局,
	// 其余(绝大多数)按 v2/v3 布局读——v4 多出的常数段只出现在视频纹理里。
	if version >= 4 && fif != weFIFMP4 {
		version = 3
	}

	if _, ok := cursor.readInt32(); !ok { // mipmapCount
		return nil, fmt.Errorf("mipmap 数读取失败")
	}
	payload, width, height, err := weReadMipmap(cursor, version)
	if err != nil {
		return nil, err
	}

	// TEXB0003 没有 isVideo 字段,视频纹理同样以 fif=-1 出现:
	// 裸 MP4 的 ISO-BMFF 盒从第 4 字节起是 "ftyp",嗅探一下归入视频分支。
	if fif == weFIFUnknown && len(payload) > 12 && string(payload[4:8]) == "ftyp" {
		fif = weFIFMP4
	}

	// 内嵌图/视频:直接透传(LZ4 先解开)
	if mime, isEmbedded := weTexMime(fif); isEmbedded {
		if len(payload) > 0 && payload[0] == 0x04 && payload[1] == 0x22 {
			// 少数包会把内嵌图也做块压缩;PNG/JPEG 头对不上时尝试 LZ4。
			// MP4 的 ftyp 盒不会以 04 22 开头,不会误判。
			if plain, lzErr := lz4UncompressBlock(payload, width*height*4); lzErr == nil {
				payload = plain
			}
		}
		return &weTexImage{ImageData: payload, Mime: mime, Width: imageWidth, Height: imageHeight}, nil
	}

	// 裸像素:按纹理格式解码
	rgba, err := weDecodeTexPixels(payload, width, height, format)
	if err != nil {
		return nil, err
	}
	// 从填充行距裁出实际图像区
	cropped := cropRGBA(rgba, textureWidth, imageWidth, imageHeight)
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, cropped); err != nil {
		return nil, err
	}
	return &weTexImage{ImageData: buffer.Bytes(), Mime: "image/png", Width: imageWidth, Height: imageHeight}, nil
}

// weReadMipmap 读一个 mipmap 条目(不同 TEXB 版本字段不同),返回体数据与 mipmap 尺寸。
func weReadMipmap(cursor *weTexCursor, version int) ([]byte, int, int, error) {
	if version >= 4 {
		// v4 多出三段常数与一个条件 JSON,校验失败按坏包处理
		for _, want := range []int32{1, 2} {
			value, ok := cursor.readInt32()
			if !ok || value != want {
				return nil, 0, 0, fmt.Errorf("TEXB v4 头部常数异常")
			}
		}
		if _, ok := cursor.readNString(); !ok {
			return nil, 0, 0, fmt.Errorf("TEXB v4 条件段读取失败")
		}
		if value, ok := cursor.readInt32(); !ok || value != 1 {
			return nil, 0, 0, fmt.Errorf("TEXB v4 头部常数异常")
		}
	}
	width, ok := cursor.readInt32()
	if !ok {
		return nil, 0, 0, fmt.Errorf("mipmap 宽读取失败")
	}
	height, ok := cursor.readInt32()
	if !ok {
		return nil, 0, 0, fmt.Errorf("mipmap 高读取失败")
	}
	isCompressed, byteCount := int32(0), int32(0)
	if version >= 2 {
		if value, ok := cursor.readInt32(); ok {
			isCompressed = value
		}
	}
	decompressedCount := int32(0)
	if version >= 2 {
		if value, ok := cursor.readInt32(); ok {
			decompressedCount = value
		}
	}
	if value, ok := cursor.readInt32(); ok {
		byteCount = value
	} else {
		return nil, 0, 0, fmt.Errorf("mipmap 长度读取失败")
	}
	payload, ok := cursor.readBytes(int(byteCount))
	if !ok {
		return nil, 0, 0, fmt.Errorf("mipmap 数据越界")
	}
	if isCompressed == 1 {
		plain, err := lz4UncompressBlock(payload, int(decompressedCount))
		if err != nil {
			return nil, 0, 0, err
		}
		payload = plain
	}
	return payload, int(width), int(height), nil
}

// weDecodeTexPixels 把裸像素 mipmap 解成 RGBA。width/height 是纹理(填充)尺寸。
func weDecodeTexPixels(payload []byte, width, height, format int) (*image.NRGBA, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("mipmap 尺寸异常:%dx%d", width, height)
	}
	switch format {
	case weTexFormatRGBA8888:
		if len(payload) < width*height*4 {
			return nil, fmt.Errorf("RGBA 数据不足:%d < %d", len(payload), width*height*4)
		}
		image := image.NewNRGBA(image.Rect(0, 0, width, height))
		copy(image.Pix, payload[:width*height*4])
		return image, nil
	case weTexFormatRG88:
		if len(payload) < width*height*2 {
			return nil, fmt.Errorf("RG88 数据不足")
		}
		out := image.NewNRGBA(image.Rect(0, 0, width, height))
		for i := 0; i < width*height; i++ {
			out.Pix[i*4] = payload[i*2]
			out.Pix[i*4+1] = payload[i*2+1]
			out.Pix[i*4+2] = 0
			out.Pix[i*4+3] = 0xFF
		}
		return out, nil
	case weTexFormatR8:
		if len(payload) < width*height {
			return nil, fmt.Errorf("R8 数据不足")
		}
		out := image.NewNRGBA(image.Rect(0, 0, width, height))
		for i := 0; i < width*height; i++ {
			out.Pix[i*4] = payload[i]
			out.Pix[i*4+1] = payload[i]
			out.Pix[i*4+2] = payload[i]
			out.Pix[i*4+3] = 0xFF
		}
		return out, nil
	case weTexFormatDXT1, weTexFormatDXT3, weTexFormatDXT5:
		return decodeDXT(payload, width, height, format)
	default:
		return nil, fmt.Errorf("不支持的纹理格式:%d", format)
	}
}

// cropRGBA 从按 textureWidth 排列的填充位图里裁出 imageWidth×imageHeight。
func cropRGBA(src *image.NRGBA, textureWidth, imageWidth, imageHeight int) *image.NRGBA {
	if textureWidth == imageWidth || textureWidth <= 0 {
		return src
	}
	if src.Bounds().Dx() == imageWidth {
		return src
	}
	out := image.NewNRGBA(image.Rect(0, 0, imageWidth, minInt(imageHeight, src.Bounds().Dy())))
	for row := 0; row < out.Bounds().Dy(); row++ {
		source := row * src.Stride
		copy(out.Pix[row*out.Stride:row*out.Stride+imageWidth*4], src.Pix[source:source+imageWidth*4])
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ---- DXT(BC1/BC2/BC3)解块 ----

// decodeDXT 把 DXT 压缩数据解成 NRGBA。WE 只存 4 对齐尺寸,块数按上取整。
func decodeDXT(payload []byte, width, height, format int) (*image.NRGBA, error) {
	blocksX, blocksY := (width+3)/4, (height+3)/4
	blockBytes := 8
	if format != weTexFormatDXT1 {
		blockBytes = 16
	}
	if len(payload) < blocksX*blocksY*blockBytes {
		return nil, fmt.Errorf("DXT 数据不足:%d < %d", len(payload), blocksX*blocksY*blockBytes)
	}
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for blockY := 0; blockY < blocksY; blockY++ {
		for blockX := 0; blockX < blocksX; blockX++ {
			base := (blockY*blocksX + blockX) * blockBytes
			block := payload[base : base+blockBytes]
			var alpha [16]byte
			if format == weTexFormatDXT3 {
				decodeDXT3Alpha(block[:8], &alpha)
				decodeBC1Colors(block[8:], &alpha, out, blockX*4, blockY*4, width, height, false)
			} else if format == weTexFormatDXT5 {
				decodeDXT5Alpha(block[:8], &alpha)
				decodeBC1Colors(block[8:], &alpha, out, blockX*4, blockY*4, width, height, false)
			} else {
				decodeBC1Colors(block, &alpha, out, blockX*4, blockY*4, width, height, true)
			}
		}
	}
	return out, nil
}

// rgb565 解码
func decode565(value uint16) (byte, byte, byte) {
	r := byte((value >> 11) & 0x1F)
	g := byte((value >> 5) & 0x3F)
	b := byte(value & 0x1F)
	return (r << 3) | (r >> 2), (g << 2) | (g >> 4), (b << 3) | (b >> 2)
}

// decodeBC1Colors 解 4 字节颜色端点 + 4 字节 2bit 索引;punchthrough 表示
// DXT1 独有的"索引 3 = 透明"语义(DXT3/5 的颜色块没有这一位)。
func decodeBC1Colors(block []byte, alpha *[16]byte, out *image.NRGBA, offsetX, offsetY, width, height int, punchthrough bool) {
	color0 := binary.LittleEndian.Uint16(block[0:2])
	color1 := binary.LittleEndian.Uint16(block[2:4])
	r0, g0, b0 := decode565(color0)
	r1, g1, b1 := decode565(color1)
	// 注意:插值必须走 int —— byte 算术会回绕(2*214+214 超过 255),
	// 曾把所有插值色洗成垃圾色,只有端点色幸存
	var palette [4][3]byte
	palette[0] = [3]byte{r0, g0, b0}
	palette[1] = [3]byte{r1, g1, b1}
	threeColor := punchthrough && color0 <= color1
	if threeColor {
		// 三色模式:color2 = 均值,color3 = 透明黑
		palette[2] = [3]byte{byte((int(r0) + int(r1)) / 2), byte((int(g0) + int(g1)) / 2), byte((int(b0) + int(b1)) / 2)}
		palette[3] = [3]byte{0, 0, 0}
	} else {
		// 四色模式:2/3-1/3 插值
		palette[2] = [3]byte{byte((2*int(r0) + int(r1)) / 3), byte((2*int(g0) + int(g1)) / 3), byte((2*int(b0) + int(b1)) / 3)}
		palette[3] = [3]byte{byte((int(r0) + 2*int(r1)) / 3), byte((int(g0) + 2*int(g1)) / 3), byte((int(b0) + 2*int(b1)) / 3)}
	}
	for pixel := 0; pixel < 16; pixel++ {
		x := offsetX + pixel%4
		y := offsetY + pixel/4
		if x >= width || y >= height {
			continue
		}
		index := int(block[4+pixel/4]>>(2*uint(pixel%4))) & 0x3
		a := byte(0xFF)
		if threeColor && index == 3 {
			a = 0
		} else if !punchthrough {
			// DXT3/5 的颜色块没有 punchthrough 位,alpha 恒来自 alpha 块
			a = alpha[pixel]
		}
		out.SetNRGBA(x, y, color.NRGBA{R: palette[index][0], G: palette[index][1], B: palette[index][2], A: a})
	}
}

// decodeDXT3Alpha BC2:每像素 4bit 显式 alpha。每字节装两个像素:
// 高 nibble = 偶数像素,低 nibble = 奇数像素,统一 <<4 归一到 8bit。
func decodeDXT3Alpha(block []byte, alpha *[16]byte) {
	for i := 0; i < 16; i += 2 {
		value := block[i/2]
		alpha[i] = value & 0xF0
		alpha[i+1] = (value & 0x0F) << 4
	}
}

// decodeDXT5Alpha BC3:两 alpha 端点 + 16 个 3bit 索引,48bit 索引区按小端排布。
func decodeDXT5Alpha(block []byte, alpha *[16]byte) {
	a0, a1 := block[0], block[1]
	var palette [8]byte
	palette[0], palette[1] = a0, a1
	if a0 > a1 {
		for i := 0; i < 6; i++ {
			palette[i+2] = byte((int(a0)*(6-i-1) + int(a1)*(i+1)) / 6)
		}
	} else {
		for i := 0; i < 4; i++ {
			palette[i+2] = byte((int(a0)*(4-i-1) + int(a1)*(i+1)) / 4)
		}
		palette[6], palette[7] = 0, 0xFF
	}
	// 48 位索引:小端整数流,每像素 3bit,低位在前
	var bits uint64
	for i := 0; i < 6; i++ {
		bits |= uint64(block[2+i]) << (8 * uint(i))
	}
	for i := 0; i < 16; i++ {
		alpha[i] = palette[(bits>>(3*uint(i)))&0x7]
	}
}

// weTexWriteTo 把解码结果写给 HTTP 响应(调用方负责缓存头)。
func weTexWriteTo(w io.Writer, decoded *weTexImage) error {
	_, err := w.Write(decoded.ImageData)
	return err
}
