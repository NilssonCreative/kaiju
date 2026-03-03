/******************************************************************************/
/* jolt_rigid_body.go                                                         */
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

package physics

import "kaijuengine.com/matrix"

// invalidBodyID matches Jolt's BodyID::cInvalidBodyID (0xFFFFFFFF) and is
// used as a sentinel before a body has been added to a World.
const invalidBodyID = ^uint32(0)

// ShapeType identifies the geometry of a collision shape.
type ShapeType int

const (
	// ShapeTypeBox is a solid axis-aligned box defined by half-extents.
	ShapeTypeBox ShapeType = iota
	// ShapeTypeSphere is a sphere defined by a radius.
	ShapeTypeSphere
	// ShapeTypeCapsule is a capsule defined by a radius and half-height.
	ShapeTypeCapsule
	// ShapeTypeCylinder is a cylinder defined by half-extents (X = radius,
	// Y = half-height).
	ShapeTypeCylinder
	// ShapeTypeCone is a cone defined by a base radius and total height.
	ShapeTypeCone
	// ShapeTypePlane is an infinite static plane defined by a normal and a
	// plane constant.
	ShapeTypePlane
)

// ShapeConfig describes the geometry of a RigidBody's collision shape.
// Only the fields relevant to the chosen Type are read by the physics
// backend.
type ShapeConfig struct {
	// Type selects which shape geometry is used.
	Type ShapeType
	// HalfExtents provides the half-extents for box and cylinder shapes.
	HalfExtents matrix.Vec3
	// Radius is the sphere, capsule, or cone base radius.
	Radius float32
	// Height is the full height for capsule, cylinder, and cone shapes.
	Height float32
	// Normal is the surface normal for a plane shape.
	Normal matrix.Vec3
	// Constant is the plane-equation constant (d in n·x = d).
	Constant float32
}

// BoxShape returns a ShapeConfig for a box with the given half-extents.
func BoxShape(halfExtents matrix.Vec3) ShapeConfig {
	return ShapeConfig{Type: ShapeTypeBox, HalfExtents: halfExtents}
}

// SphereShape returns a ShapeConfig for a sphere with the given radius.
func SphereShape(radius float32) ShapeConfig {
	return ShapeConfig{Type: ShapeTypeSphere, Radius: radius}
}

// CapsuleShape returns a ShapeConfig for a capsule with the given radius and
// total height (tip to tip).
func CapsuleShape(radius, height float32) ShapeConfig {
	return ShapeConfig{Type: ShapeTypeCapsule, Radius: radius, Height: height}
}

// CylinderShape returns a ShapeConfig for a cylinder whose cross-section
// radius is halfExtents.X() and whose total height is halfExtents.Y() * 2.
func CylinderShape(halfExtents matrix.Vec3) ShapeConfig {
	return ShapeConfig{
		Type:        ShapeTypeCylinder,
		HalfExtents: halfExtents,
		Radius:      halfExtents.X(),
		Height:      halfExtents.Y() * 2,
	}
}

// ConeShape returns a ShapeConfig for a cone with the given base radius and
// total height.
func ConeShape(radius, height float32) ShapeConfig {
	return ShapeConfig{Type: ShapeTypeCone, Radius: radius, Height: height}
}

// PlaneShape returns a ShapeConfig for an infinite static plane described by
// the equation normal·x = constant.
func PlaneShape(normal matrix.Vec3, constant float32) ShapeConfig {
	return ShapeConfig{
		Type:     ShapeTypePlane,
		Normal:   normal,
		Constant: constant,
	}
}

// RigidBody represents a physics body in a Jolt World.  It holds only
// plain-data fields (no C pointers) so it is cheap to copy and GC-friendly.
// A body is unregistered (id == invalidBodyID) until World.AddRigidBody is
// called.
type RigidBody struct {
	id       uint32
	mass     float32
	friction float32
	shape    ShapeConfig
	position matrix.Vec3
	rotation matrix.Quaternion
}

// NewRigidBody constructs a RigidBody that will use the given shape, mass,
// and friction coefficient.  position and rotation describe the initial
// world-space transform applied when the body is added to a World.
// Pass mass = 0 for a static (immovable) body.
func NewRigidBody(mass, friction float32, shape ShapeConfig,
	position matrix.Vec3, rotation matrix.Quaternion) *RigidBody {
	return &RigidBody{
		id:       invalidBodyID,
		mass:     mass,
		friction: friction,
		shape:    shape,
		position: position,
		rotation: rotation,
	}
}

// ID returns the Jolt BodyID assigned after World.AddRigidBody, or
// invalidBodyID if the body has not yet been added.
func (r *RigidBody) ID() uint32 { return r.id }

// IsStatic reports whether the body is static (mass == 0).
func (r *RigidBody) IsStatic() bool { return r.mass == 0 }
