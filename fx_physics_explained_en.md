# FX Physics — Ghidra Decompiled `FXDynamicsNewton` Analysis

Source file: `fx_and_particles_and_anims_and_vis.c` (MSVC x64, Dawn of War Essence Engine FX subsystem).
Focus functions:
- `FXDynamicsNewton::FXDynamicsNewton` (copy and default ctor)
- `FXDynamicsNewton::vfunction8` (per-particle step / "Dynamics" update)
- `FXDynamicsNewton::vfunction9` (collision / resolve helper)
- `FXDynamicsNewtonLength::vfunction9` (length-clamped variant)

---

## 1. Physical model (plain language)

This is a **custom, limited particle integrator**, not a full Newton/ODE solver.
The main flow (`vfunction8`) does, per frame:

1. **Push / acceleration along X/Y/Z.**
   A particle does not store pure velocity; it stores "scale" coefficients (`param_2`, probably `deltaTime` or `speedScale`).
   Global constants `_DAT_..._141715bf0/4/8` set the direction/magnitude of base acceleration along the three axes,
   and the object's fields add per-particle push/offset. So this is simplified "physics" for predictable scenarios
   (exhaust, smoke, sparks), where motion is often authored/deterministic rather than computed from live forces.

2. **Drag / damping.**
   A second set of constants (`0x141764730/34/38`) multiplies the current (milli-)velocity by `scale` and adds the remainder —
   a typical exponential velocity damping form (`v *= (1 - drag*dt)` or `v += base*dt`).

3. **Square-root / vector normalization.**
   The code has a lot of `sqrtf(...)` / `SQRT(...)` and `1.0 / length`.
   This is a strong indicator of direction normalization (reflections, tangents, `direction = diff / |diff|`),
   not `F = m*g`-style gravity.

4. **Rotation via orthogonal 3x3 without trig.**
   In `vfunction9` (branch `iVar1 == 1`) there is the classic reflection/rotation formula using the dot product
   and doubled normal:
   ```
   newV = v - 2 * (v·n) * n
   ```
   appearing as `local_100 * 2.0 * fVar9 - ...`.
   This is a **mirror reflection across a plane/normal**, or a tangential decomposition of motion.

5. **Mesh hit / collision point.**
   In `vfunction9` there is a vtable-based collision call:
   ```
   ((**)(plVar2->vtable + 0x10))(plVar2, &fromPos, &toPos, &hitInfo, &someOut)
   ```
   If a nonzero "hit" flag comes back, motion is recomputed:
   - if the new point barely differs from the old one (`dist^2 < eps` or `< step^2`) — the particle is stuck/bounced;
   - otherwise — a reflected/displaced point is used.
   The code shows an **elasticity/reflectivity coefficient** = `1.0 - this->offset_0xc` (interpreted as restitution).
   There is also conditional decomposition of velocity into the "remaining after impact" part (again `2.0 * dot(n,v)`).

6. **Loose clamps / clipping.**
   - `if (|vel|^2 <= 360000)` and `if (|newPos|^2 <= 4e6)` — squared distance throttles so nothing runs off to infinity.
   - In the "too fast" branch there is a safety net `v = v / |v| * 600.0` — normalize and cap speed to 600.

7. **Output synchronization.**
   After the step, if `|vel| > 0.05`, it calls `FUN_140d826a0(local_48, &velocity)` and some callback via `(*param_1 + 8)`.
   Likely this hands the new velocity/position to the visualization/render system.

**Summary**: this is a typical game-FX "Newtonian-ish" system: there is velocity, drag, elastic reflection off the mesh,
distance/speed clamps, and vector normalization. Full physics (mass, momentum, angular momentum, a moderately accurate integrator)
is not necessary — motion is often authored authoritatively, and the engine just constrains and reflects.

---

## 2. Struct fields (`FXDynamicsNewton_data`) — inferred types

Reconstructed from the copy ctor and `vfunction8`:

