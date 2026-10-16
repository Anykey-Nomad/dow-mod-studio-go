# FX Physics — Ghidra Decompiled `FXDynamicsNewton` Analysis

Source file: `fx_and_particles_and_anims_and_vis.c` (MSVC x64, Dawn of War Essence Engine FX subsystem).
Focus functions:
- `FXDynamicsNewton::FXDynamicsNewton` (copy and default ctor)
- `FXDynamicsNewton::vfunction8` (per-particle step / "Dynamics" update)
- `FXDynamicsNewton::vfunction9` (collision / resolve helper)
- `FXDynamicsNewtonLength::vfunction9` (length-clamped variant)

---

## 1. Physical model (plain language)

Это **кастомный ограниченный интегратор частиц**, а не полноценный Newton/ODE.
Основной поток (`vfunction8`) делает за фрейм:

1. **Тяга / ускорение по осям X/Y/Z.**
   Частица хранит не чистую скорость, а коэффициенты `scale` (параметр `param_2`, вероятно `deltaTime` или `speedScale`).
   Глобальные константы `_DAT_..._141715bf0/4/8` задают направление/величину базового ускорения по трём осям,
   а поля объекта добавляют персональную тягу/смещение. То есть это упрощённая "physics" с предсказуемыми сценариями
   (выхлоп, дым, искры), где движение часто детерминировано, а не рассчитывается от сил в реальном времени.

2. **Drag / затухание.**
   Второй набор констант (`0x141764730/34/38`) умножает текущую милли-velocità/скорость на `scale` и добавляет остаток —
   типичная форма экспоненциального затухания скорости (`v *= (1 - drag*dt)` или `v += base*dt`).

3. **Квадратный корень / нормализация вектора.**
   По ходу кода много `sqrtf(...)` / `SQRT(...)` и `1.0 / length`.
   Это индикатор нормализации направлений (отражений, касательных, "direction = diff / |diff|"),
   а не гравитации в стиле `F = m*g`.

4. **Вращение (ортогональная матрица 3x3 без trig).**
   В `vfunction9` (ветка `iVar1 == 1`) есть классическая формула отражения/поворота через скалярное произведение
   и удвоенную нормаль:
   ```
   newV = v - 2 * (v·n) * n
   ```
   фигурирует как `local_100 * 2.0 * fVar9 - ...`.
   Это **зеркальное отражение относительно плоскости/нормали**, либо компонент разложения движения по касательной.

5. **Удар по мешу / точка коллизии.**
   В `vfunction9` есть вызов коллизии через vtable:
   ```
   ((**)(plVar2->vtable + 0x10))(plVar2, &fromPos, &toPos, &hitInfo, &someOut)
   ```
   Если вернулась ненулевая "ударная" метка, движение пересчитывается:
   - если новая точка почти не отличается от старой (`dist^2 < eps` или `< step^2`) — частица застряла/отскочила;
   - иначе — берётся отражённая/смещённая точка.
   В коде фигурирует **коэффициент упругости/отражения** = `1.0 - this->offset_0xc` (восстановлен как restitution).
   Также есть условное разложение скорости на "оставшуюся после удара" часть (те же `2.0 * dot(n,v)`).

6. **Бязовые отсечения / клиппинг.**
   - `if (|vel|^2 <= 360000)` и `if (|newPos|^2 <= 4e6)` — квадратные дистанции-дроссели, чтобы не лететь в бесконечность.
   - В ветке "слишком быстро" есть страховка `v = v / |v| * 600.0` — нормализация и огранка скорости до 600.

7. **Синхронизация вывода.**
   После шага, если `|vel| > 0.05`, вызывается `FUN_140d826a0(local_48, &velocity)` и какая-то callback-функция через `(*param_1 + 8)`.
   Вероятно, это передача новой скорости/позиции в систему визуализации/рендера.

**Итог**: у нас типичная для игровых FX "Newtonian-ish" система: есть скорость, drag, упругое отражение от меша,
вагнеровские отсечения по дистанции/скорости и нормализация векторов. Полная физика (масса, импульс, момент, интегратор
умеренной точности) не обязательна — движение часто авторитетно задаётся автором эффекта, а движок лишь ограничивает и
отражает.

---

## 2. Поля структуры (`FXDynamicsNewton_data`) — предполагаемые типы

По восстановлению из конструктора копирования и `vfunction8`:

