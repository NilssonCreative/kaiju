/******************************************************************************/
/* empty_gizmo.go                                                             */
/******************************************************************************/
/*                            This file is part of                            */
/*                                KAIJU ENGINE                                */
/*                          https://kaijuengine.com/                          */
/******************************************************************************/
/* MIT License                                                                */
/*                                                                            */
/* Copyright (c) 2023-present Kaiju Engine authors (AUTHORS.md).              */
/* Copyright (c) 2015-present Brent Farris.                                   */
/*                                                                            */
/* May all those that this source may reach be blessed by the LORD and find   */
/* peace and joy in life.                                                     */
/* Everyone who drinks of this water will be thirsty again; but whoever       */
/* drinks of the water that I will give him shall never thirst; John 4:13-14  */
/*                                                                            */
/* Permission is hereby granted, free of charge, to any person obtaining a    */
/* copy of this software and associated documentation files (the "Software"), */
/* to deal in the Software without restriction, including without limitation  */
/* the rights to use, copy, modify, merge, publish, distribute, sublicense,   */
/* and/or sell copies of the Software, and to permit persons to whom the      */
/* Software is furnished to do so, subject to the following conditions:       */
/*                                                                            */
/* The above copyright notice and this permission notice shall be included in */
/* all copies or substantial portions of the Software.                        */
/*                                                                            */
/* THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS    */
/* OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF                 */
/* MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT.     */
/* IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY       */
/* CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT  */
/* OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE      */
/* OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.                              */
/******************************************************************************/

package editor_stage_manager

import (
	"log/slog"

	"kaijuengine.com/engine"
	"kaijuengine.com/engine/assets"
	"kaijuengine.com/engine/collision"
	"kaijuengine.com/engine/stages"
	"kaijuengine.com/matrix"
	"kaijuengine.com/platform/profiler/tracing"
	"kaijuengine.com/registry/shader_data_registry"
	"kaijuengine.com/rendering"
)

const (
	// EmptyMeshId is re-exported from the stages package for backward
	// compatibility.  New code should reference stages.EmptyMeshId directly.
	EmptyMeshId = stages.EmptyMeshId

	emptyAxesMeshKey = "ed_empty_axes"
	emptyAxesExtent  = matrix.Float(0.5)
)

// emptyAxesColor is the orange used by Blender for its "Plain Axes" empty
// display type.
var emptyAxesColor = matrix.NewColor(0.89, 0.42, 0.04, 1)

// SpawnEmptyGizmo attaches a plain-axes wire gizmo and a BVH picking box to
// the given entity.  The gizmo consists of three line pairs drawn along the
// local X, Y and Z axes in Blender's characteristic empty orange, using the
// existing ed_transform_wire material so it renders at the same depth as
// other editor overlays.
//
// Activate / Deactivate / Destroy hooks are installed automatically so the
// gizmo follows the entity's lifecycle.
func SpawnEmptyGizmo(e *StageEntity, host *engine.Host, manager *StageManager) {
	defer tracing.NewRegion("SpawnEmptyGizmo").End()
	material, err := host.MaterialCache().Material(assets.MaterialDefinitionEdTransformWire)
	if err != nil {
		slog.Error("failed to load the empty gizmo material", "error", err)
		return
	}
	// Three line pairs: (-extent,0,0)→(+extent,0,0), Y, Z.
	points := []matrix.Vec3{
		matrix.NewVec3(-emptyAxesExtent, 0, 0), matrix.NewVec3(emptyAxesExtent, 0, 0),
		matrix.NewVec3(0, -emptyAxesExtent, 0), matrix.NewVec3(0, emptyAxesExtent, 0),
		matrix.NewVec3(0, 0, -emptyAxesExtent), matrix.NewVec3(0, 0, emptyAxesExtent),
	}
	mesh := rendering.NewMeshGrid(host.MeshCache(), emptyAxesMeshKey, points, emptyAxesColor)
	sd := shader_data_registry.Create(material.Shader.ShaderDataName())
	gsd := sd.(*shader_data_registry.ShaderDataEdTransformWire)
	gsd.Color = emptyAxesColor
	host.Drawings.AddDrawing(rendering.Drawing{
		Material:   material,
		Mesh:       mesh,
		ShaderData: gsd,
		Transform:  &e.Transform,
		ViewCuller: &host.Cameras.Primary,
	})
	// Small AABB so the empty can be picked / selected in the viewport.
	box := collision.AABB{}
	box.Extent = matrix.NewVec3(emptyAxesExtent, emptyAxesExtent, emptyAxesExtent)
	e.StageData.Bvh = collision.NewBVH([]collision.HitObject{box}, &e.Transform, e)
	manager.AddBVH(e.StageData.Bvh, &e.Transform)
	e.OnActivate.Add(gsd.Activate)
	e.OnDeactivate.Add(gsd.Deactivate)
	e.OnDestroy.Add(func() { gsd.Destroy() })
}
