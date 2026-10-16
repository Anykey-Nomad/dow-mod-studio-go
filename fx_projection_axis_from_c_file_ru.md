# Оси проекции FX — что говорит C-файл

Это C-ответ на «как определять оси координат отображения FX», до внесения изменений в Go.

## 1. В C-файле нет фразы «Y up» / «Z up»

`fx_and_particles_and_anims_and_vis.c` — это уровень ассетов и рантайма: он
десериализует FX-данные, хранит состояние динамики на каждый экземпляр, считает
шаги и передаёт 9 float'ов в callback. В этом файле нет предложения вроде
«авторы пишут в Y-up, рендерер держит Z-up». Такая фраза живет в Go-слое и в
тестах. В C-плане вопрос конкретнее:

- какое поле описания FX считается вектором направления,
- какое значение по умолчанию, если файл направление не задал,
- в каком порядке компоненты упакованы в run-time struct,
- и как называются вектора, упакованные в тот 9-float'ный scratch-буфер,
  который передается в визуализационный callback.

## 2. Направление — это свойство, а не глобальная константа

Ближайшее C-понятие к «FX нарисован вдоль какой-то оси» — это инфраструктура
анимированных векторных свойств. Имя свойства, которое уже есть в Go-слое, —
`Emitter_Direction`.

Релевантные места:

