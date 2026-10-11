package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	gio "gioui.org/shader/gio"
)

// TestSpirvIntegrity validates the embedded SPIR-V blobs the Vulkan backend
// feeds to vkCreateShaderModule: 4-byte alignment, magic number and a
// consistent instruction stream (each word count must land exactly on the
// end of the buffer). Corrupt data here makes the driver shader compiler
// fault inside vkCreateGraphicsPipelines.
func TestSpirvIntegrity(t *testing.T) {
	srcs := map[string]string{
		"blit_vert":  gio.Shader_blit_vert.SPIRV,
		"cover_vert": gio.Shader_cover_vert.SPIRV,
	}
	for i, s := range gio.Shader_blit_frag {
		srcs[fmt.Sprintf("blit_frag[%d]", i)] = s.SPIRV
	}
	for i, s := range gio.Shader_cover_frag {
		srcs[fmt.Sprintf("cover_frag[%d]", i)] = s.SPIRV
	}
	srcs["material_frag"] = gio.Shader_material_frag.SPIRV
	srcs["material_vert"] = gio.Shader_material_vert.SPIRV
	srcs["stencil_vert"] = gio.Shader_stencil_vert.SPIRV
	srcs["stencil_frag"] = gio.Shader_stencil_frag.SPIRV
	srcs["intersect_vert"] = gio.Shader_intersect_vert.SPIRV
	srcs["intersect_frag"] = gio.Shader_intersect_frag.SPIRV
	srcs["input_vert"] = gio.Shader_input_vert.SPIRV
	srcs["copy_vert"] = gio.Shader_copy_vert.SPIRV
	srcs["copy_frag"] = gio.Shader_copy_frag.SPIRV
	computeSPIRV, err := os.ReadFile("shaders/fx_vk_expand.comp.spv")
	if err != nil {
		t.Fatalf("read FX compute SPIR-V: %v", err)
	}
	srcs["fx_vk_expand.comp"] = string(computeSPIRV)
	ribbonSPIRV, err := os.ReadFile("fx_vk_ribbon.comp.spv")
	if err != nil {
		t.Fatalf("read ribbon compute SPIR-V: %v", err)
	}
	srcs["fx_vk_ribbon.comp"] = string(ribbonSPIRV)
	for name, s := range srcs {
		b := []byte(s)
		if len(b)%4 != 0 {
			t.Errorf("%s: byte length %d is not a multiple of 4", name, len(b))
			continue
		}
		words := make([]uint32, len(b)/4)
		for i := range words {
			words[i] = binary.LittleEndian.Uint32(b[i*4:])
		}
		if words[0] != 0x07230203 {
			t.Errorf("%s: bad SPIR-V magic %#08x", name, words[0])
			continue
		}
		bound := words[3]
		i := 5
		bad := false
		for i < len(words) {
			wc := int(words[i] >> 16)
			if wc == 0 {
				t.Errorf("%s: zero instruction word count at word %d/%d", name, i, len(words))
				bad = true
				break
			}
			i += wc
		}
		if bad {
			continue
		}
		if i != len(words) {
			t.Errorf("%s: instruction stream ends at word %d, buffer has %d words", name, i, len(words))
			continue
		}
		t.Logf("%s: OK, %d words, id bound %d", name, len(words), bound)
	}
}