| Offset | Type (inferred) | Inferred purpose |
|--------|----------------|------------------|
| `+0x00` | `float` | `scale` / speed-factor on X, or overall acceleration multiplier |
| `+0x04` | `float` | drag coefficient / velocity damping factor |
| `+0x08` | `float` | restitution / bounce / "bounciness" (used as `1 - offset_0xc` in `vfunction9`) |
| `+0x0C` | `float` (union with `+0x0C..0x13`) | "reflection/remainder" coefficient after impact: read as `(1 - this->offset_0xc)` in `vfunction9` |
| `+0x10` | `float[2]`? (`vec2` or part of `vec3`) | part of position/velocity (X and Y?); copied from `param_1+0x18` in ctor |
| `+0x18` | `float` | continuation of the vector from `+0x10` (Y or Z) |
| `+0x1C` | `float[2]` (together `0x1C..0x23`) | velocity/scale vector (`local_68 = this->offset_0x1c`, often read as a float pair) |
| `+0x24` | `float` | third component of that same vector (`local_60`) |
| `+0x28` | `int` | collision mode: 0 = off, 1 = full reflect/resolve, 2/3 = other return branches |
| `+0x30` | pointer (`void*` / vtable context) | mesh collision object/context (`this->offset_0x30` passed to `vfunction9` as `plVar2`) |

> Note: some fields overlap from the decompiler's point of view, but the accesses in `vfunction8`/`vfunction9`
> point to this set: 3 floats of scale/push, drag parameters, velocity/square-vector (3 floats),
> collision mode, and a pointer to the collision object.
> Init constants `0x3f800000 = 1.0f`, `0x3e4ccccd ≈ 0.2` suggest sensible defaults.

For `FXDynamicsNewtonLength`:
- `+0x00` / `+0x04` (in its own `FXDynamicsNewtonLength_data`) — `vec2` constraint (min/max on XY/Z?)
- `+0x08` — `float` Z constraint / length
- `+0x0C` — `float` max distance (`fVar6 <= this->offset_0xc`)

---

## 3. Refactoring: clean C++ equivalent

Below is a restored high-level view of `FXDynamicsNewton::step` and `resolveCollision` using normal names.
Some details (exact meaning of `FUN_140d95ef0` and `FUN_140d826a0`) are left as gateway calls, but semantics are reconstructed from context.

