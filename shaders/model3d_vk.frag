#version 450

// The SPIR-V this compiles to is checked in as model3d_vk.frag.spv and
// embedded by the renderer, so editing this file on its own changes nothing.
// It is rebuilt with:
//
//	glslangValidator -V model3d_vk.frag -o model3d_vk.frag.spv
//
// glslang 16.6.0 reproduces the checked in binaries byte for byte, which is
// what makes it the version to use: a different one rewrites every id in
// both modules and the diff is the whole file. The Direct3D path needs none
// of this — its shader is HLSL text compiled by the driver at startup.

// Port of modelGpuPixelShader (HLSL): the same Lambert term the software
// renderer uses, over the sampled diffuse map, so a model looks the same on
// either path. The coordinate is wrapped before sampling because a model's
// UVs run past the edge of its texture and the sampler clamps, which smears
// the last row over the whole overhang.
//
// v is taken as the model wrote it and from mip 0: a .rsh stores its map top
// down, which is the convention the coordinate was authored against, and the
// software path samples the top level and nothing else.
//
// The colour leaves premultiplied by the tint's alpha, which is what the
// blended pipeline reads: its factors are one for the source and one minus
// the source alpha for the destination, so a fragment meets what is behind it
// as src + dst * (1 - src.a). At a full alpha that reduces to the colour on
// its own, so the opaque pipeline drawing this same shader is unchanged.
layout(set = 0, binding = 0, std140) uniform Frame {
    mat4 ViewProj;
    vec4 Light;  // xyz points towards the light, w is the ambient term
    mat4 Bones[256];
} c;

layout(push_constant) uniform Draw {
    vec4 Shade;  // x is the diffuse term, y the texture rotation in quarter turns
    vec4 Tint;
    vec4 UV;
} d;

layout(set = 0, binding = 1) uniform sampler2D tex;

layout(location = 0) in vec3 vNrm;
layout(location = 1) in vec2 vUV;
layout(location = 0) out vec4 fragColor;

void main() {
    vec3 n = normalize(vNrm);
    float lambert = max(dot(n, c.Light.xyz), 0.0);
    float f = min(c.Light.w + d.Shade.x * lambert, 1.0);
    vec3 col = textureLod(tex, fract(vUV), 0.0).rgb * d.Tint.rgb * f;
    fragColor = vec4(col * d.Tint.a, d.Tint.a);
}
