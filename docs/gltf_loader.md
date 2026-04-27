# glTF Loader

This page documents the glTF 2.0 loader located at
`src/rendering/loaders/gltf.go` (public API) and its supporting packages:

| Package | Purpose |
|---------|---------|
| `kaijuengine.com/rendering/loaders` | Public entry point (`loaders.GLTF`) |
| `kaijuengine.com/rendering/loaders/gltf` | Raw JSON structs that mirror the glTF schema |
| `kaijuengine.com/rendering/loaders/load_result` | Engine-facing result types returned to callers |

---

## Supported capabilities

### File formats
| Format | Extension | Notes |
|--------|-----------|-------|
| glTF binary | `.glb` | Single-file; JSON + binary data in one container |
| glTF text | `.gltf` | JSON file + external `.bin` buffer(s) |

### Scene graph
* Full node hierarchy with parent → child relationships.
* Both `Node.Parent` (int, `-1` for roots) and `Node.Children` ([]int32) are
  populated so callers can walk the tree in either direction.

### Node transforms
Per the [glTF 2.0 specification §5.25](https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html#reference-node),
a node's local transform is expressed as either:

1. A combined 4 × 4 column-major **matrix** (highest priority when present), or
2. Separate **translation** (Vec3), **rotation** (XYZW quaternion), and
   **scale** (Vec3) fields.

The loader decomposes the matrix form into TRS so all downstream code works
with the same representation.

### Meshes
* **POSITION**, **NORMAL**, **TANGENT** vertex attributes.
* **TEXCOORD_0** / **TEXCOORD_1** UV channels.
* **JOINTS_0** / **WEIGHTS_0** skinning data (byte, unsigned-short, and
  float component types).
* Morph targets (blend shapes) on the POSITION attribute.
* Index buffers: BYTE, UNSIGNED_BYTE, SHORT, UNSIGNED_SHORT, UNSIGNED_INT.

### Materials (PBR Metallic-Roughness)
The following texture slots are extracted into the `Mesh.Textures` map:

| Map key | glTF field |
|---------|-----------|
| `baseColor` | `pbrMetallicRoughness.baseColorTexture` |
| `metallicRoughness` | `pbrMetallicRoughness.metallicRoughnessTexture` |
| `normal` | `normalTexture` |
| `occlusion` | `occlusionTexture` |
| `emissive` | `emissiveTexture` |

### Skeletal animation
* Translation, rotation, and scale channels.
* Interpolation modes: **LINEAR**, **STEP**, **CUBICSPLINE**.
* Key-frames are stored in ascending time order; each frame's `Time` field
  holds the *duration* until the next frame (the last frame has `Time = 0`).

### Skinning
* First skin only (multi-skin support is a known TODO).
* Joints prefixed with `DRV_` or `CTRL_` are filtered out (Blender driver /
  control bones).

### Custom properties (extras)
Any custom properties set in Blender under
*Object Properties → Custom Properties* are exported to the glTF `extras`
object and surfaced via `Node.Attributes map[string]any` — provided the
**Export Custom Properties** checkbox is enabled in Blender's glTF exporter.

---

## Blender Empties

A Blender **Empty** is an object that has no geometry, camera, or rig — it is
a pure transform in 3D space.  Common uses include:

* Reference points (e.g. wheel-hub positions on a vehicle).
* Attach points for physics constraints.
* Hierarchy parents that group other objects.

### How Empties are exported

When you export a Blender scene to glTF, each Empty becomes a **glTF node**
that has no `mesh`, `camera`, or `skin` field.  It still carries a full TRS
transform.

### How Kaiju represents Empties

After loading, every such node has `Node.IsEmpty == true` in the
`load_result.Result`.  All other `Node` fields (`Name`, `Position`,
`Rotation`, `Scale`, `Parent`, `Children`, `Attributes`) are populated
normally.

### Querying Empties

```go
result, err := loaders.GLTF("car.glb", host.AssetDB())
if err != nil { ... }

// Get all empties in the scene
empties := result.Empties()
for _, e := range empties {
    fmt.Println(e.Name, e.Position)
}

// Look up a specific empty by name
hub := result.NodeByName("WheelHub_FL")
if hub == nil || !hub.IsEmpty {
    panic("WheelHub_FL not found or not an empty")
}
```

### Getting the world-space transform

When an Empty is nested inside other nodes you need to multiply up the parent
chain to get the actual world-space position.
`Result.NodeWorldTransform(nodeIdx int)` does this for you:

```go
hub := result.NodeByName("WheelHub_FL")
pos, rot, scale := result.NodeWorldTransform(int(hub.Id))
```

---

## Full example – wheel hub attach points

The pattern below shows how to use Empties exported from Blender as wheel hub
references on a car model, attaching mesh entities and setting up physics
anchor points.

```go
package main

import (
    "fmt"

    "kaijuengine.com/bootstrap"
    "kaijuengine.com/engine"
    "kaijuengine.com/engine/assets"
    "kaijuengine.com/matrix"
    "kaijuengine.com/rendering"
    "kaijuengine.com/rendering/loaders"
    "kaijuengine.com/rendering/loaders/load_result"
    "kaijuengine.com/registry/shader_data_registry"
    "reflect"
)

// wheelHubNames are the names of the Blender Empties used as wheel attach
// points.  Make sure your Blender objects carry exactly these names.
var wheelHubNames = []string{
    "WheelHub_FL", // front-left
    "WheelHub_FR", // front-right
    "WheelHub_RL", // rear-left
    "WheelHub_RR", // rear-right
}

type CarGame struct {
    host     *engine.Host
    carResult load_result.Result
    wheels   [4]*engine.Entity
}

func (CarGame) PluginRegistry() []reflect.Type { return []reflect.Type{} }

func (CarGame) ContentDatabase() (assets.Database, error) {
    return assets.NewFileDatabase("game_content")
}

func (g *CarGame) Launch(host *engine.Host) {
    g.host = host

    var err error
    // host.AssetDatabase() returns the assets.Database registered at startup.
    g.carResult, err = loaders.GLTF("models/car.glb", host.AssetDatabase())
    if err != nil {
        panic(fmt.Sprintf("failed to load car: %v", err))
    }

    // Attach a wheel mesh entity at every hub Empty.
    // IMPORTANT: Pass host.WorkGroup() for concurrent transform updates.
    wheelMesh := rendering.NewMeshSphere(host.MeshCache(), 0.35, 16, 16)
    mat, _ := host.MaterialCache().Material(assets.MaterialDefinitionBasic)
    tex, _ := host.TextureCache().Texture(assets.TextureSquare, rendering.TextureFilterLinear)

    for i, hubName := range wheelHubNames {
        hub := g.carResult.NodeByName(hubName)
        if hub == nil {
            panic("missing empty: " + hubName)
        }

        // Compute the world-space position accounting for any parent nodes.
        worldPos, worldRot, worldScale := g.carResult.NodeWorldTransform(int(hub.Id))

        // Create an engine entity and position it at the hub.
        // Pass host.WorkGroup() so the transform can be updated concurrently.
        wheel := engine.NewEntity(host.WorkGroup())
        wheel.Transform.SetPosition(worldPos)
        wheel.Transform.SetRotation(worldRot.ToEuler())
        wheel.Transform.SetScale(worldScale)
        g.wheels[i] = wheel

        // CRITICAL: Attach &wheel.Transform to Drawing so the drawing follows
        // the entity when its transform is updated.
        sd := shader_data_registry.Create("basic")
        draw := rendering.Drawing{
            Material:   mat.CreateInstance([]*rendering.Texture{tex}),
            Mesh:       wheelMesh,
            ShaderData: sd,
            Transform:  &wheel.Transform,
            ViewCuller: &host.Cameras.Primary,
        }
        host.Drawings.AddDrawing(draw)
        // Always clean up ShaderData in OnDestroy to prevent memory leaks.
        wheel.OnDestroy.Add(func() { sd.Destroy() })

        // You can also read any custom properties exported from Blender:
        if v, ok := hub.Attributes["wheel_radius"]; ok {
            radius := matrix.Float(v.(float64))
            _ = radius // use for physics setup
        }
    }
}

func getGame() bootstrap.GameInterface { return &CarGame{} }
```

### Blender export checklist

1. Open *File → Export → glTF 2.0*.
2. Under **Include**, enable **Custom Properties** if you use Blender custom
   properties on the Empties.
3. Under **Transform**, choose **+Y Up** (the default) — Kaiju handles the
   coordinate-system mapping automatically.
4. Export as `.glb` (single file) or `.gltf` + `.bin` (text + binary).

---

## Known limitations / TODOs

| Area | Status |
|------|--------|
| Multiple skins | First skin only; multi-skin is a TODO |
| Morph-target weights animation | Channel parsing skipped (TODO) |
| Multiple scenes | Only the default scene (`GLTF.Scene` index) is parsed |
| Vertex colors (`COLOR_0` / `COLOR_1`) | Parsed but NaN values from exporters cause them to be replaced with white |
| Matrix transform on mesh nodes | Not applied to vertex positions (marked TODO in code) |
| Camera nodes | Parsed into the node list but camera parameters are not extracted |
