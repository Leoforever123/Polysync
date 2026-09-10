package tray

import (
	"encoding/binary"
	"math"
)

// iconDIB is a 32-bit Windows icon resource: three linked green diamonds,
// matching the web brand. Generated in code so no external image is needed.
func iconDIB() []byte { return iconDIBSize(32) }
func iconDIBSize(size int) []byte {
	stride := ((size + 31) / 32) * 4
	pixels := size * size * 4
	data := make([]byte, 40+pixels+size*stride)
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(data[offset:], value) }
	put(0, 40)
	put(4, uint32(size))
	put(8, uint32(size*2))
	binary.LittleEndian.PutUint16(data[12:], 1)
	binary.LittleEndian.PutUint16(data[14:], 32)
	put(20, uint32(pixels))
	centers := [][2]float64{{9, 19}, {16, 11}, {23, 19}}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			coverage := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					px, py := float64(x)+float64(sx)/4, float64(y)+float64(sy)/4
					px, py = px*32/float64(size), py*32/float64(size)
					for _, c := range centers {
						d := math.Abs(px-c[0]) + math.Abs(py-c[1])
						if d >= 4 && d <= 8 {
							coverage++
							break
						}
					}
				}
			}
			offset := 40 + ((size-1-y)*size+x)*4
			data[offset], data[offset+1], data[offset+2], data[offset+3] = 90, 158, 72, byte(coverage*255/16)
			if coverage == 0 {
				data[40+pixels+(size-1-y)*stride+x/8] |= 0x80 >> (x % 8)
			}
		}
	}
	return data
}
