#version 450

// The other half of the particle pass: one texel of the particle's own map,
// multiplied by the colour its colour ramp gave it. What comes out is
// premultiplied, which is what both blend modes want — the covering pipeline
// lays it over what is already there, and the additive one adds it.
layout(set = 0, binding = 0, std140) uniform Frame {
    mat4 ViewProj;
    vec4 Right;
    vec4 Up;
    vec4 Light;
    vec4 Diffuse;
} c;

layout(push_constant) uniform Draw {
    vec4 Tint;
} d;

layout(set = 0, binding = 1) uniform sampler2D tex;

layout(location = 0) in vec2 vUV;
layout(location = 1) in vec4 vTint;
layout(location = 0) out vec4 fragColor;

void main() {
    vec4 t = textureLod(tex, vUV, 0.0);
    float a = t.a * vTint.a;
    fragColor = vec4(t.rgb * vTint.rgb * a, a);
}