# 3D Rendering and FX Shaders Breakdown (Vulkan / SPIR-V)

The 3D model and FX rendering in this project relies on Vulkan GLSL shaders compiled into SPIR-V bytecode[cite: 17].

* Read this in other languages: [Русский](README.ru.md)

## Fragment Shaders (Materials and Particles)

* **Compilation:** Shaders are built using `glslangValidator`. For the model fragment shader (`model3d_vk.frag`), it is critical to use exactly version **16.6.0**; other versions regenerate internal IDs, causing massive binary diffs even when the logic is unchanged[cite: 17].
* **Lambert Shading:** The model shader implements a Lambert lighting term ported directly from HLSL, ensuring the GPU output perfectly matches the software rendering path[cite: 17].
* **UV Wrapping:** Texture coordinates are wrapped using `fract(vUV)` before sampling. Model UVs often extend past the [0, 1] bounds, and since the sampler clamps by default, failing to wrap would smear the edge pixels across the overhang[cite: 17].
* **Texture Sampling:** Shaders sample strictly from mip level 0 (`textureLod(tex, uv, 0.0)`) because `.rsh` textures are stored top-down[cite: 17, 18].
* **Premultiplied Alpha:** Both the model and particle shaders output colors with premultiplied alpha (`t.rgb * a`)[cite: 17, 18]. This fulfills the blending pipeline's requirements, allowing the same pipeline to correctly handle opaque, covering, and additive blending[cite: 17, 18].

## Compute Shaders (FX Geometry Expansion)

Compute shaders are used to offload FX geometry generation from the CPU to the GPU.

* **`fx_vk_expand.comp` (Sprites):** Reads a compact array of sprite records written by the CPU (containing position, radius, tint, and orientation) and expands each record into a full 6-vertex quad (billboard)[cite: 19]. It outputs a packed 80-byte vertex format expected by the graphics pipeline's vertex shader[cite: 19].
* **`fx_vk_ribbon.comp` (Ribbons and Beams):** Handles the tessellation of ribbon-like effects, including beams (`STYLE_BEAM`), rings (`STYLE_RING`), and skeleton lines[cite: 20]. It mirrors the software renderer's logic exactly:
  * Computes the arc and bow across the span[cite: 20].
  * Applies positional noise (wobble) based on position rather than time, preventing the noise from crawling between frames[cite: 20].
  * Orientates the cross-section to face the camera, unless the ribbon carries its own fixed plane (like a ring)[cite: 20].