```cpp
#include <cmath>
#include <cstdint>

// Pseudo-driver for Ghidra global constants.
extern float g_fxNewtonAccelX;   // _DAT_141715bf0-ish
extern float g_fxNewtonAccelY;   // _DAT_141715bf4-ish
extern float g_fxNewtonAccelZ;   // _DAT_141715bf8-ish
extern float g_fxNewtonDragScale; // _DAT_141764730-ish
extern float g_fxNewtonDragY;     // _DAT_141764734-ish
extern float g_fxNewtonDragZ;     // _DAT_141764738-ish
extern float g_fxNewtonDefaultVal; // DAT_141764728-ish

struct FXDynamicsNewton
{
    // vtablePtr omitted — not relevant to physics.
    float  scale;            // +0x00
    float  drag;             // +0x04
    float  restitution;      // +0x08  (read as 1 - offset_0xc in vfunction9)
    union {
        float field_c[2];    // +0x0C..+0x13
        struct {
            float velScale;        // +0x0C — used as bounce/restitution multiplier
            char   _pad[4];
        };
    };
    float  posX;             // +0x10
    float  posY;             // +0x18
    float  velX;             // +0x1C (lower float)
    float  velY;             // +0x20
    float  velZ;             // +0x24
    int    collisionMode;    // +0x28
    void  *collisionCtx;     // +0x30

    // Equivalent of default constructor.
    FXDynamicsNewton()
    {
        scale      = 1.0f;
        drag       = 1.0f;
        restitution= 0.0f;
        velScale   = 0.2f;            // 0x3e4ccccd ≈ 0.2f
        posX       = 0.0f;
        posY       = 0.0f;
        velX       = 0.0f;
        velY       = 0.0f;
        velZ       = 0.0f;
        collisionMode = 0;
        collisionCtx  = const_cast<void*>(&g_fxNewtonDefaultVal);
    }

    // Equivalent of copy ctor from "params".
    FXDynamicsNewton(const FXDynamicsNewtonParams& src)
    {
        scale        = src.m1;        // param_1+0x08
        drag         = src.m2;        // param_1+0x0c
        restitution  = src.m3;        // param_1+0x10
        // +0x10.. — copied as 8-byte value
        posX         = src.v1.x;      // param_1+0x18
        posY         = src.v1.y;      // param_1+0x20
        velX         = src.v2.x;      // param_1+0x24
        velY         = src.v2.y;      // param_1+0x2c
        velZ         = src.v2.z;      // param_1+0x30
        collisionMode= src.mode;      // param_1+0x38-ish
        collisionCtx = src.collision; // param_1+0x38 as ptr
    }
};

// Wrapper for parameters coming from outside (param_1 in vfunction8 is
// a context/task pointer, param_2 is time scale / delta).
struct FXDynamicsNewtonStepInput
{
    void*   ctx;
    float   local_48[9]; // not used directly here, only as a transfer buffer
};

// "Magic" driver function for accel/noise (Ghidra — FUN_140d95ef0).
// From context: takes "raw" velocity, normalizes and scales it.
extern float fxNewtonNormalizeAndCap(float raw, float ref, float scale);

// Handing result to the visualization system (Ghidra — FUN_140d826a0).
extern void fxNewtonWriteVelocity(float out[3], const float vel[3]);

// Returns true if the particle "escaped" this frame / went out of bounds.
bool stepFXDynamicsNewton(
    FXDynamicsNewton& self,
    const FXDynamicsNewtonStepInput& input,
    float timeScale   // param_2 — often local frame time or speed scale
)
{
    constexpr float MAX_VEL_SQ = 360000.0f;   // "360000.0" in code
    constexpr float MAX_POS_SQ = 4000000.0f;  // "4e+06" in code

    // ---------- drag-like velocity filter ----------
    float s = self.scale;
    float dragFactor = s * g_fxNewtonDragScale; // fVar4
    float velScale   = self.velScale;           // fVar5, reused

    float newVX = velScale * g_fxNewtonAccelX * timeScale + self.velX; // fVar5
    float newVY = s * g_fxNewtonAccelY * timeScale + self.velY;
    float newVZ = s * g_fxNewtonAccelZ * timeScale + self.velZ;

    // "absorb" with past values (in code — fVar7 += ...)
    newVX = dragFactor * timeScale + newVX; // fVar7 (overwritten)
    newVY = s * g_fxNewtonAccelY * timeScale + newVY;
    newVZ = velScale * g_fxNewtonAccelZ * timeScale + newVZ;

    self.velX = newVX;
    self.velY = newVY;
    self.velZ = newVZ;

    // ---------- another drag wave / axis corrections ----------
    float vel2 = self.velX;  // re-read self.velX
    float d2 = vel2 * g_fxNewtonDragScale; // fVar4
    float v3 = vel2 * g_fxNewtonDragX * timeScale + self.velX;
    float v4 = d2 * timeScale + self.velY;
    float v5 = vel2 * g_fxNewtonDragZ * timeScale + self.velZ;

    self.velX = v3;
    self.velY = v4;
    self.velZ = v5;

    // normalize direction through "Magic" (Ghidra FUN_140d95ef0).
    float mag = fxNewtonNormalizeAndCap(self.velX, self.restitution, timeScale);

    float dirX = mag * self.velX;
    float dirY = mag * self.velY;
    float dirZ = mag * self.velZ;

    self.velX = dirX;
    self.velY = dirY;
    self.velZ = dirZ;

    // ---------- speed clamp ----------
    float speedSq = dirX*dirX + dirY*dirY + dirZ*dirZ;
    if (speedSq > MAX_VEL_SQ)
    {
        // safety net "slow to 600"
        float invLen = 1.0f / std::sqrt(speedSq);
        self.velX = invLen * dirX * 600.0f;
        self.velY = invLen * dirY * 600.0f;
        self.velZ = invLen * dirZ * 600.0f;

        // encode "success but wild" status
        uint8_t status = (static_cast<uint32_t>(invLen * 600.0f) >> 8) & 0xFF;
        return (status & 1) != 0; // Ghidra return semantics — status code
    }

    // ---------- predict new position ----------
    float newPosX = dirX * timeScale + self.posX;
    float newPosY = dirY * timeScale + self.posY;
    float newPosZ = dirZ * timeScale + self.posZ;

    // ---------- distance clamp ----------
    float posSq = newPosX*newPosX + newPosY*newPosY + newPosZ*newPosZ;
    if (posSq > MAX_POS_SQ)
    {
        // "went very far" — status code via local_60 (velZ?)
        uint32_t raw = static_cast<uint32_t>(self.velZ) >> 8;
        return (raw & 1) != 0;
    }

    // ---------- collision step ----------
    float hitPos[3]  = { newPosX, newPosY, newPosZ };
    float hitVel[3]  = { dirX, dirY, dirZ };
    float extraOut[9];
    float restitutionFromObj = g_fxNewtonDefaultVal; // param_7-ish

    bool collisionHit = false;
    if (self.collisionCtx != nullptr)
    {
        // vtable call: (**(*\*plVar2+0x10))(plVar2, &pos, &toPos, &hitOut, &out2)
        // Reconstructed as:
        //   collide(collisionCtx, fromPos, toPos, hitPos, &collisionInfo)
        collisionHit = invokeMeshCollision(
            self.collisionCtx,
            &self.posX, &hitPos[0], hitPos, &restitutionFromObj
        );
    }

    if (collisionHit)
    {
        // distance from "old" to "new(reflected)"
        float dx = hitPos[0] - self.posX;
        float dy = hitPos[1] - self.posY;
        float dz = hitPos[2] - self.posZ;
        float dSq = dx*dx + dy*dy + dz*dz;

        if (restitutionFromObj == 0.0f)
        {
            // particle "stuck" / remained in place
            hitPos[0] = self.posX;
            hitPos[1] = self.posY;
            hitPos[2] = self.posZ;
        }
        else if (dSq < 1.1920929e-07f || dSq < timeScale*timeScale)
        {
            // hit the same point / stuck — place at previous position
            hitPos[0] = self.posX;
            hitPos[1] = self.posY;
            hitPos[2] = self.posZ;
        }
        else
        {
            // correct by step back/forward
            hitPos[0] = hitPos[0] - dirX * timeScale;
            hitPos[1] = hitPos[1] - dirY * timeScale;
            hitPos[2] = hitPos[2] - dirZ * timeScale;
        }

        // ----------- mode 1: full reflection decomposition -----------
        if (self.collisionMode == 1)
        {
            float fromDX = self.posX - hitPos[0];
            float fromDY = self.posY - hitPos[1];
            float fromDZ = self.posZ - hitPos[2];
            float diffSq = fromDX*fromDX + fromDY*fromDY + fromDZ*fromDZ;

            float diffLen = (diffSq < 0.0f) ? std::sqrt(-diffSq) : std::sqrt(diffSq);
            float invDiff = 1.0f / diffLen;

            float bounce = 1.0f - self.velScale; // offset_0xc → 1 - bounce
            float normalDot = hitVel[0]*fromDY*invDiff + hitVel[2]*fromDZ*invDiff + /*...*/ 0.0f;
            // ... (reconstructed from local_100 * 2.0 * fVar9 - ...)
            // Simplified: post-impact vector = incident - 2 * (incident·n) * n

            // then decompose by collision velocity (param_5-ish)
            float rvelLenSq = hitVel[1]*hitVel[1] + hitVel[0]*hitVel[0] + hitVel[2]*hitVel[2];
            float rvelLen = (rvelLenSq < 0.0f) ? std::sqrt(-rvelLenSq) : std::sqrt(rvelLenSq);
            float invRvel = 1.0f / rvelLen;

            float scaleFactor = invRvel * bounce * 1.0f;
            hitVel[0] = scaleFactor * hitVel[0];
            hitVel[1] = scaleFactor * hitVel[1];
            hitVel[2] = scaleFactor * hitVel[2];

            // decompose by impact component...
            float cvx = hitVel[0];
            float cvy = hitVel[1];
            float cvz = hitVel[2];
            float cvLenSq = cvx*cvx + cvy*cvy + cvz*cvz;

            float reflectedLen = std::sqrt(cvLenSq);
            float invReflected = 1.0f / reflectedLen;
            float rf = invReflected * cvx * rvelLen * bounce;
            float rfY = invReflected * cvy * rvelLen * bounce;
            float rfZ = invReflected * cvz * rvelLen * bounce;

            hitVel[0] = rf;
            hitVel[1] = rfY;
            hitVel[2] = rfZ;

            // interpolate position between old and new by weight fVar7
            float diffOld[3] = { hitPos[0] - self.posX, hitPos[1] - self.posY, hitPos[2] - self.posZ };
            float dDotN = std::abs(
                diffOld[0]*hitVel[0] + diffOld[1]*hitVel[1] + diffOld[2]*hitVel[2]);
            float dDotN2 = std::abs(
                diffOld[0]*hitVel[0] + diffOld[1]*hitVel[1] + diffOld[2]*hitVel[2]) + dDotN;
            float t = (dDotN2 <= 0.0f) ? 0.0f : dDotN / dDotN2;

            hitPos[0] = rf * t * timeScale + hitPos[0]; // param_6 = timeScale
            hitPos[1] = rfY * t * timeScale + hitPos[1];
            hitPos[2] = rfZ * t * timeScale + hitPos[2];

            self.posX = hitPos[0];
            self.posY = hitPos[1];
            self.posZ = hitPos[2];

            return 0;
        }
        else if (self.collisionMode == 2)
        {
            // mode 2 — just return "hit against plane" (code returns 1)
            return 1;
        }
        else if (self.collisionMode == 3)
        {
            return 0; // or 1? in code — "if iVar1 != 3 return 0"
        }
        else
        {
            // simple reset to uVar6 (input position param)
            self.posX = hitPos[0];
            self.posY = hitPos[1];
            self.posZ = hitPos[2];
            hitVel[0] = 0.0f;
            hitVel[1] = 0.0f;
            hitVel[2] = 0.0f;
            return 0;
        }
    }

    // no collision — just move along predicted direction
    self.posX = hitPos[0];
    self.posY = hitPos[1];
    self.posZ = hitPos[2];
    hitVel[0] = dirX; // or param_5 (input velocity)
    hitVel[1] = dirY;
    hitVel[2] = dirZ;

    // status code from one component (in Ghidra — param_5[2])
    uint32_t statusRaw = static_cast<uint32_t>(dirZ) >> 8;
    return (statusRaw & 0xFF) != 0; // "status bit" semantics
}

// -----------------------------------------------------
// FXDynamicsNewton::vfunction9 — collision/reflection resolution
// (pulled out separately, since used by many descendants)
// -----------------------------------------------------
struct CollisionResult
{
    float outPos[3];  // param_1 (output position)
    float outVel[3];  // param_2 (output velocity)
};

bool resolveNewtonCollision(
    FXDynamicsNewton& self,
    const float fromPos[3],
    const float toPos[3],
    const float inVel[3],
    float restitutionGlobal,   // param_6
    float stepScale,           // param_7
    CollisionResult& result
)
{
    // If motion is "zero" — fast exit
    if (toPos[0] == fromPos[0] && toPos[1] == fromPos[1] && toPos[2] == fromPos[2])
        return false;

    float diffX = toPos[0] - fromPos[0];
    float diffY = toPos[1] - fromPos[1];
    float diffZ = toPos[2] - fromPos[2];
    float diffLenSq = diffX*diffX + diffY*diffY + diffZ*diffZ;

    if (diffLenSq < 0.0f)
        diffLenSq = std::sqrt(-diffLenSq);
    else
        diffLenSq = std::sqrt(diffLenSq);

    float invDiffLen = 1.0f / diffLenSq;
    float dirX = diffX * invDiffLen;
    float dirY = invDiffLen * diffY;
    float dirZ = invDiffLen * diffZ;

    // --------------------
    // collision call (vtable gateway)
    // --------------------
    float hitOut[2];
    bool collided = false;
    if (self.collisionCtx)
    {
        collided = invokeMeshCollision(
            self.collisionCtx,
            fromPos,
            &toPos[0],
            &result.outPos[0],
            &hitOut[0]
        );
    }

    if (!collided)
        return false;  // control goes to LAB_140d9c771 — "just passed through"

    // distance from the reflected point to fromPos
    float hx = result.outPos[0] - fromPos[0];
    float hy = result.outPos[1] - fromPos[1];
    float hz = result.outPos[2] - fromPos[2];
    float dHitSq = hy*hy + hx*hx + hz*hz;

    const float eps = 1.1920929e-07f;
    if (dHitSq < eps || dHitSq < stepScale*stepScale)
    {
        // "stuck" — use a "safe" point
        if (hitOut[0] * dirX + hitOut[1] * /* local_fc */ 0.0f + dirZ * /*local_f8*/0.0f > 0.0f)
        {
            result.outPos[0] = fromPos[0];
            result.outPos[1] = fromPos[1];
            result.outPos[2] = fromPos[2];
        }
        else
        {
            result.outPos[0] = result.outPos[0] - dirX * stepScale;
            result.outPos[1] = result.outPos[1] - dirY * stepScale;
            result.outPos[2] = result.outPos[2] - dirZ * stepScale;
        }
    }

    // -----------
    // mode 1: full reflection
    // -----------
    if (self.collisionMode == 1)
    {
        float bX = fromPos[0] - result.outPos[0];
        float bY = fromPos[1] - result.outPos[1];
        float bZ = fromPos[2] - result.outPos[2];
        float bLenSq = bY*bY + bX*bX + bZ*bZ;

        float bLen = (bLenSq < 0.0f) ? std::sqrt(-bLenSq) : std::sqrt(bLenSq);
        float invB = 1.0f / bLen;
        float bounceFactor = 1.0f - self.velScale;

        float dot = hitOut[0] * bY * invB + /*local_100*/hitOut[0] * bX * invB + /*local_f8*/hitOut[0] * bZ * invB;
        float refX = hitOut[0] * 2.0f * dot - bX * invB;
        float refY = hitOut[0] * 2.0f * dot - bY * invB;
        float refZ = /*local_f8*/ hitOut[0] * 2.0f * dot - bZ * invB;

        float velLenSq = inVel[1]*inVel[1] + inVel[0]*inVel[0] + inVel[2]*inVel[2];
        float velLen = (velLenSq < 0.0f) ? std::sqrt(-velLenSq) : std::sqrt(velLenSq);
        float invVel = 1.0f / velLen;

        float rvx = refX, rvy = refY, rvz = refZ;
        float rLenSq = rvy*rvy + rvx*rvx + rvz*rvz;
        float rLen = (rLenSq < 0.0f) ? std::sqrt(-rLenSq) : std::sqrt(rLenSq);
        float invR = 1.0f / rLen;

        float scale2 = invR * rvx * velLen * bounceFactor;
        result.outVel[0] = scale2;
        result.outVel[1] = invR * rvy * velLen * bounceFactor;
        result.outVel[2] = invR * rvz * velLen * bounceFactor;

        // position interpolation
        float dDot1 = std::abs((result.outPos[0] - toPos[0])*inVel[0] +
                               (result.outPos[1] - toPos[1])*inVel[1] +
                               (result.outPos[2] - toPos[2])*inVel[2]);
        float dDot2 = dDot1 + std::abs((result.outPos[0] - fromPos[0])*inVel[0] +
                                        (result.outPos[1] - fromPos[1])*inVel[1] +
                                        (result.outPos[2] - fromPos[2])*inVel[2]);
        float t2 = (dDot2 <= 0.0f) ? 0.0f : dDot1 / dDot2;

        result.outPos[0] = scale2 * t2 * restitutionGlobal + result.outPos[0];
        result.outPos[1] = result.outVel[1] * t2 * restitutionGlobal + result.outPos[1];
        result.outPos[2] = result.outVel[2] * t2 * restitutionGlobal + result.outPos[2];

        return true;
    }
    else if (self.collisionMode == 2)
    {
        // simple exit — hit-vector value
        result.outPos[0] = result.outPos[0]; // uVar6
        result.outPos[2] = /*fVar4*/ result.outPos[2];
        result.outVel[0] = 0.0f;
        result.outVel[1] = 0.0f;
        result.outVel[2] = 0.0f;
        return true; // "1" as collision status return code
    }
    else if (self.collisionMode == 3)
    {
        return true; // code 1
    }
    else
    {
        // code 0 — "not processed/ignore"
        return false;
    }
}

// -----------------------------------------------------
// FXDynamicsNewtonLength::vfunction9 — length clamp
// -----------------------------------------------------
bool stepFXDynamicsNewtonLength(
    FXDynamicsNewtonLength& self,
    const float pos[3],
    const float vel[3],
    float inScale,
    CollisionResult& result
)
{
    float limX = self.limX;          // offset_0x00
    float limY = self.limY;          // offset_0x04
    float limZ = self.limZ;          // offset_0x08
    float maxDist = self.maxDist;    // offset_0x0C

    float ddx = pos[0] - limX;
    float ddy = pos[1] - limY;
    float ddz = pos[2] - limZ;
    float dSq = ddy*ddy + ddx*ddx + ddz*ddz;

    float dist = (dSq < 0.0f) ? std::sqrt(-dSq) : std::sqrt(dSq);

    if (dist <= maxDist)
    {
        // within bounds — delegate to base Newton
        CollisionResult tmp;
        bool hit = resolveNewtonCollision(
            *reinterpret_cast<FXDynamicsNewton*>(&self),
            &self.posX, &pos[0], &vel[0], inScale, inScale, tmp
        );
        result = tmp;
        return hit;
    }
    else
    {
        if (dist <= 1e-06f)
        {
            result.outPos[0] = limX;
            result.outPos[1] = limY;
            result.outPos[2] = limZ;
            result.outVel[0] = 0.0f;
            result.outVel[1] = 0.0f;
            result.outVel[2] = 0.0f;
            result.outPos[0] = limX;
            result.outPos[1] = limY;
            result.outPos[2] = limZ;
        }
        else
        {
            float invD = 1.0f / dist;
            float pullX = invD * ddy * maxDist; // note: indices swapped in decompilation
            float pullY = invD * ddx * maxDist;
            float pullZ = invD * ddz * maxDist;

            result.outPos[0] = limX + pullX;
            result.outPos[1] = limY + pullY;
            result.outPos[2] = limZ + pullZ;
        }

        result.outVel[0] = vel[0]; // or param_5
        result.outVel[2] = vel[2];
        result.outVel[1] = vel[1];
        // param_8 flag = 1
        return true;
    }
}
```

