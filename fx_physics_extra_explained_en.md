# FX Physics — Extended Analysis (Planar, Gravwell, Weather, FXHandler)

Source file: `fx_and_particles_and_anims_and_vis.c` (MSVC x64, Dawn of War Essence Engine FX subsystem).
Related header: `fx_and_particles_and_anims_and_vis.h`.

Source lines referenced below are pinned to the current file on disk, so the
Evidence points in each section can be re-checked directly from
`fx_and_particles_and_anims_and_vis.c` and `fx_and_particles_and_anims_and_vis.h`.

This document extends the previous analysis (`fx_physics_explained_en.md`) with:
- `FXDynamicsPlanar` — planar collision/clamp for FX particles,
- `FXDynamicsGravwell` — local gravity-well pull,
- `FXWeatherDynamics` — global weather/wind field with random scatter and table interpolation,
- `FXHandler::vfunction3` / `vfunction4` — FX job culling and per-frame synchronization in the simulation/render pipeline,
- `FXCollisionSurfPlanar` — a planar surface collision primitive used by `FXDynamicsPlanar`.

---

## 1. FXDynamicsPlanar — planar surface for FX particles

Related C evidence:
- header type/vftable: [fx_and_particles_and_anims_and_vis.h:29958](fx_and_particles_and_anims_and_vis.h#L29958), [fx_and_particles_and_anims_and_vis.h:29966](fx_and_particles_and_anims_and_vis.h#L29966)
- property getter bodies: [fx_and_particles_and_anims_and_vis.c:6553](fx_and_particles_and_anims_and_vis.c#L6553), [fx_and_particles_and_anims_and_vis.c:6571](fx_and_particles_and_anims_and_vis.c#L6571)
- inline accessor/serialization pattern used by this property type: same pool paths used by `FXPropT<Math::Vector3f,...>` in the same file

This extra block is deliberately narrow: it does not claim to recover the full semantics of the property system; it only documents the surfaces most relevant to the FX axis question.

### 1.a. `Emitter_Direction` as a first-class vector property, not a global axis constant

The closest C concept to "FX is authored along some axis" is the animated vector
property infrastructure, and the property name that already exists in the Go layer
is `Emitter_Direction`.

Relevant evidence for the property side:

- [fx_and_particles_and_anims_and_vis.h:29958](fx_and_particles_and_anims_and_vis.h#L29958)
- [fx_and_particles_and_anims_and_vis.h:29966](fx_and_particles_and_anims_and_vis.h#L29966)
- [fx_and_particles_and_anims_and_vis.c:6553](fx_and_particles_and_anims_and_vis.c#L6553)
- [fx_and_particles_and_anims_and_vis.c:6571](fx_and_particles_and_anims_and_vis.c#L6571)

That is:
- `FXPropT<Math::Vector3f, FXPropPredUnchecked>` — a variant of the generic
  property template specialized to a 3-float vector.
- `FXPropT<...>_vftable` declares `vfunction1/vfunction2/vfunction3` on the
  property type.
- `FXPropT<>::vfunction3(FXPropT<> *this, int *param_1)` is the concrete getter
  that produces an `int *` output.

There is no `new/delete` inside these property getter bodies; they reuse a
lifetime-managing allocator `FUN_140d95a30(size)` and serialize the property back
through the same pool path used everywhere else in this translation unit.

The important point for axis work: the direction is **a value stored in a named
property that the runtime re-reads and deserializes like any other per-instance
state**. It is not a per-module global or a fixed convention baked into the
dynamics math itself.

### 1.b. The Go layer already picked an interpretation

The semantics creator already inferred `Emitter_Direction` as "the spray's
local axis, default +Z when a file does not declare it":

- [internal/fx/sim.go:159](internal/fx/sim.go#L159)
- [x_test_fx_frame_test.go:67](x_test_fx_frame_test.go#L67)

Specifically:
- `the root emitter's authored direction is the spray's local axis;`
- `default to +Z only when a file does not declare it.`
- authored value `{ 1, 0, 0 }` is treated as "renderer-local +X".

This matters because it is the first place where the project has already chosen an
interpretation of what "the FX axis" means:

- authored `{ 1, 0, 0 }` = the emitter shoots along its local +X,
- missing field → fallback `+Z` = `(0,0,1)`.

That is a **model decision**, not yet a confirmed convention from the C file.

### 1.c. The 9-float buffer is packed in XYZ order

The per-step callbacks pass a `local_48[9]` / `local_38[9]` scratch buffer into
`FUN_140d826a0(local_XX, &velocity)`, and that same buffer is then handed to the
renderer callback `(*param_1 + 8)(param_1, local_XX)`.

The buffer is used in:

- [fx_and_particles_and_anims_and_vis.c:6945](fx_and_particles_and_anims_and_vis.c#L6945)
  — `FXDynamicsNewton`
- [fx_and_particles_and_anims_and_vis.c:7112](fx_and_particles_and_anims_and_vis.c#L7112)
  — `FXDynamicsPlanar`
- [fx_and_particles_and_anims_and_vis.c:8029](fx_and_particles_and_anims_and_vis.c#L8029)
  — `FXWeatherDynamics`

From the body of the dynamics step functions and the way positions/velocities are
written into it, the buffer is packed in XYZ order:
- index `0` → X component of first vector
- index `1` → Y component of first vector
- index `2` → Z component of first vector
- index `3` → X component of second vector
- index `4` → Y component of second vector
- index `5` → Z component of second vector
- ...

The exact semantic of each of the three vectors is the part that belongs to the
review of the render/overlay layer and the tests, not to this C summary.

Header fields (`FXDynamicsPlanar_data`):

| Offset | Type (inferred) | Inferred purpose |
|--------|----------------|------------------|
| `+0x00` | `uint8_t` / flag | plane active flag (`vfunction3`/`vfunction5` branch on it) |
| `+0x04..0x0C` | `float[3]` (vec3) | plane normal/definition; read as `field_0x4`, `field_0x8`, `field_0xC` |
| `+0x10..0x18` | `float[3]` (vec3) | another plane-defined point/vector (in `vfunction5`) |
| `+0x1C` | `float` (part of vec3 with `+0x20`) | velocity/position component used by `vfunction8` and `vfunction3` |
| `+0x20` | `float[3]` (vec3) | base plane vector (used in dot products) |
| `+0x28` | `float` | plane offset / d (constant term in `n·p + d`) |
| `+0x2C` | — | not used by the current reconstructed read of this type |

Notes:
- In ctor, `offset_0x1C = 0`, `offset_0x20 = 1.0f`, `offset_0x28 = 0`, `offset_0x0 = 0`, i.e. an inactive unit plane.

### vfunction3 — project onto the plane

Signature: `void FXDynamicsPlanar::vfunction3(FXDynamicsPlanar *this, undefined8 *param_1)`.

Behavior (reconstructed):
1. Writes `*param_1` into `field_0x4` (lower 4 bytes) and `param_1[1]` into `field_0xC` (next 4 bytes): i.e. sets a new plane normal/point from outside.
2. Reads the three plane components:
   - `fVar1 = field_0x4`
   - `fVar2 = field_0x8`
   - `fVar3 = field_0xC`
3. Computes `fVar4 = -(fVar2 * plane._0x20 + fVar1 * plane._0x1C + fVar3 * plane._0x20_next + plane._0x28)`.
   This is a **signed plane offset** `-(n·p + d)` form: `fVar4` is the signed distance of a point to the plane (with sign chosen by convention).
4. Then projects/adjusts the plane-affected values:
   - `field_0x4 = fVar4 * plane._0x1C + fVar1`
   - `field_0x8 = fVar4 * plane._0x20 + fVar2`
   - `field_0xC = fVar4 * plane._0x20_next + fVar3`
So `vfunction3` takes a plane definition from outside and updates the stored plane-dependent vector using the signed distance to the plane. This is the typical "push point to plane" or "recompute plane-relative value" step.

### vfunction5 — same projection, only if active

Signature: `void FXDynamicsPlanar::vfunction5(FXDynamicsPlanar *this, undefined8 *param_1)`.

Behavior:
- If `offset_0x0 != 0`:
  1. Writes `*param_1` into `field_0x10` and `param_1[1]` into `field_0x18`.
  2. Computes `fVar4 = fVar2 * plane._0x20 + fVar1 * plane._0x1C + fVar3 * plane._0x20_next` (signed distance-ish, without the `d` term).
  3. Subtracts the plane component from the point:
     - `field_0x10 = fVar1 - fVar4 * plane._0x1C`
     - `field_0x14 = fVar2 - fVar4 * plane._0x20`
     - `field_0x18 = fVar3 - fVar4 * plane._0x20_next`
  This is another **remove plane component / project onto plane normal direction** step, gated by the active flag.
- If inactive, zeroes `field_0x10` and `field_0x18`.

So:
- `vfunction3` is used when the planar surface is being **defined** and its effect recalculated;
- `vfunction5` is used when the planar surface is already active and we need to **remove its component** from a particle value (e.g., constrain velocity or position away from the plane).

### vfunction8 — push plane data to visualization

Signature: `ulonglong FXDynamicsPlanar::vfunction8(FXDynamicsPlanar *this, longlong *param_1)`.

Behavior:
1. Calls `FUN_140d826a0(local_38, &this->offset_0x1C)`: packs/copies the plane velocity/state into a 9-float scratch buffer.
2. Reads `field_0xC` and `field_0x4` (the plane normal/point).
3. Calls callback `(**(*param_1 + 8))(param_1, local_38)`: hands the plane state to the visualization/simulation side.
4. Returns `uVar1 & 0xffffffffffffff00` (status with low bits masked out).

So `vfunction8` is the **output thunk**: collect plane state and forward it. It does not compute physics by itself; it mirrors the conclusion of planar interaction to downstream consumers.

### vfunction9 — set active flag

Signature: `void FXDynamicsPlanar::vfunction9(FXDynamicsPlanar *this, undefined1 param_1)`.
It simply sets `offset_0x0 = param_1`. So `vfunction9` is the **activate/deactivate** setter for the plane.

---

## 2. FXDynamicsGravwell — local gravity well (pull to a point)

Header fields (`FXDynamicsGravwell_data`):

| Offset | Type (inferred) | Inferred purpose |
|--------|----------------|------------------|
| `+0x00` | `float[3]` (vec3) | well center position (read as `offset_0x0` in ctor and vfunction8) |
| `+0x08` | `float` (or 4 bytes of a vec3) | velocity/scale component (`offset_0x8` is read/written in vfunction8) |
| `+0x0C..0x0F` | `float[?`] / remaining fields | rest of the local state (written in `vfunction3`, `vfunction5`, used in `vfunction8`) |

Default ctor sets `offset_0x0 = 0`, `offset_0x8 = 0`, then overwrites `offset_0x0` with `DAT_141764680` and `offset_0x8` with `_DAT_141764688`. So the default well is "inactive / placeholder" until configured, then a default center/state is applied.

### vfunction8 — apply gravwell pull per frame

Signature: `ulonglong FXDynamicsGravwell::vfunction8(FXDynamicsGravwell *this, longlong *param_1, float param_2)`.

Behavior (reconstructed):
1. If `offset_0x0 == DAT_141764680` and `offset_0x8 == DAT_141764688` (i.e. still in default placeholder form):
   - overwrite `offset_0x0 = param_1[4]`, `offset_0x8 = param_1[5]` (configure the well from external task data).
2. Compute `fVar9 = 0.125`; if `param_2 <= 0.125`, use `param_2` instead (frame time / scale capping).
3. Call `FUN_140d97ec0(DAT_141764678, &this->offset_0x0)` to read some well state — returns `uVar4` (a scalar) and `extraout_XMM0_Da/Db` (a float pair).
4. If that call succeeded (char != 0):
   - Compute direction from current context to well center:
     - `fVar10 = local_64 - vftablePtr_x` (some current X)
     - `fVar11 = local_60 - vftablePtr_y` (some current Y)
     - `fVar12 = local_5c - offset_0x0` (some current Z)
   - Compute `fVar8 = fVar11^2 + fVar10^2 + fVar12^2`, normalize to `inv = 1/|dir|`.
   - Get `uVar4` again via `FUN_140d97b40(DAT_141764678, &this->offset_0x0)` (magnitude/strength of well), `fVar6 = uVar4 * fVar9` (scaled pull strength).
   - Read existing velocity-like components:
     - `fVar1 = offset_0x0_next_float` (low float of offset_0x0 + 4)
     - `fVar2 = offset_0x8` (existing Y or Z)
     - `fVar0` / `field_0xC` from `offset_0x0` (existing X or whatever)
   - Update the velocity-ish components toward the well:
     - `field_0xC = inv * fVar12 * fVar6 + field_0xC`
     - `offset_0x0_next = inv * fVar10 * fVar6 + fVar1`
     - `offset_0x8 = inv * fVar11 * fVar6 + fVar2`
5. After that, call `FUN_140d95ef0(CONCAT44(uVar7, fVar6), vftablePtr_z, fVar9)` — this normalizes/scales the well pull into a final factor `fVar10`.
6. Multiply the well velocity components by that factor:
   - `offset_0x8 = fVar10 * offset_0x8`
   - `field_0xC = fVar10 * field_0xC`
   - `offset_0x0_next = fVar10 * offset_0x0_next`
7. Move the position-like components (`vftablePtr` and `offset_0x0`) using the well velocity and `fVar9`:
   - `vftablePtr_x = fVar10 * fVar9 + old_vftablePtr_x`
   - `vftablePtr_y = fVar12 * fVar9 + old_vftablePtr_y`
   - `offset_0x0 = fVar11 * fVar9 + old_offset_0x0`
8. Then call callback `(**(*param_1 + 8))(param_1, &local_b8)` and return status masked by `0xffffffffffffff00`.

So the gravwell is a **point attractor**:
- it computes direction to the well,
- normalizes,
- scales by `wellStrength * min(delta, 0.125)`,
- adds that to velocity,
- then integrates position by `velocity * dt`-like step via `fVar9`.

In plain language: a particle gets pulled toward a local "gravity well" point; the pull is softened by `min(delta, 0.125)` and normalized, and the well can be configured at runtime from task data.

---

## 3. FXWeatherDynamics — global weather / wind field

Header fields (`FXWeatherDynamics_data`):

| Offset | Type (inferred) | Inferred purpose |
|--------|----------------|------------------|
| `+0x00` | `float` | weather intensity/active flag; `> 1e-6` means weather is on |
| `+0x04` | `float` / `float[?]` | weather scale/multiplier (used as `baseScale = offset_0x04`) |
| `+0x0C` | `float[3]` (vec3) | base wind/direction vector (lower part) |
| `+0x14` | `float[3]` (vec3) | base wind/direction vector (upper part) |
| `+0x1C` | `float[3]` (vec3) | current accumulated wind/velocity |
| `+0x24` | `float[3]` (vec3) | another wind/velocity component (often uppercased as `+0x24..+0x2C` in code) |
| `+0x2C` | `float[3]` (vec3) | yet another wind state (often used as "settled" wind direction) |

Default ctor sets `offset_0x4 = 1.0f`, `offset_0x0 = 0`, `offset_0x0C = 0.2f`, `offset_0x14 = 0`, `offset_0x1C = 0`, `offset_0x24 = 0`, `offset_0x2C = 0`. So the default is an inactive weather field with a small base scale (0.2) and unit multiplier (1.0).

### vfunction8 — weather step (the big one)

Signature: `ulonglong FXWeatherDynamics::vfunction8(FXWeatherDynamics *this, longlong *param_1, float param_2)`.

Behavior (reconstructed, in stages):

**0. No-op case**
- If `param_2 == 0.0`, return early status (via `register0x00000020` low bits). So weather step is skipped when delta is zero.

**1. Weather active path**
If `offset_0x0 > 1e-6`:
- Allocate/obtain local state via `FUN_140d870e0(2, &local_c8)`.
- Shuffle a lot of stack locals (uVar11..uVar5 etc.) — essentially loading current wind state into local variables.
- Set `uStack_8c = 1.0f` (a working constant).
- Prepare two 8-byte packed values `local_d8` and `uStack_d0` as `(uVar9,uVar8)` and `(uVar11,uVar10)` with a weird `^ 0x8000000080000000` transform — this looks like unpacking a packed wind direction/velocity into components.

**2. Sample "here" point and diff to base weather position**
- Call `FUN_14129aef0(&local_d8, &local_c8)` to get a "here" position `pfVar12` (likely a world point associated with the particle/effect).
- Compute diff to weather base position:
  - `fVar22 = pfVar12[0] - offset_0x0C_next`
  - `fVar23 = pfVar12[1] - offset_0x14`
  - `local_e0 = pfVar12[2] - offset_0x14_next`
- Compute `fVar19 = fVar23^2 + fVar22^2 + local_e0^2` (squared distance).
- Fast inverse square root on `fVar19` (the `0x5f3759df` magic + two Newton iterations).
- Multiply `local_e0` and `local_e8` by the inverse length (normalize direction).

**3. Random rotation / scatter**
- `fVar19 = FUN_140da45e0()` — random value.
- `FUN_140d801e0(&local_88, &local_e8, offset_0x0 * PI * (fVar19 - 0.5) * 0.5)` — build a rotation by angle `intensity * PI * (rand-0.5) * 0.5`.
  This is a **random scatter around the wind direction**, scaled by weather intensity.
- `FUN_140d84510(&offset_0x24_next, &local_88)` — store that rotation into the weather state (`offset_0x24` + 4 area).

**4. Normalize / smooth current weather direction**
- Compute `fVar23 = offset_0x2C^2 + offset_0x24_next^2 + offset_0x2C_next^2` (squared magnitude of some weather direction vector).
- Fast inverse sqrt again.
- Scale the weather direction components by that inverse sqrt and by `offset_0x0` (intensity) — so the wind direction is **re-normalized and scaled by intensity**.

**5. Look up weather "mode/table"**
- `local_d8 = param_1[4]`, `uStack_d0 = param_1[5]` — external parameters (probably weather mode indices).
- `lVar13 = FUN_140d97cf0(DAT_141764678, &local_d8)` — lookup weather data from the global state.
- Read `fVar19 = 1.0` if `tableValueAtOffset88 > 1.0`, else `fVar19 = that value`; then scale: `fVar19 = fVar19 / tableValueAtOffset68`.
- `pfVar12 = *(float**)(lVar3 + 0x160)` — pointer to a weather table (array of entries).
- `(lVar3 + 0x168) - pfVar12) >> 4` — compute number of table entries (`lVar13`).
- Linear search: advance `pfVar4` while `fVar19 > pfVar4[4]` and `lVar17 < entryCount`.
- Interpolate: `fVar19 = (fVar19 - pfVar12[lVar1*4]) / (pfVar12[lVar17*4] - pfVar12[lVar1*4])`.
- If the table entry components (indices 1/2/3) are equal between the two sample points, use the exact entry; otherwise **lerp** across components.
- If `(lVar3 + 0x6e)` has a flag set, apply `0.9 + 0.1` scale (small jitter/boost) to the weather vector components.

So this stage is **table-driven weather interpolation**: pick a sample from a weather table by a parameter, possibly lerp between two entries, with an optional subtle jitter.

**6. Add weather contribution to particle velocity**
- `fVar23 = offset_0x04`, `fVar21 = fVar23 * _DAT_141764748`, `fVar18 = fVar23 * _DAT_141764740`.
- Build new `offset_0x1C_next`:
  ```
  offset_0x1C_next += fVar23 * _DAT_141764744 * param_2 + fVar22 * param_2
  ```
- Build `offset_0x1C`:
  ```
  offset_0x1C += fVar18 * param_2 + fVar20 * param_2
  ```
- Build `offset_0x24`:
  ```
  offset_0x24 += fVar21 * param_2 + fVar19 * param_2
  ```
So weather adds to three velocity/state components (`offset_0x1C`/`0x24`) with a combination of:
  - global scale constants (`_DAT_141764748/740/744`),
  - weather table sample (`fVar20/22/19`),
  - frame time/scale `param_2`.

**7. Normalize final weather velocity**
- Read `fVar19 = offset_0x04_next` (some other component of the scale).
- `fVar20 = fxNewtonNormalizeAndCap((uint)fVar20, fVar19, param_2)` — normalize/scale the accumulated weather velocity.
- Then scale `offset_0x1C`/`0x24` by that factor.

**8. Position integration and bounds**
- If `|velocity|^2 <= 40000.0`:
  - compute new position:
    ```
    newPosX = velX * param_2 + offset_0x0C_next
    newPosY = velY * param_2 + offset_0x14
    newPosZ = velZ * param_2 + offset_0x14_next
    ```
  - If weather is active (`offset_0x0 > 1e-6`), also add an extra weather offset from `offset_0x24_next / 0x2C / 0x2C_next`.
  - If `|newPos|^2 <= 4e6` and `-150.0 <= newPosY` (or some component), accept the new position:
    - store new position into `offset_0x0C_next` (as packed 8-byte result) and `offset_0x14_next`.
    - if `|velocity| > 0.05`, call `FUN_140d7d7c0()` to get some additional state, `FUN_140d826a0(...)` to write velocity, and callback `(**(*param_1 + 8))(param_1, &local_88)`.
- Return status via low bits of `extraout_RAX`.

So weather is **a global wind field**:
- it can be turned on/off by intensity,
- it normalizes a direction with a random scatter proportional to intensity,
- it interpolates from a weather table (table lookup + lerp + optional jitter),
- it adds that wind to the particle velocity/position with global scale constants and frame time,
- it normalizes/clamps the final velocity,
- it integrates position,
- it has squared distance/speed clamps and a floor of `-150` on some component,
- and finally pushes state to the render/simulation side when velocity is nonzero.

In plain language: the weather system is not just "add a constant wind vector"; it is a **procedural wind with random scatter, table interpolation, intensity scaling, and integration into the particle's motion**, then clipped and forwarded.

---

## 4. FXHandler — FX simulation/render job control

Header (`FXHandler`):
- Inherits from `SimVisHandler` (so it's a SimVis subsystem with a name "FXHandler" and some default scale `0x3fc00000`).
- Has `FXHandler_data` with:
  - 7 StatGraph handles (`offset_0x0`, `0x08`, `0x10`, `0x18`, `0x20`, `0x28`, `0x30`),
  - `offset_0x40` = SRWLOCK,
  - `offset_0x38` = CONDITION_VARIABLE,
  - `offset_0x48` = a flag/state byte.

### vfunction4 — wait for frame completion and submit FX stats

Signature: `void FXHandler::vfunction4(FXHandler *this, longlong param_1)`.

High-level behavior:
1. Initialize local stat variables to 0 (local_b8, local_98, uStack_90, local_88, local_78, local_68, local_58).
2. `WaitOnCompleteYieldCore_CS()` — yield to the core during a “complete” phase.
3. Acquire the SRWLOCK on `FXHandler_data.field_0x40`.
4. Wait on the condition variable `FXHandler_data.field_0x38` until `offset_0x48` becomes nonzero (i.e., the FX job has flagged completion).
5. Once signaled, set `offset_0x48 = 0` under lock, release the lock.
6. `WaitOnCompleteResumeCore_CS()` — resume after yield.
7. `cVar3 = FUN_1411df180(this)` — some readiness / status check on the FX handler.
8. If `cVar3 != 0`:
   - grab a pointer `plVar1` from a global (`DAT_1417d5570 + 0x40`),
   - optionally inspect a flag at `*(param_1 + 8) + 0xc9` to set `cVar3 = 1`,
   - call some helper routines on `plVar1` (via `plVar1->vtable + 0x40`, `+0x38`, and `FUN_14116ef00`),
   - call `FUN_140d960f0(DAT_141764678, 0, param_1 + 0x18, param_1 + 0x10, uVar4, uVar6, uVar5, cVar3)` to collect FX output into `local_b8`,
   - call `FUN_140d98910(DAT_141764678, &local_b8)` to finalize/extract the stat.
9. Submit seven StatGraph channels with the computed stat values:
   - `FXHandler_data.offset_0x0` → local_b8,
   - `0x08` → local_98,
   - `0x10` → uStack_90,
   - `0x18` → local_88,
   - `0x20` → local_78,
   - `0x28` → local_68,
   - `0x30` → local_58.
10. Return.

So `vfunction4` is the **per-frame FX synchronization**:
- it waits for an FX job to complete using a condition variable and SRW lock,
- then gathers FX output stats (likely particle counts, timing, or other simulation metrics),
- then submits those stats to StatGraph channels for monitoring/profiling.

It does not directly update particles; it coordinates the pipeline and reports FX subsystem stats each frame (or each completed FX job cycle).

### vfunction3 — post a "FX Culling" job

Signature: `void FXHandler::vfunction3(FXHandler *this, longlong param_1)`.

Behavior:
1. Call `FUN_140d98f90(DAT_141764678, 0, param_1 + 0x78)` — likely setting up a culling job with a parameter from `param_1 + 0x78`.
2. Read 10 pointers from `param_1 + 0x18` through `param_1 + 0x60` (`lVar1`..`lVar10`).
3. Allocate a buffer via `FUN_14122ac7c(0x58)` (likely a job context / function object).
4. Write the vtable pointer and the 10 pointers into that buffer.
5. Copy the string `"FX Culling"` into a local buffer (`strncpy_s(..., 0x40, "FX Culling", 0x3f)`).
6. Call `rcss::CreateJob_CS(local_a8, local_e8, &local_128, 0)` to create a job named "FX Culling".
7. If the allocated buffer is valid, invoke `(*buffer + 0x20)(buffer, buffer != local_e8)` to execute/queue the job.
8. Also clean up the other function-object pointer (`local_f0`) via its vtable.

So `vfunction3` is the **culling job submission**:
- it packages 10 pointer arguments (likely culling parameters / regions / lists),
- creates an "FX Culling" job in the RCScript/CS job system,
- executes it immediately or posts it, depending on the buffer.

In plain language: FX culling (deciding which FX are visible/should run) is kicked off as an explicit job, with its parameters passed through a small buffer and executed under the job system, rather than inline.

---

## 5. FXCollisionSurfPlanar — planar surface collision primitive

Header (`FXCollisionSurfPlanar`):
- Inherits from `FXCollisionSurf`.
- `FXCollisionSurfPlanar_data`:
  - `offset_0x0 = 0`
  - `offset_0x0C` (lower of `field_0x4`) = `1.0f` by default (from ctor)
  - `offset_0x04` (upper of `field_0x4`) = a stored 4-byte value, default unused/zero-ish
  - effectively a plane with `d = -1` (or `d = 1`) depending on sign conventions.

### vfunction3 — test particle vs plane and return hit

Signature: `undefined8 FXCollisionSurfPlanar::vfunction3(FXCollisionSurfPlanar *this, float *param_1, float *param_2, float *param_3, undefined8 *param_4)`.

Behavior (reconstructed):
1. Pack `param_1` into `local_28` (8-byte value).
2. Compute components of the segment from `param_1` to `param_2`:
   - `local_18 = param_2[1] - param_1[1]`
   - `local_20 = param_1[2]`
   - `local_1c = param_2[0] - param_1[0]`
   - `local_14 = param_2[2] - param_1[2]`
3. Call `FUN_140dbb9c0(&this->data, &local_28, param_3)` — a plane-vs-point/segment test using the stored plane and the input point/segment.
4. If it returns nonzero:
   - write `this->data` into `*param_4` (the hit surface info),
   - write `data.offset_0x04` into `param_4[1]`,
   - return `CONCAT71((uint3)(uint)data._0x04 >> 8, 1)` — a status code derived from the stored plane value.
5. Else return `(ulonglong)extraout_var << 8` (essentially 0 with low bits cleared).

So `FXCollisionSurfPlanar::vfunction3` is a **compact planar collision test**: it tests a particle (point or small segment) against the stored plane, and if hitting, returns the plane data as the hit surface plus a status code; otherwise returns a "miss" code.

This is exactly the primitive that `FXDynamicsNewton::vfunction9` uses through its vtable call `(**(*plVar2 + 0x10))(plVar2, ...)` when `plVar2` is an `FXCollisionSurfPlanar`.

### vfunction2 — set plane from outside

Signature: `void FXCollisionSurfPlanar::vfunction2(FXCollisionSurfPlanar *this, float *param_1)`.
It calls `FUN_140dbfa40(&this->data, param_1)` — presumably sets the plane normal/offset from `param_1`. So this is the **configure** call for the collision plane.

---

---

## 7. FXProjectionAxis — what the C file actually defines (standalone summary)

If you only need the axis answer from this file, read this section first. The longer
Section 1 above is the same material with more context; this section strips the longer
walkthrough and keeps the actionable mapping.

### 7.a. The C file has no global `Y up` / `Z up` sentence

`fx_and_particles_and_anims_and_vis.c` is the asset/runtime layer. It
deserializes FX data, stores per-instance dynamics state, runs per-frame math, and
hands a 9-float result out to a callback. It never writes a sentence like
"file authors author Y-up, the renderer keeps Z-up". That sentence lives in the Go
preview layer and your tests.

In the C layer, the axis question is more concrete:

- what field of the FX description is interpreted as a direction vector,
- what the default value is when the file does not author one,
- what order the components are packed into the runtime struct,
- and what the naming scheme is for the vectors that get packed into that 9-float
  scratch buffer handed to the visualization callback.

### 7.b. Direction is a property, serialized through `FXPropT<Math::Vector3f,...>`

Relevant evidence:

- `FXPropT<Math::Vector3f, FXPropPredUnchecked>` type: [fx_and_particles_and_anims_and_vis.h:29958](fx_and_particles_and_anims_and_vis.h#L29958)
- vftable for that property type: [fx_and_particles_and_anims_and_vis.h:29966](fx_and_particles_and_anims_and_vis.h#L29966)
- getter bodies using the same pool/emit path: [fx_and_particles_and_anims_and_vis.c:6553](fx_and_particles_and_anims_and_vis.c#L6553), [fx_and_particles_and_anims_and_vis.c:6571](fx_and_particles_and_anims_and_vis.c#L6571)

The important point: direction is **a value stored in a named property that the runtime re-reads and deserializes like any other per-instance state**. It is not a per-module global or a fixed convention baked into the dynamics math itself.

### 7.c. The Go layer already chose an interpretation

That interpretation is already encoded outside this C file:

- `Emitter_Direction` is "the spray's local axis".
- missing means fallback to `+Z`.
- authored `{1,0,0}` = local +X.

Sources: [internal/fx/sim.go:159](internal/fx/sim.go#L159), [x_test_fx_frame_test.go:67](x_test_fx_frame_test.go#L67).

### 7.d. Dynamics stores position/velocity as three scalars each

For `FXDynamicsNewton`, the per-instance dynamics struct is
`FXDynamicsNewton_data`. The constructor sets:

- [fx_and_particles_and_anims_and_vis.c:6615](fx_and_particles_and_anims_and_vis.c#L6615)
- [fx_and_particles_and_anims_and_vis.c:6617](fx_and_particles_and_anims_and_vis.c#L6617)

with `0x3f800000 = 1.0f`. The default constructor for `FXDynamicsNewton` sets
`offset_0x0 = 1.0f` and `offset_0x4 = 1.0f`.

The step function `FXDynamicsNewton::vfunction8` uses:
- `this->offset_0x0` / `this->offset_0x4` as scalar parameters that scale
  velocity and drive the per-frame integration,
- `this->offset_0x8`, `this->offset_0xc` together as a second scalar pair used
  with `1 - offset_0xc` in collision resolution,
- `offset_0x10 / offset_0x18 / offset_0x1c` / `offset_0x20 / offset_0x24` as the
  three position and three velocity components.

That layout was previously reconstructed as:

| Offset | reconstructed name | meaning |
|---|---|---|
| `0x00` | `scale` | scalar accel/multiplier param used by driver |
| `0x04` | `drag` | velocity damping coefficient |
| `0x08` | `restitution` | bounce / restitution constant |
| `0x0c` | `velScale` | restitution-related weight used with `+0x08` |
| `0x10` | `posX` | position X |
| `0x14` | `posY` | position Y |
| `0x18` | `posZ` | position Z |
| `0x1c` | `velX` | velocity X |
| `0x20` | `velY` | velocity Y |
| `0x24` | `velZ` | velocity Z |
| `0x28` | `colliderPtr` | collision surface pointer (`FXCollisionSurfPlanar*`) |
| `0x30` | `colliderCached` | cached collision-field pointer |

Evidence for that layout is spread across:
- [fx_and_particles_and_anims_and_vis.c:6596-6606](fx_and_particles_and_anims_and_vis.c#L6596)
- [fx_and_particles_and_anims_and_vis.c:6656](fx_and_particles_and_anims_and_vis.c#L6656)
- [fx_and_particles_and_anims_and_vis.c:6750](fx_and_particles_and_anims_and_vis.c#L6750)
- [fx_and_particles_and_anims_and_vis.c:6881](fx_and_particles_and_anims_and_vis.c#L6881)
- [fx_and_particles_and_anims_and_vis.c:6945](fx_and_particles_and_anims_and_vis.c#L6945)

So where the dynamics side talks about "axis" at all, it is as three separate
scalar velocity components. There is no dedicated "this FX is oriented around the
Z axis" bit in `FXDynamicsNewton_data`.

### 7.e. The 9-float buffer is packed XYZ

The per-step callbacks pass a `local_48[9]` / `local_38[9]` scratch buffer into
`FUN_140d826a0(local_XX, &velocity)`, and that same buffer is then handed to the
renderer callback `(*param_1 + 8)(param_1, local_XX)`.

The buffer is used in:

- [fx_and_particles_and_anims_and_vis.c:6945](fx_and_particles_and_anims_and_vis.c#L6945)
  — `FXDynamicsNewton`
- [fx_and_particles_and_anims_and_vis.c:7112](fx_and_particles_and_anims_and_vis.c#L7112)
  — `FXDynamicsPlanar`
- [fx_and_particles_and_anims_and_vis.c:8029](fx_and_particles_and_anims_and_vis.c#L8029)
  — `FXWeatherDynamics`

From the body of the dynamics step functions and the way positions/velocities are
written into it, the buffer is packed in XYZ order:
- index `0` → X component of first vector
- index `1` → Y component of first vector
- index `2` → Z component of first vector
- index `3` → X component of second vector
- index `4` → Y component of second vector
- index `5` → Z component of second vector
- ...

The exact semantic of each of the three vectors is the part that belongs to the
review of the render/overlay layer and the tests, not to this C summary.

### 7.f. Driver function is `FUN_140d95ef0`

Every dynamics step calls `FUN_140d95ef0(...)` with combinations of:
- a packed velocity component,
- a scalar parameter (often `offset_0x8` or a `field_*` component or
  `param_2`),
- and `param_2` (frame dt).

Calls:
- [fx_and_particles_and_anims_and_vis.c:6919](fx_and_particles_and_anims_and_vis.c#L6919)
- [fx_and_particles_and_anims_and_vis.c:7325](fx_and_particles_and_anims_and_vis.c#L7325)
- [fx_and_particles_and_anims_and_vis.c:7551](fx_and_particles_and_anims_and_vis.c#L7551)
- [fx_and_particles_and_anims_and_vis.c:7994](fx_and_particles_and_anims_and_vis.c#L7994)

This is the "driver" that injects noise/randomness or applies the per-instance
direction-influencing math. Its exact formula is opaque in this file, but it is
still the place where a direction scalar/axis can affect the velocity update.

### 7.g. `FXHandler` is the pipeline side, not the axis side

`FXHandler` is the dispatch side that submits the per-frame FX job and waits on
completion. That is:
- [fx_and_particles_and_anims_and_vis.c:23995](fx_and_particles_and_anims_and_vis.c#L23995)
- [fx_and_particles_and_anims_and_vis.c:23911](fx_and_particles_and_anim_and_vis.h:23911)![fx_and_particles_and_anims_and_vis.c:23911](fx_and_particles_and_anims_and_vis.c#L23911)
- [fx_and_particles_and_anims_and_vis.h:142547](fx_and_particles_and_anims_and_vis.h#L142547)
- [fx_and_particles_and_anims_and_vis.h:142584](fx_and_particles_and_anims_and_vis.h#L142584)

The signature shows it is given a per-call parameter block `param_1`, and inside it
reads a bunch of `param_1 + 0x18`, `param_1 + 0x20`, ... `param_1 + 0x78`
offsets. That block holds the per-frame job input. The naming for the fields of
that block is not written out in this file.

For the purposes of "which way will the FX face", these functions do not define any
convention on their own. They just move the already-computed per-particle state
into the job pipeline.

### 7.h. Practical conclusion for Go

The C file gives you three concrete facts:

1. Direction is a per-instance `Math::Vector3f` property, and the property name already used in the Go layer is `Emitter_Direction`.
2. Dynamics stores position/velocity as three scalars each and packs them into a 9-float buffer in XYZ order.
3. The driver `FUN_140d95ef0` is where direction-influencing math can enter the velocity update.

So the practical decision for the Go layer is not "does the C file say Y-up?".
It is:

- which vector in the 9-float buffer is the emitter's **forward/axis** vector,
- whether that vector is already transformed into a renderer-space convention, and
- whether `Emitter_Direction` is interpreted as local-emitter-space or
  world-space in the Go layer.

The C file does not answer that last part for you; it only says direction is a
property and velocity is three scalars.

## 7. Dictionary for Ghidra (extended)

### FXDynamicsPlanar_data fields

| Old name | New name | Explanation |
|-----------|-----------|-----------|
| `offset_0x0` | `active` | plane active flag |
| `field_0x4` / `field_0x8` / `field_0xC` | `planeNormal` / `planePoint` / `planeVec` | plane definition components |
| `offset_0x1C` | `velOrPosComp` | velocity/position component used in calculations |
| `offset_0x20` / `offset_0x20_next` | `planeBaseVec` | base plane vector in dot products |
| `offset_0x28` | `planeD` | plane offset / d |

### FXDynamicsGravwell_data fields

| Old name | New name | Explanation |
|-----------|-----------|-----------|
| `offset_0x0` | `wellCenter` | gravity well center position |
| `offset_0x0_next` (float after 0x0) | `velCompX` / `wellVelX` | velocity-like component derived from well |
| `offset_0x8` | `wellVelY` / `velCompZ` | another velocity component |
| `field_0xC` | `wellVelOrState` | local state updated by well pull |

### FXWeatherDynamics_data fields

| Old name | New name | Explanation |
|-----------|-----------|-----------|
| `offset_0x0` | `intensity` | weather intensity / active threshold |
| `offset_0x04` | `scale` | weather scale/multiplier |
| `offset_0x0C` / `offset_0x0C_next` | `baseWindX` / `baseWindXLow` | base wind vector component (lower) |
| `offset_0x14` / `offset_0x14_next` | `baseWindY` / `baseWindYLow` | base wind vector component (upper) |
| `offset_0x1C` / `offset_0x1C_next` | `windVelX` / `windAccX` | accumulated wind velocity X |
| `offset_0x24` / `offset_0x24_next` | `windVelY` / `windAccY` | another wind velocity component |
| `offset_0x2C` / `offset_0x2C_next` | `windSettledX` / `windSettledXLow` | settled wind direction |

### FXHandler_data fields

| Old name | New name | Explanation |
|-----------|-----------|-----------|
| `offset_0x00..0x30` (7 handles) | `statHandle0..statHandle6` | StatGraph channels for FX stats |
| `field_0x40` | `lock` | SRWLOCK for job completion signaling |
| `field_0x38` | `completionCond` | CONDITION_VARIABLE waited on in `vfunction4` |
| `offset_0x48` | `completionFlag` | signaled when an FX job finishes |

### Local variables

#### FXDynamicsPlanar::vfunction3
| Old | New | Explanation |
|-----|-----|-----------|
| `fVar1` | `planeComp1` | first plane component |
| `fVar2` | `planeComp2` | second plane component |
| `fVar3` | `planeComp3` | third plane component |
| `fVar4` | `signedDist` | signed distance to plane (`-(n·p + d)`) |

#### FXDynamicsGravwell::vfunction8
| Old | New | Explanation |
|-----|-----|-----------|
| `fVar9` | `frameScale` | `min(param_2, 0.125)` |
| `fVar6` | `pullStrength` | `wellStrength * frameScale` |
| `fVar10`/`fVar11`/`fVar12` | `dirX`/`dirY`/`dirZ` | direction to well center |
| `fVar8` | `dirLenSq` | squared direction length |
| `fVar1` / `fVar2` | `existingVelX/Y` | existing velocity components |

#### FXWeatherDynamics::vfunction8
| Old | New | Explanation |
|-----|-----|-----------|
| `fVar19` (distance) | `distSq` | squared distance to weather base point |
| `fVar19` (inverse sqrt) | `invLen` | 1/|diff| via fast inverse sqrt |
| `fVar22`/`fVar23`/`local_e0` | `diffX`/`diffY`/`diffZ` | diff to weather base |
| `fVar23` (table lerp) | `lerpT` | interpolation factor between table entries |
| `fVar20`/`fVar22`/`fVar19` (final) | `windSampleX/Y/Z` | interpolated weather wind sample |
| `local_d8`/`uStack_d0` | `weatherParamX/Y` | external weather parameters |
| `pfVar12` | `weatherTable` | weather table pointer |
| `lVar13` | `tableEntryCount` | number of weather table entries |

#### FXHandler::vfunction4
| Old | New | Explanation |
|-----|-----|-----------|
| `cVar3` (wait loop) | `completionSignaled` | whether completion flag is set |
| `local_b8` | `statPrimary` | primary FX stat value submitted |
| `local_98`/`uStack_90`/`local_88`/`local_78`/`local_68`/`local_58` | `statN1..statN6` | additional FX stat channels |

#### FXHandler::vfunction3
| Old | New | Explanation |
|-----|-----|-----------|
| `lVar1`..`lVar10` | `cullParam1`..`cullParam10` | culling job parameters from `param_1 + 0x18..0x60` |
| `local_a8` | `jobName` | job name buffer ("FX Culling") |

#### FXCollisionSurfPlanar::vfunction3
| Old | New | Explanation |
|-----|-----|-----------|
| `local_28` | `pointPacked` | input point packed to 8 bytes |
| `local_18` | `segY` | segment Y component |
| `local_1c` | `segX` | segment X component |
| `local_14` | `segZ` | segment Z component |
| `local_20` | `pointZ` | input point Z |

---

## 7. What was added

This file (`fx_physics_extra_explained_en.md`) extends the previous English analysis with:
1. `FXDynamicsPlanar` — fields, `vfunction3`, `vfunction5`, `vfunction8`, `vfunction9`, reconstructed behavior,
2. `FXDynamicsGravwell` — fields and `vfunction8` (point attractor with `min(delta, 0.125)` and normalized pull),
3. `FXWeatherDynamics` — fields and `vfunction8` (intensity-gated wind with random scatter, table interpolation, jitter, integration, clamps, forwarding),
4. `FXHandler::vfunction4` — per-frame FX completion wait and StatGraph stats submission,
5. `FXHandler::vfunction3` — "FX Culling" job creation and execution,
6. `FXCollisionSurfPlanar` — plane collision primitive (`vfunction3` hit test, `vfunction2` configure),
7. an extended Ghidra rename dictionary for the new locals and fields,
8. `FXProjectionAxis` — a standalone C-side axis summary (Sections 1.c/1.e + Section 7 above).

It is meant to be read alongside `fx_physics_explained_en.md`; together they cover the FX Dynamics family and the FXHandler job/sync layer in the Essence Engine FX subsystem.
