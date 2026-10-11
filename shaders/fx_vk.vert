#version 450

// Particle quads and pre-tessellated ribbons share this vertex path. Sprites
// face the camera unless their orientation requests a fixed Z-up plane. The
// frame carries the model light once so all FX are shaded without per-particle
// CPU colour work.
layout(set = 0, binding = 0, std140) uniform Frame {
    mat4 ViewProj;
    vec4 Right;
    vec4 Up;
    vec4 Light;   // xyz points towards the light; w is ambient
    vec4 Diffuse; // x is diffuse strength
} c;

layout(push_constant) uniform Draw {
    vec4 Tint;  // rgb multiplies every particle, a scales their alpha
} d;

layout(location = 0) in vec3 pos;    // the particle, in model space
layout(location = 1) in vec2 corner; // which corner of the quad: -1..1
layout(location = 2) in float size;  // its radius, in model units
layout(location = 3) in vec4 tint;   // its own colour and alpha
layout(location = 4) in vec2 uv;     // where on the map to read
layout(location = 5) in vec4 orientation; // angle, horizontal flag, self-lit flag, padding
layout(location = 6) in vec4 surfaceNormal; // xyz normal, w reserved

layout(location = 0) out vec2 vUV;
layout(location = 1) out vec4 vTint;

void main() {
    vec3 right = c.Right.xyz;
    vec3 up = c.Up.xyz;
    vec3 normal = surfaceNormal.xyz;
    if (orientation.y < 0.5) {
        normal = normalize(cross(right, up));
    }
    if (orientation.y > 0.5 && orientation.y < 1.5) {
        float cs = cos(orientation.x);
        float sn = sin(orientation.x);
        right = vec3(cs, sn, 0.0);
        up = vec3(-sn, cs, 0.0);
        normal = vec3(0.0, 0.0, 1.0);
    } else if (orientation.y >= 1.5) {
        // Pre-tessellated ribbons already carry their model-space normal.
        normal = surfaceNormal.xyz;
    }
    vec3 world = pos
        + right * (corner.x * size)
        + up * (corner.y * size);
    gl_Position = c.ViewProj * vec4(world, 1.0);
    vUV = uv;
    float shade = 1.0;
    if (orientation.z < 0.5) {
        float lambert = max(dot(normal, c.Light.xyz), 0.0);
        shade = min(c.Light.w + c.Diffuse.x * lambert, 1.0);
    }
    vTint = vec4(tint.rgb * d.Tint.rgb * shade, tint.a * d.Tint.a);
}