| Смещение | Тип (восстановлено) | Предполагаемое назначение |
|----------|---------------------|---------------------------|
| `+0x00` | `float` | `scale` / speed-factor по оси X или общий множитель ускорения |
| `+0x04` | `float` | drag-коэффициент / коэф. затухания скорости |
| `+0x08` | `float` | restitution / bounce / "bounciness" (используется как `1 - offset_0xc` в `vfunction9`) |
| `+0x0C` | `float` (`union` с `+0x0C..0x13`) | коэффициент "отражения/остатка" после удара: в `vfunction9` читается как `(1 - this->offset_0xc)` |
| `+0x10` | `float[2]`? (`vec2` или часть `vec3`) | часть позиции/скорости (X и Y?); в конструкторе копируется из `param_1+0x18` |
| `+0x18` | `float` | продолжение вектора из `+0x10` (Y или Z) |
| `+0x1C` | `float[2]` (вместе `0x1C..0x23`) | вектор шкалы/скорости (из кода — `local_68 = this->offset_0x1c`, часто читается как пара float) |
| `+0x24` | `float` | третий компонент того же вектора (`local_60`) |
| `+0x28` | `int` | режим коллизии: 0 — выключено, 1 — полноценный разбор (отражение/пересчёт), 2/3 — иные ветки возврата |
| `+0x30` | указатель (`void*` / vtable-контекст) | объект/контекст коллизии меша (`this->offset_0x30` передаётся в `vfunction9` как `plVar2`) |

> Примечание: некоторые поля перекрываются глазами декомпилятора, но наблюдаемые обращения в `vfunction8`/`vfunction9`
> указывают именно на такой набор: 3 float'a масштаба/тяги, drag-параметры, вектор скорости/квадрата (3 float'a),
> режим коллизии, и указатель на коллизионный объект.
> Константы инициализации `0x3f800000 = 1.0f`, `0x3e4ccccd ≈ 0.2` говорят о разумных дефолтах.

Для `FXDynamicsNewtonLength`:
- `+0x00` / `+0x04` (в своём `FXDynamicsNewtonLength_data`) — `vec2` ограничения (min/max по XY/Z?)
- `+0x08` — `float` ограничение Z / длина
- `+0x0C` — `float` макс. расстояние (`fVar6 <= this->offset_0xc`)

---

## 3. Рефакторинг: чистый C++ эквивалент

Ниже — восстановленный высокоуровневый вид `FXDynamicsNewton::step` и `resolveCollision` с использованием
нормальных имён. Некоторые детали (например, точный смысл `FUN_140d95ef0` и `FUN_140d826a0`) оставлены как
вызовы-шлюзы, но семантика восстановлена по контексту.

