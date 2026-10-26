#version 450

// Port of modelGpuVertexShader (the reference shader). Per-frame data lives in a uniform
// block, per-draw data (shade term, tint, UV mapping) in a push constant
// block, which is what keeps the descriptor single and offset-free.
// Column-major packing is the default, which is what GLSL std140 mat4 uses,
// and mul(M, v) equals M * v, so the buffer bytes are consumed unchanged.
layout(set = 0, binding = 0, std140) uniform Frame {
    mat4 ViewProj;
    vec4 Light;  // xyz points towards the light, w is the ambient term
    mat4 Bones[256];
} c;

layout(push_constant) uniform Draw {
    vec4 Shade;  // x is the diffuse term, y the texture rotation in quarter turns
    vec4 Tint;
    vec4 UV;     // xy offset, z scale, w mirror
} d;

layout(location = 0) in vec3 pos;
layout(location = 1) in vec3 nrm;
layout(location = 2) in vec2 uvIn;
layout(location = 3) in vec4 bon;
layout(location = 4) in vec4 wgt;

layout(location = 0) out vec3 vNrm;
layout(location = 1) out vec2 vUV;

// rotateUV turns the coordinates a quarter turn at a time, folded into 0..3
// with the same float arithmetic the reference original uses.
vec2 rotateUV(vec2 uv, float turns) {
    float k = floor(abs(turns) + 0.5);
    k = k - floor(k / 4.0) * 4.0;
    if (k == 1.0) {
        return vec2(uv.y, -uv.x);
    }
    if (k == 2.0) {
        return vec2(-uv.x, -uv.y);
    }
    if (k == 3.0) {
        return vec2(-uv.y, uv.x);
    }
    return uv;
}

vec2 uvTransform(vec2 uv) {
    if (d.UV.w != 0.0) {
        uv.x = -uv.x;
    }
    if (d.UV.z > 0.0 && d.UV.z != 1.0) {
        uv *= d.UV.z;
    }
    return rotateUV(uv, d.Shade.y) + d.UV.xy;
}

void main() {
    vec4 p = vec4(pos, 1.0);
    vec3 n = nrm;
    float total = wgt.x + wgt.y + wgt.z + wgt.w;
    if (total > 0.0) {
        vec4 acc = vec4(0.0);
        vec3 nacc = vec3(0.0);
        for (int i = 0; i < 4; i++) {
            float w = wgt[i];
            if (w <= 0.0) {
                continue;
            }
            mat4 m = c.Bones[int(bon[i])];
            acc += (m * p) * (w / total);
            nacc += (mat3(m) * n) * (w / total);
        }
        p = acc;
        n = nacc;
    }
    gl_Position = c.ViewProj * p;
    vNrm = n;
    vUV = uvTransform(uvIn);
}