Refactoring notes:
- `invokeMeshCollision` and `fxNewtonNormalizeAndCap` / `fxNewtonWriteVelocity` are incompletely restored gateways
  (originally `FUN_140d95ef0`, `FUN_140d826a0` and vtable calls through `plVar2`).
  I left them as external functions because their internals go beyond the excerpt analyzed.
- Exact constant values (`1.1920929e-07f` — `FLT_EPSILON`-like, `360000.0` and `4000000.0` — squared clamps,
  `600.0` — extreme normalized speed) come straight from the decompiled text.

---

## 4. Dictionary for Ghidra

Suggested names for `FXDynamicsNewton_data` and local variables in `vfunction8`/`vfunction9`.

### Object fields (offsets in `FXDynamicsNewton_data`)

| Old name (Ghidra form) | New name | Explanation |
|---------------------------|-----------|-----------|
| `(this->FXDynamicsNewton_data).offset_0x0` / `fVar3` read | `scale` | acceleration/push multiplier on X and overall step parameter |
| `(this->FXDynamicsNewton_data).offset_0x4` / `fVar3` second block | `drag` | velocity damping coefficient (animate drag) |
| `(this->FXDynamicsNewton_data).offset_0x8` / `fVar4` | `restitution` | restitution/bounce on impact (used as `1 - offset_0xc` in `vfunction9`) |
| `(this->FXDynamicsNewton_data).offset_0xc` / `fVar10` | `velScale` | "remaining velocity" weight after impact / bounce coefficient (often read together with `+0x08`) |
| `(this->FXDynamicsNewton_data).offset_0x10` / `local_58` | `posX` | X component of position |
| `(this->FXDynamicsNewton_data).offset_0x18` / `local_50` | `posY` | Y component of position |
| `(this->FXDynamicsNewton_data).offset_0x1c` / `local_68` | `velX` | X component of velocity (often read in pair with `+0x20`) |
| `(this->FXDynamicsNewton_data).offset_0x20` | `velY` | Y component of velocity |
| `(this->FXDynamicsNewton_data).offset_0x24` / `local_60` | `velZ` | Z component of velocity |
| `(this->FXDynamicsNewton_data).offset_0x28` / `iVar1` | `collisionMode` | collision mode: 0=none, 1=full reflect/resolve, 2, 3 — other branches |
| `(this->FXDynamicsNewton_data).offset_0x30` / `plVar2` | `collisionCtx` | pointer to mesh collision context/object (passed into `vfunction9`) |