```cpp
#include <cmath>
#include <cstdint>

// Псевдодрайвер глобальных констант из базы данных Ghidra.
extern float g_fxNewtonAccelX;   // _DAT_141715bf0-ish
extern float g_fxNewtonAccelY;   // _DAT_141715bf4-ish
extern float g_fxNewtonAccelZ;   // _DAT_141715bf8-ish
extern float g_fxNewtonDragScale; // _DAT_141764730-ish
extern float g_fxNewtonDragY;     // _DAT_141764734-ish
extern float g_fxNewtonDragZ;     // _DAT_141764738-ish
extern float g_fxNewtonDefaultVal; // DAT_141764728-ish

struct FXDynamicsNewton
{
    // vtablePtr опущен — не важно для физики.
    float  scale;            // +0x00
    float  drag;             // +0x04
    float  restitution;      // +0x08  (читается как 1 - offset_0xc в vfunction9)
    union {
        float field_c[2];    // +0x0C..+0x13
        struct {
            float velScale;        // +0x0C — используется как bounce/restitution множитель
            char   _pad[4];
        };
    };
    float  posX;             // +0x10
    float  posY;             // +0x18
    float  velX;             // +0x1C (нижний float)
    float  velY;             // +0x20
    float  velZ;             // +0x24
    int    collisionMode;    // +0x28
    void  *collisionCtx;     // +0x30

    // Эквивалент дефолтного конструктора.
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

    // Эквивалент конструктора копирования из "параметров".
    FXDynamicsNewton(const FXDynamicsNewtonParams& src)
    {
        scale        = src.m1;        // param_1+0x08
        drag         = src.m2;        // param_1+0x0c
        restitution  = src.m3;        // param_1+0x10
        // +0x10.. — копируется как 8-байтовое значение
        posX         = src.v1.x;      // param_1+0x18
        posY         = src.v1.y;      // param_1+0x20
        velX         = src.v2.x;      // param_1+0x24
        velY         = src.v2.y;      // param_1+0x2c
        velZ         = src.v2.z;      // param_1+0x30
        collisionMode= src.mode;      // param_1+0x38-ish
        collisionCtx = src.collision; // param_1+0x38 as ptr
    }
};

// Обёртка параметров, которые приходят извне (смещение param_1 в vfunction8 — это
// указатель на контекст/задание, а param_2 — time scale / delta).
struct FXDynamicsNewtonStepInput
{
    void*   ctx;
    float   local_48[9]; // не используется напрямую здесь, только как буфер для передачи
};

// "Магическая" функция драйвера ускорения/шума (в Ghidra — FUN_140d95ef0).
// По контексту: принимает "сырую" скорость, нормализует и масштабирует.
extern float fxNewtonNormalizeAndCap(float raw, float ref, float scale);

// Передача результата в систему визуализации (Ghidra — FUN_140d826a0).
extern void fxNewtonWriteVelocity(float out[3], const float vel[3]);

// Возвращает true, если частица "уделала" течение кадра / ушла за пределы.
bool stepFXDynamicsNewton(
    FXDynamicsNewton& self,
    const FXDynamicsNewtonStepInput& input,
    float timeScale   // param_2 — часто это local frame time или speed scale
)
{
    constexpr float MAX_VEL_SQ = 360000.0f;   // "360000.0" в коде
    constexpr float MAX_POS_SQ = 4000000.0f;  // "4e+06" в коде

    // ---------- drag-подобный фильтр скорости ----------
    float s = self.scale;
    float dragFactor = s * g_fxNewtonDragScale; // fVar4
    float velScale   = self.velScale;           // fVar5, используется повторно

    float newVX = velScale * g_fxNewtonAccelX * timeScale + self.velX; // fVar5
    float newVY = s * g_fxNewtonAccelY * timeScale + self.velY;
    float newVZ = s * g_fxNewtonAccelZ * timeScale + self.velZ;

    // "поглощение" с past values (в коде — fVar7 += ...)
    newVX = dragFactor * timeScale + newVX; // fVar7 (перезаписан)
    newVY = s * g_fxNewtonAccelY * timeScale + newVY;
    newVZ = velScale * g_fxNewtonAccelZ * timeScale + newVZ;

    self.velX = newVX;
    self.velY = newVY;
    self.velZ = newVZ;

    // ---------- ещё одна волна drag / правки по осям ----------
    float vel2 = self.velX;  // повторное чтение self.velX
    float d2 = vel2 * g_fxNewtonDragScale; // fVar4
    float v3 = vel2 * g_fxNewtonDragX * timeScale + self.velX;
    float v4 = d2 * timeScale + self.velY;
    float v5 = vel2 * g_fxNewtonDragZ * timeScale + self.velZ;

    self.velX = v3;
    self.velY = v4;
    self.velZ = v5;

    // нормализуем направление через "Magic" (Ghidra FUN_140d95ef0).
    float mag = fxNewtonNormalizeAndCap(self.velX, self.restitution, timeScale);

    float dirX = mag * self.velX;
    float dirY = mag * self.velY;
    float dirZ = mag * self.velZ;

    self.velX = dirX;
    self.velY = dirY;
    self.velZ = dirZ;

    // ---------- отсечение по скорости ----------
    float speedSq = dirX*dirX + dirY*dirY + dirZ*dirZ;
    if (speedSq > MAX_VEL_SQ)
    {
        // страховка "замедлить до 600"
        float invLen = 1.0f / std::sqrt(speedSq);
        self.velX = invLen * dirX * 600.0f;
        self.velY = invLen * dirY * 600.0f;
        self.velZ = invLen * dirZ * 600.0f;

        // сразу кодируем "успех, но буйный"
        uint8_t status = (static_cast<uint32_t>(invLen * 600.0f) >> 8) & 0xFF;
        return (status & 1) != 0; // семантика возврата в Ghidra — код статуса
    }

    // ---------- предсказание новой позиции ----------
    float newPosX = dirX * timeScale + self.posX;
    float newPosY = dirY * timeScale + self.posY;
    float newPosZ = dirZ * timeScale + self.posZ;

    // ---------- отсечение по дистанции ----------
    float posSq = newPosX*newPosX + newPosY*newPosY + newPosZ*newPosZ;
    if (posSq > MAX_POS_SQ)
    {
        // "ушла очень далеко" — код статуса через local_60 (velZ?)
        uint32_t raw = static_cast<uint32_t>(self.velZ) >> 8;
        return (raw & 1) != 0;
    }

    // ---------- коллизионный шаг ----------
    float hitPos[3]  = { newPosX, newPosY, newPosZ };
    float hitVel[3]  = { dirX, dirY, dirZ };
    float extraOut[9];
    float restitutionFromObj = g_fxNewtonDefaultVal; // param_7-подобное

    bool collisionHit = false;
    if (self.collisionCtx != nullptr)
    {
        // vtable call: (**(*\*plVar2+0x10))(plVar2, &pos, &toPos, &hitOut, &out2)
        // Восстанавливаем как:
        //   collide(collisionCtx, fromPos, toPos, hitPos, &collisionInfo)
        collisionHit = invokeMeshCollision(
            self.collisionCtx,
            &self.posX, &hitPos[0], hitPos, &restitutionFromObj
        );
    }

    if (collisionHit)
    {
        // расстояние от "старой" до "новая(отражённая)"
        float dx = hitPos[0] - self.posX;
        float dy = hitPos[1] - self.posY;
        float dz = hitPos[2] - self.posZ;
        float dSq = dx*dx + dy*dy + dz*dz;

        if (restitutionFromObj == 0.0f)
        {
            // частица "прилипла" / осталась на месте
            hitPos[0] = self.posX;
            hitPos[1] = self.posY;
            hitPos[2] = self.posZ;
        }
        else if (dSq < 1.1920929e-07f || dSq < timeScale*timeScale)
        {
            // значит ударилась в ту же точку / застряла — кладём в точку предыдущего положения
            hitPos[0] = self.posX;
            hitPos[1] = self.posY;
            hitPos[2] = self.posZ;
        }
        else
        {
            // корректируем по шагу назад/вперёд
            hitPos[0] = hitPos[0] - dirX * timeScale;
            hitPos[1] = hitPos[1] - dirY * timeScale;
            hitPos[2] = hitPos[2] - dirZ * timeScale;
        }

        // ----------- режим 1: полное разложение отражения -----------
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
            // ... (восстанавливаем по формуле local_100 * 2.0 * fVar9 - ...)
            // Упрощённо: вектор после удара = incident - 2 * (incident·n) * n

            // Затем разложение по Kolliｓion velocity (param_5-подобное)
            float rvelLenSq = hitVel[1]*hitVel[1] + hitVel[0]*hitVel[0] + hitVel[2]*hitVel[2];
            float rvelLen = (rvelLenSq < 0.0f) ? std::sqrt(-rvelLenSq) : std::sqrt(rvelLenSq);
            float invRvel = 1.0f / rvelLen;

            float scaleFactor = invRvel * bounce * 1.0f;
            hitVel[0] = scaleFactor * hitVel[0];
            hitVel[1] = scaleFactor * hitVel[1];
            hitVel[2] = scaleFactor * hitVel[2];

            // разложение по компоненте удара...
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

            // интерполяция позиции между старой и новой по весу fVar7
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
            // режим 2 — просто возвращаем "удар по плоскости" (код возвращает 1)
            return 1;
        }
        else if (self.collisionMode == 3)
        {
            return 0; // или 1? в коде — "if iVar1 != 3 return 0"
        }
        else
        {
            // простой сброс в uVar6 (параметр позиции из входных)
            self.posX = hitPos[0];
            self.posY = hitPos[1];
            self.posZ = hitPos[2];
            hitVel[0] = 0.0f;
            hitVel[1] = 0.0f;
            hitVel[2] = 0.0f;
            return 0;
        }
    }

    // без коллизии — просто перемещаем по предсказанному направлению
    self.posX = hitPos[0];
    self.posY = hitPos[1];
    self.posZ = hitPos[2];
    hitVel[0] = dirX; // или param_5 (входная скорость)
    hitVel[1] = dirY;
    hitVel[2] = dirZ;

    // код статуса по одной из компонент (в Ghidra — param_5[2])
    uint32_t statusRaw = static_cast<uint32_t>(dirZ) >> 8;
    return (statusRaw & 0xFF) != 0; // семантика возврата "бит статуса"
}

// -----------------------------------------------------
// FXDynamicsNewton::vfunction9 — разбор коллизии/отражения
// (вынесен отдельно, так как используется многими наследниками)
// -----------------------------------------------------
struct CollisionResult
{
    float outPos[3];  // param_1 (выходная позиция)
    float outVel[3];  // param_2 (выходная скорость)
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
    // Если движение "нулевое" — быстрый выход
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
    // вызов коллизии (vtable-шлюз)
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
        return false;  // управление уходит в LAB_140d9c771 — "просто прошла"

    // расстояние от н อ рефлектированной точки до fromPos
    float hx = result.outPos[0] - fromPos[0];
    float hy = result.outPos[1] - fromPos[1];
    float hz = result.outPos[2] - fromPos[2];
    float dHitSq = hy*hy + hx*hx + hz*hz;

    const float eps = 1.1920929e-07f;
    if (dHitSq < eps || dHitSq < stepScale*stepScale)
    {
        // "застряла" — используем "безопасную" точку
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
    // режим 1: полноценное отражение
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

        // интерполяция позиции
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
        // простой выход — значение hit-вектора
        result.outPos[0] = result.outPos[0]; // uVar6
        result.outPos[2] = /*fVar4*/ result.outPos[2];
        result.outVel[0] = 0.0f;
        result.outVel[1] = 0.0f;
        result.outVel[2] = 0.0f;
        return true; // "1" как код возврата статуса коллизии
    }
    else if (self.collisionMode == 3)
    {
        return true; // код 1
    }
    else
    {
        // код 0 — "не обработано/игнорировать"
        return false;
    }
}

// -----------------------------------------------------
// FXDynamicsNewtonLength::vfunction9 — ограничение по длине
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
        // в пределах — делегируем базовый Newton
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
            float pullX = invD * ddy * maxDist; // примечание: в коде индексы перепутаны в декомпиляции
            float pullY = invD * ddx * maxDist;
            float pullZ = invD * ddz * maxDist;

            result.outPos[0] = limX + pullX;
            result.outPos[1] = limY + pullY;
            result.outPos[2] = limZ + pullZ;
        }

        result.outVel[0] = vel[0]; // или param_5
        result.outVel[2] = vel[2];
        result.outVel[1] = vel[1];
        // флаг param_8 = 1
        return true;
    }
}
```

