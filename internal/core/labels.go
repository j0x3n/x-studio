package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

func ImplicitLabel(data []byte) ([]byte, error) {
	xmp := []byte(`<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#"><rdf:Description xmlns:xs="https://xstudio.local/ai/1.0/" xs:Label="AI 生成或合成" xs:Tool="X Studio" xs:Time="` + Now() + `"/></rdf:RDF></x:xmpmeta>`)
	if bytes.HasPrefix(data, []byte{0xff, 0xd8}) {
		payload := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), xmp...)
		if len(payload) > 65533 {
			return nil, fmt.Errorf("AI 元数据过大")
		}
		segment := []byte{0xff, 0xe1, 0, 0}
		binary.BigEndian.PutUint16(segment[2:], uint16(len(payload)+2))
		out := append([]byte{}, data[:2]...)
		out = append(out, segment...)
		out = append(out, payload...)
		return append(out, data[2:]...), nil
	}
	if bytes.HasPrefix(data, []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
		payload := append([]byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00"), xmp...)
		block := make([]byte, 8+len(payload)+4)
		binary.BigEndian.PutUint32(block, uint32(len(payload)))
		copy(block[4:], "iTXt")
		copy(block[8:], payload)
		binary.BigEndian.PutUint32(block[len(block)-4:], crc32.ChecksumIEEE(block[4:len(block)-4]))
		for pos := 8; pos+12 <= len(data); {
			n := int(binary.BigEndian.Uint32(data[pos:]))
			if n > len(data)-pos-12 {
				return nil, fmt.Errorf("PNG 块损坏")
			}
			if string(data[pos+4:pos+8]) == "IEND" {
				out := append([]byte{}, data[:pos]...)
				out = append(out, block...)
				return append(out, data[pos:]...), nil
			}
			pos += n + 12
		}
		return nil, fmt.Errorf("PNG 缺少结束块")
	}
	return nil, fmt.Errorf("隐式标识仅支持 JPEG 和 PNG")
}
func LabelFile(filename string) error {
	b, e := os.ReadFile(filename)
	if e != nil {
		return e
	}
	b, e = ImplicitLabel(b)
	if e != nil {
		return e
	}
	return os.WriteFile(filename, b, 0600)
}