### Constants / globals (in Ghidra — `_DAT_...`)

| Old name | New name | Explanation |
|------------|-----------|-----------|
| `_DAT_141715bf0` | `g_fxNewtonAccelX` | base acceleration on X for this particle type |
| `_DAT_141715bf4` | `g_fxNewtonAccelY` | base acceleration on Y |
| `_DAT_141715bf8` | `g_fxNewtonAccelZ` | base acceleration on Z |
| `_DAT_141764738` | `g_fxNewtonDragScale` | drag multiplier on X / speed retention |
| `_DAT_141764730` | `g_fxNewtonDragX` | drag component on X in step (`fVar5`) |
| `_DAT_141764734` | `g_fxNewtonDragY` | drag component on Y |
| `DAT_141764728` | `g_fxNewtonDefaultRestitution` | default restitution value / initial context |

### Local variables `vfunction8`

| Old name | New name | Explanation |
|------------|-----------|-----------|
| `param_2` | `timeScale` | time scale / frame delta or speedFactor |
| `fVar3` (first block) | `scale` | read of `self.scale` |
| `fVar5` (second block) | `velX` | rewritten velocity field |
| `fVar7` (in drag block) | `dragAccelX` / `newVX` | intermediate acceleration + drag value |
| `local_78`, `local_74`, `local_70` | `newPosX`, `newPosY`, `newPosZ` | predicted new position |
| `local_68`, `local_60`, `local_58` (second set) | `velX`, `velY`, `velZ` | current velocity for collision handoff |
| `local_48[9]` | `sScratch[9]` | buffer for handing result to visualization (Ghidra FUN_140d826a0) |
| `uVar2` (status) | `status` | particle status code (bool/int hybrid) |