Примечания к рефактору:
- `invokeMeshCollision` и `fxNewtonNormalizeAndCap` / `fxNewtonWriteVelocity` — это неполностью восстановленные шлюзы
  (в оригинале `FUN_140d95ef0`, `FUN_140d826a0` и вызовы через `plVar2` vtable).
  Я оставил их как внешние функции, так как их внутренности выходят за рамки выделенного фрагмента.
- Точные значения констант (`1.1920929e-07f` — `FLT_EPSILON`-подобное, `360000.0` и `4000000.0` — квадратные отсечения,
  `600.0` — крайняя нормированная скорость) взяты прямо из декомпилированного текста.

---

## 4. Словарь переименований для Ghidra

Предлагаемые имена для `FXDynamicsNewton_data` и локальных переменных `vfunction8`/`vfunction9`.

### Поля объекта (смещения в `FXDynamicsNewton_data`)

| Старое имя (Ghidra-фома) | Новое имя | Пояснение |
|---------------------------|-----------|-----------|
| `(this->FXDynamicsNewton_data).offset_0x0` / `fVar3` чтение | `scale` | множитель ускорения/тяги по оси X и общий параметр шага |
| `(this->FXDynamicsNewton_data).offset_0x4` / `fVar3` второй блок | `drag` | коэффициент затухания скорости (animate drag) |
| `(this->FXDynamicsNewton_data).offset_0x8` / `fVar4` | `restitution` | restitution/отдача при ударе (используется как `1 - offset_0xc` в `vfunction9`) |
| `(this->FXDynamicsNewton_data).offset_0xc` / `fVar10` | `velScale` | вес "остатка скорости" после удара / bounce-коэффициент (часто читается вместе с `+0x08`) |
| `(this->FXDynamicsNewton_data).offset_0x10` / `local_58` | `posX` | X-компонента позиции |
| `(this->FXDynamicsNewton_data).offset_0x18` / `local_50` | `posY` | Y-компонента позиции |
| `(this->FXDynamicsNewton_data).offset_0x1c` / `local_68` | `velX` | X-компонента скорости (часто читается попарно с `+0x20`) |
| `(this->FXDynamicsNewton_data).offset_0x20` | `velY` | Y-компонента скорости |
| `(this->FXDynamicsNewton_data).offset_0x24` / `local_60` | `velZ` | Z-компонента скорости |
| `(this->FXDynamicsNewton_data).offset_0x28` / `iVar1` | `collisionMode` | режим коллизии: 0=none, 1=full reflect/resolve, 2, 3 — другие ветки |
| `(this->FXDynamicsNewton_data).offset_0x30` / `plVar2` | `collisionCtx` | указатель на контекст/объект коллизии меша (передаётся в `vfunction9`) |

