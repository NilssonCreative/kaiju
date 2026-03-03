/******************************************************************************/
/* rigid_body_data_binding.go                                                 */
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

package engine_entity_data_physics

import (
	"kaijuengine.com/engine"
	"kaijuengine.com/engine/encoding/pod"
	"kaijuengine.com/engine/physics"
	"kaijuengine.com/matrix"
)

var bindingKey = ""

type Shape int

const (
	ShapeBox Shape = iota
	ShapeSphere
	ShapeCapsule
	ShapeCylinder
	ShapeCone
)

func init() {
	engine.RegisterEntityData(RigidBodyEntityData{})
}

func BindingKey() string {
	if bindingKey == "" {
		bindingKey = pod.QualifiedNameForLayout(RigidBodyEntityData{})
	}
	return bindingKey
}

type RigidBodyEntityData struct {
	Extent   matrix.Vec3 `default:"1,1,1"`
	Mass     float32     `default:"1"`
	Radius   float32     `default:"1"`
	Height   float32     `default:"1"`
	Shape    Shape
	IsStatic bool
}

func (r RigidBodyEntityData) Init(e *engine.Entity, host *engine.Host) {
	host.StartPhysics()
	t := &e.Transform
	scale := t.Scale()
	mass := r.Mass
	if r.IsStatic {
		mass = 0
	}
	var shape physics.ShapeConfig
	switch r.Shape {
	case ShapeBox:
		shape = physics.BoxShape(r.Extent.Multiply(scale))
	case ShapeSphere:
		rad := r.Radius * float32(scale.LongestAxis())
		shape = physics.SphereShape(rad)
	case ShapeCapsule:
		rad := r.Radius * float32(scale.LongestAxis())
		height := r.Height * scale.Y()
		shape = physics.CapsuleShape(rad, height)
	case ShapeCylinder:
		shape = physics.CylinderShape(r.Extent.Multiply(scale))
	case ShapeCone:
		rad := r.Radius * float32(scale.LongestAxis())
		height := r.Height * scale.Y()
		shape = physics.ConeShape(rad, height)
	}
	body := physics.NewRigidBody(mass, 0.5, shape,
		t.Position(), matrix.QuaternionFromEuler(t.Rotation()))
	host.Physics().AddEntity(e, body)
}