- [fx_and_particles_and_anims_and_vis.h:29958](fx_and_particles_and_anims_and_vis.h#L29958)
- [fx_and_particles_and_anims_and_vis.h:29966](fx_and_particles_and_anims_and_vis.h#L29966)
- [fx_and_particles_and_anims_and_vis.c:6553](fx_and_particles_and_anims_and_vis.c#L6553)
- [fx_and_particles_and_anims_and_vis.c:6571](fx_and_particles_and_anims_and_vis.c#L6571)

То есть:
- `FXPropT<Math::Vector3f, FXPropPredUnchecked>` — вариант шаблона свойства,
  специализированный под 3-float вектор.
- `FXPropT<...>_vftable` объявляет `vfunction1/vfunction2/vfunction3` на типе
  свойства.
- `FXPropT<>::vfunction3(FXPropT<> *this, int *param_1)` — конкретный getter,
  выдающий `int *`.

Внутри этих getter нет `new/delete`; они используют аллокатор
`FUN_140d95a30(size)` и сериализуют свойство тем же пулом, что и везде в этом
трансляции.

Главное для осей: направление — **это значение в именованном свойстве, которое
рантайм перечитывает и десериализует как любое другое состояние экземпляра**.
Это не глобальный модуль и не конвенция, вшитая в саму динамику.

## 3. Семантика уже создана в Go-слое: локальная ось spray, +Z по умолчанию

Эта интерпретация уже закодирована в Go как:

- [internal/fx/sim.go:159](internal/fx/sim.go#L159)
- [x_test_fx_frame_test.go:67](x_test_fx_frame_test.go#L67)

Конкретно:
- `the root emitter's authored direction is the spray's local axis;`
- `default to +Z only when a file does not declare it.`
- authored `{ 1, 0, 0 }` воспринимается как «локальный +X рендерера».

Это важно, потому что это первый момент, когда проект уже выбрал
интерпретацию «FX-оси»:

- authored `{ 1, 0, 0 }` = излучатель стреляет вдоль локального +X,
- отсутствующее поле → fallback `+Z` = `(0,0,1)`.

Это **модельное решение**, а не конвенция, прямо заявленная в C-файле.

## 4. Где динамика делает ось «распространения снаряда» видимой

Для `FXDynamicsNewton` структура на экземпляр — `FXDynamicsNewton_data`.
Конструктор задает:

- [fx_and_particles_and_anims_and_vis.c:6615](fx_and_particles_and_anims_and_vis.c#L6615)
- [fx_and_particles_and_anims_and_vis.c:6617](fx_and_particles_and_anims_and_vis.c#L6617)

со значением `0x3f800000 = 1.0f`. Дефолтный конструктор
`FXDynamicsNewton` ставит `offset_0x0 = 1.0f` и `offset_0x4 = 1.0f`.

Функция шага `FXDynamicsNewton::vfunction8` использует:
- `this->offset_0x0` / `this->offset_0x4` как скалярные параметры, которые
  масштабируют скорость и управляют пошаговой интеграцией,
- `this->offset_0x8`, `this->offset_0xc` вместе как вторую скалярную пару,
- используется в решении коллизий как `1 - offset_0xc`,
- `offset_0x10 / offset_0x18 / offset_0x1c` / `offset_0x20 / offset_0x24` — три
  компоненты позиции и три компоненты скорости.

Таблица была восстановлена ранее как:

| Смещение | восстановленное имя | смысл |
|---|---|---|
| `0x00` | `scale` | скалярный параметр ускорения/тяги, используемый драйвером |
| `0x04` | `drag` | коэффициент затухания скорости |
| `0x08` | `restitution` | bounce / restitution |
| `0x0c` | `velScale` | вес «остатка скорости», связанный с `+0x08` |
| `0x10` | `posX` | позиция X |
| `0x14` | `posY` | позиция Y |
| `0x18` | `posZ` | позиция Z |
| `0x1c` | `velX` | скорость X |
| `0x20` | `velY` | скорость Y |
| `0x24` | `velZ` | скорость Z |
| `0x28` | `colliderPtr` | указатель на поверхность (`FXCollisionSurfPlanar*`) |
| `0x30` | `colliderCached` | закэшированный указатель на поле коллизии |

Доказательства размазаны по:
- [fx_and_particles_and_anims_and_vis.c:6596-6606](fx_and_particles_and_anims_and_vis.c#L6596)
- [fx_and_particles_and_anims_and_vis.c:6656](fx_and_particles_and_anims_and_vis.c#L6656)
- [fx_and_particles_and_anims_and_vis.c:6750](fx_and_particles_and_anims_and_vis.c#L6750)
- [fx_and_particles_and_anims_and_vis.c:6881](fx_and_particles_and_anims_and_vis.c#L6881)
- [fx_and_particles_and_anims_and_vis.c:6945](fx_and_particles_and_anims_and_vis.c#L6945)

То есть там, где динамика вообще говорит об «оси», это три отдельных скалярных
компонента скорости. В `FXDynamicsNewton_data` нет бита
«этот FX ориентирован вокруг Z».

## 5. 9-float буфер «ориентации» упакован в том же XYZ порядке

Пошаговые callback'и передают `local_48[9]` / `local_38[9]` в
`FUN_140d826a0(local_XX, &velocity)`, и тот же буфер дальше идет в
рендерерный callback `(*param_1 + 8)(param_1, local_XX)`.

Буфер используется в:

- [fx_and_particles_and_anims_and_vis.c:6945](fx_and_particles_and_anims_and_vis.c#L6945)
  — `FXDynamicsNewton`
- [fx_and_particles_and_anims_and_vis.c:7112](fx_and_particles_and_anims_and_vis.c#L7112)
  — `FXDynamicsPlanar`
- [fx_and_particles_and_anims_and_vis.c:8029](fx_and_particles_and_anims_and_vis.c#L8029)
  — `FXWeatherDynamics`

Значит вопрос для Go-слоя — не «файл Y-up или Z-up?», а «что это за 9-float буфер и
в каком порядке в нем упакованы вектора».

Из тела функций шага и из того, как позиция/скорость записываются в него, буфер
упакован в порядке XYZ:
- индекс `0` → X первой компоненты
- индекс `1` → Y первой компоненты
- индекс `2` → Z первой компоненты
- индекс `3` → X второй компоненты
- индекс `4` → Y второй компоненты
- индекс `5` → Z второй компоненты
- ...

Точная семантика каждого из трех векторов — это уже вопрос про рендер/оверлей/тесты,
а не про этот C-резюме.

## 6. Драйвер-функция `FUN_140d95ef0`

Каждый шаг динамики вызывает `FUN_140d95ef0(...)` с комбинациями:
- упакованной компоненты скорости,
- скалярного параметра (часто `offset_0x8` или компонента `field_*` или
  `param_2`),
- и `param_2` (dt).

Вызовы:
- [fx_and_particles_and_anims_and_vis.c:6919](fx_and_particles_and_anims_and_vis.c#L6919)
- [fx_and_particles_and_anims_and_vis.c:7325](fx_and_particles_and_anims_and_vis.c#L7325)
- [fx_and_particles_and_anims_and_vis.c:7551](fx_and_particles_and_anims_and_vis.c#L7551)
- [fx_and_particles_and_anims_and_vis.c:7994](fx_and_particles_and_anims_and_vis.c#L7994)

Это «драйвер», который вносит шум/случайность или применяет
математику, зависящую от направления. Формула в этом файле не прозрачна, но это
всё равно место, где скаляр/ось могут влиять на обновление скорости.

## 7. `FXHandler::vfunction3/vfunction4` — диспетчеризация пайплайна

`FXHandler` — это сторона диспетчеризации, которая отправляет пошаговый FX-задачу
и ждет завершения. То есть:
- [fx_and_particles_and_anims_and_vis.c:23995](fx_and_particles_and_anims_and_vis.c#L23995)
- [fx_and_particles_and_anims_and_vis.c:23911](fx_and_particles_and_anims_and_vis.c#L23911)
- [fx_and_particles_and_anims_and_vis.h:142547](fx_and_particles_and_anims_and_vis.h#L142547)
- [fx_and_particles_and_anims_and_vis.h:142584](fx_and_particles_and_anims_and_vis.h#L142584)

Сигнатура показывает, что ему передается блок параметров `param_1`, и внутри
он читает `param_1 + 0x18`, `param_1 + 0x20`, ... `param_1 + 0x78`. Этот блок —
вход задачи на кадр. Имена полей этого блока в этом файле не заданы.

Для вопроса «в какую сторону будет смотреть FX» эти функции собственной конвенции
не определяют. Они просто перекладывают уже вычисленное состояние частиц в пайплайн.

## 8. Что это значит для Go

C-файл дает три конкретных факта:

1. Семантика создателя уже зафиксирована:
   - `Emitter_Direction` = «локальная ось spray».
   - отсутствие → fallback `+Z`.
   - `{1,0,0}` = локальный +X.
   Это то, что придется защищать, а не то, что C-файл пишет как лейбл.
2. Рантайм хранит направление как `Math::Vector3f` через `FXPropT`, и имя свойства в Go-слое — `Emitter_Direction`.
3. Динамика хранит позицию и скорость по три скаляра и упаковывает их в 9-float буфер в порядке XYZ.

Практическое решение для Go — не «пишет ли C-файл, что Y-up?».
Это:
- какой вектор в 9-float буфере — вектор **вперед/ось** излучателя,
- преобразован ли этот вектор в конвенцию пространства рендерера,
- трактуется ли `Emitter_Direction` как локальное пространство излучателя или как мировое в Go-слое.

Последний пункт C-файл не отвечает; он лишь говорит, что направление — свойство, а скорость — три скаляра.