### Local variables `vfunction9`

| Old name | New name | Explanation |
|------------|-----------|-----------|
| `param_1` (float*) | `outPos` | output position after collision |
| `param_2` (float*) | `outVel` | output velocity after collision |
| `param_3`, `param_4` | `fromPos`, `toPos` | start and target positions of the step |
| `param_5` | `inVel` | input velocity (often "original" before reflection) |
| `param_6` | `restitutionGlobal` | global restitution/time-weight (used when interpolating `outPos`) |
| `param_7` | `stepScale` | step size / timeScale for correcting reflected point |
| `fVar4` (first block) | `stepVectorX` | step offset on X (`toX - fromX`) |
| `fVar5`, `fVar6`, `fVar7` | `stepLenSq`, `dirX`, `dir` | squared step length → normalized direction |
| `local_fc`, `local_100`, `local_f8` | `normalY`, `normalX`, `normalZ` | normal/reflected direction components when computing `v - 2*(v·n)*n` |
| `local_e0` | `fromPosAsVec` | input position repacked into `undefined8` |
| `uVar6` | `hitTargetPos` | target point returned when mode != 1 |
| `fVar10`, `fVar11`, `fVar12` | `reflectedVelX/Y/Z` | velocity projection after reflection |

---

## 5. What was saved

- The analysis and cleaned code are saved in this same file: `fx_physics_explained_en.md`.
- The file contains:
  1. a plain-language explanation of the physics model,
  2. a table of struct fields by offset,
  3. a cleaned C++ equivalent of `FXDynamicsNewton::step`, `resolveNewtonCollision`, and `FXDynamicsNewtonLength`,
  4. a Ghidra rename dictionary (object fields, global constants, local variables).
