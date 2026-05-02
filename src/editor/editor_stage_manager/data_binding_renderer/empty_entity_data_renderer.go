/******************************************************************************/
/* empty_entity_data_renderer.go                                              */
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

package data_binding_renderer

import (
	"kaijuengine.com/editor/codegen/entity_data_binding"
	"kaijuengine.com/editor/editor_stage_manager"
	"kaijuengine.com/engine"
	"kaijuengine.com/engine_entity_data/engine_entity_data_empty"
	"kaijuengine.com/platform/profiler/tracing"
	"kaijuengine.com/rendering"
)

func init() {
	AddRenderer(engine_entity_data_empty.BindingKey(), &EmptyEntityDataRenderer{
		Icons: make(map[*editor_stage_manager.StageEntity]rendering.DrawInstance),
	})
}

// EmptyEntityDataRenderer displays a billboard gizmo icon in the editor
// viewport for entities that carry EmptyEntityData (i.e. Blender "Empty"
// objects imported from GLTF/GLB files).
type EmptyEntityDataRenderer struct {
	Icons map[*editor_stage_manager.StageEntity]rendering.DrawInstance
}

func (c *EmptyEntityDataRenderer) Attached(host *engine.Host, manager *editor_stage_manager.StageManager, target *editor_stage_manager.StageEntity, data *entity_data_binding.EntityDataEntry) {
	defer tracing.NewRegion("EmptyEntityDataRenderer.Attached").End()
	icon := commonAttached(host, manager, target, "empty.png")
	c.Icons[target] = icon
	target.OnDestroy.Add(func() {
		c.Detatched(host, manager, target, data)
	})
}

func (c *EmptyEntityDataRenderer) Detatched(_ *engine.Host, _ *editor_stage_manager.StageManager, target *editor_stage_manager.StageEntity, _ *entity_data_binding.EntityDataEntry) {
	defer tracing.NewRegion("EmptyEntityDataRenderer.Detatched").End()
	if icon, ok := c.Icons[target]; ok {
		icon.Destroy()
		delete(c.Icons, target)
	}
}

func (c *EmptyEntityDataRenderer) Show(_ *engine.Host, target *editor_stage_manager.StageEntity, _ *entity_data_binding.EntityDataEntry) {
	defer tracing.NewRegion("EmptyEntityDataRenderer.Show").End()
	if icon, ok := c.Icons[target]; ok {
		icon.Activate()
	}
}

func (c *EmptyEntityDataRenderer) Hide(_ *engine.Host, target *editor_stage_manager.StageEntity, _ *entity_data_binding.EntityDataEntry) {
	defer tracing.NewRegion("EmptyEntityDataRenderer.Hide").End()
	if icon, ok := c.Icons[target]; ok {
		icon.Deactivate()
	}
}

func (c *EmptyEntityDataRenderer) Update(_ *engine.Host, _ *editor_stage_manager.StageEntity, _ *entity_data_binding.EntityDataEntry) {
}