### Константы / глобальные (в Ghidra — `_DAT_...`)

| Старое имя | Новое имя | Пояснение |
|------------|-----------|-----------|
| `_DAT_141715bf0` | `g_fxNewtonAccelX` | базовое ускорение по X для данного типа частиц |
| `_DAT_141715bf4` | `g_fxNewtonAccelY` | базовое ускорение по Y |
| `_DAT_141715bf8` | `g_fxNewtonAccelZ` | базовое ускорение по Z |
| `_DAT_141764738` | `g_fxNewtonDragScale` | множитель drag по оси X / speed-сохранение |
| `_DAT_141764730` | `g_fxNewtonDragX` | drag-компонента по X в шаге (`fVar5`) |
| `_DAT_141764734` | `g_fxNewtonDragY` | drag-компонента по Y |
| `DAT_141764728` | `g_fxNewtonDefaultRestitution` | дефолтное значение restitution / начальный контекст |

### Локальные переменные `vfunction8`

| Старое имя | Новое имя | Пояснение |
|------------|-----------|-----------|
| `param_2` | `timeScale` | масштаб времени / дельта кадра или speedFactor |
| `fVar3` (первый блок) | `scale` | чтение `self.scale` |
| `fVar5` (второй блок) | `velX` | переписываемое поле скорости |
| `fVar7` (в блоке drag) | `dragAccelX` / `newVX` | промежуточное значение ускорения + drag |
| `local_78`, `local_74`, `local_70` | `newPosX`, `newPosY`, `newPosZ` | предсказанная новая позиция |
| `local_68`, `local_60`, `local_58` (второй набор) | `velX`, `velY`, `velZ` | текущая скорость для передачи в коллизию |
| `local_48[9]` | `sScratch[9]` | буффер для передачи результата в визуализацию (Ghidra FUN_140d826a0) |
| `uVar2` (статус) | `status` | код статуса частицы (гибрид bool/int) |

