# FX projection axis — what the C file actually defines

This is the C-side answer to "как определять оси координат отображения FX", before touching `go`.

## 1. The file itself does not use `Y up` or `Z up` as a sentence

`fx_and_particles_and_anims_and_vis.c` is the asset/runtime layer — the part that
deserializes FX data, stores per-instance dynamics state, runs per-frame math, and
hands a 9-float result out to a callback. It never writes a sentence like
"file authors author Y-up, the renderer keeps Z-up". That sentence lives in the Go
preview layer and your tests. In the C layer the question is more concrete:

- what field of the FX description is interpreted as a direction vector,
- what the default value is when the file does not author one,
- what order the components are packed into the runtime struct,
- and what the naming scheme is for the vectors that get packed into that 9-float
  scratch buffer handed to the visualization callback.

## 2. Direction is modeled as a runtime vector property, not a global axis constant

The closest C concept to "FX is authored along some axis" is the animated vector
property infrastructure, and the property name that already exists in the Go layer
is `Emitter_Direction`.

Relevant evidence:

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

## 3. The semantics creator already inferred `Emitter_Direction` as "the spray's
local axis, default +Z when a file does not declare it"

That semantics interpretation is already encoded in the Go layer as:

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

## 4. Where the dynamics side makes the "projectile spread axis" visible

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

## 5. The 9-float "orientation" buffer handed to the renderer uses the same XYZ
packing order

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

So the naming question for your Go layer is not "is the FX file Y-up or Z-up?" but
"what is this 9-float buffer, and in what order are the vectors packed into it".

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

## 6. The convolution/noise driver function is `FUN_140d95ef0`, passed velocity
and a scalar parameter

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

## 7. `FXHandler::vfunction3/vfunction4` — the pipeline dispatch side

`FXHandler` is the dispatch side that submits the per-frame FX job and waits on
completion. That is:
- [fx_and_particles_and_anims_and_vis.c:23995](fx_and_particles_and_anims_and_vis.c#L23995)
- [fx_and_particles_and_anims_and_vis.c:23911](fx_and_particles_and_anims_and_vis.c#L23911)
- [fx_and_particles_and_anims_and_vis.h:142547](fx_and_particles_and_anims_and_vis.h#L142547)
- [fx_and_particles_and_anims_and_vis.h:142584](fx_and_particles_and_anims_and_vis.h#L142584)

The signature shows it is given a per-call parameter block `param_1`, and inside it
reads a bunch of `param_1 + 0x18`, `param_1 + 0x20`, ... `param_1 + 0x78`
offsets. That block holds the per-frame job input. The naming for the fields of
that block is not written out in this file.

For the purposes of "which way will the FX face", these functions do not define any
convention on their own. They just move the already-computed per-particle state
into the job pipeline.

## 8. What this means for the Go layer

The C file gives you three concrete facts:

1. The semantics creator already settled on:
   - `Emitter_Direction` is "the spray's local axis".
   - missing means fallback to `+Z`.
   - authored `{1,0,0}` = local +X.
   This is the interpretation you would have to defend, not something the C file
   writes out as a label.
2. The runtime stores direction as a `Math::Vector3f` property serialized through
   `FXPropT`, and the property name already used in the Go layer is
   `Emitter_Direction`.
3. The dynamics math stores position and velocity as three scalars each
   (`posX/Y/Z`, `velX/Y/Z`) and packs them into a 9-float buffer in XYZ order
   before handing them off.

So the practical decision for the Go layer is not "does the C file say Y-up?"
It is:

- which vector in the 9-float buffer is the emitter's **forward/axis** vector,
- whether that vector is already transformed into a renderer-space convention, and
- whether `Emitter_Direction` is interpreted as local-emitter-space or
  world-space in the Go layer.

The C file does not answer that last part for you; it only says direction is a
property and velocity is three scalars.
