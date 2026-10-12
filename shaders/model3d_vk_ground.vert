#version 450

// Ground-plane vertex format: position, normal and tiled UV coordinates.
layout(set = 0, binding = 0, std140) uniform Frame {
    mat4 ViewProj;
    vec4 Light;
    mat4 Bones[256];
} c;

layout(push_constant) uniform Draw {
    vec4 Shade;
    vec4 Tint;
    vec4 UV;
} d;

layout(location = 0) in vec3 pos;
layout(location = 1) in vec3 nrm;
layout(location = 2) in vec2 uvIn;

layout(location = 0) out vec3 vNrm;
layout(location = 1) out vec2 vUV;

void main() {
    gl_Position = c.ViewProj * vec4(pos, 1.0);
    vNrm = nrm;
    vUV = uvIn;
}