### Локальные переменные `vfunction9`

| Старое имя | Новое имя | Пояснение |
|------------|-----------|-----------|
| `param_1` (float*) | `outPos` | выходная позиция после коллизии |
| `param_2` (float*) | `outVel` | выходная скорость после коллизии |
| `param_3`, `param_4` | `fromPos`, `toPos` | начальная и целевая позиции шага |
| `param_5` | `inVel` | входная скорость (часто "исходная" до отражения) |
| `param_6` | `restitutionGlobal` | глобальный restitution/время-вес (используется при интерполяции `outPos`) |
| `param_7` | `stepScale` | размер шага / timeScale для корректировки отражённой точки |
| `fVar4` (первый блок) | `stepVectorX` | смещение шага по X (`toX - fromX`) |
| `fVar5`, `fVar6`, `fVar7` | `stepLenSq`, `dirX`, `dir` | квадрат длины шага → нормализованное направление |
| `local_fc`, `local_100`, `local_f8` | `normalY`, `normalX`, `normalZ` | компоненты нормали/отражённого направления при вычислении `v - 2*(v·n)*n` |
| `local_e0` | `fromPosAsVec` | входная позиция, перепакованная в `undefined8` |
| `uVar6` | `hitTargetPos` | целевая точка, возвращаемая при режиме != 1 |
| `fVar10`, `fVar11`, `fVar12` | `reflectedVelX/Y/Z` | проекция скорости после отражения |

---

## 5. Что сохранено

- Анализ и очищенный код записаны в этот же файл: `fx_physics_explained.md`.
- В файле:
  1. объяснение физической модели простым языком,
  2. таблица полей структуры по смещениям,
  3. очищенный C++ эквивалент `FXDynamicsNewton::step`, `resolveNewtonCollision` и `FXDynamicsNewtonLength`,
  4. словарь переименований для Ghidra (поля объекта, глобальные константы, локальные переменные).
