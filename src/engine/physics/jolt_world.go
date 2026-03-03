/******************************************************************************/
/* jolt_world.go                                                              */
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

/*
#cgo CXXFLAGS: -std=c++17
#cgo windows,amd64 LDFLAGS: -L../../libs -lJolt_win_amd64 -lstdc++ -lm
#cgo linux,amd64 LDFLAGS: -L../../libs -lJolt_nix_amd64 -lstdc++ -lm
#cgo darwin,arm64 LDFLAGS: -L../../libs -lJolt_darwin_arm64 -lstdc++ -lm
#cgo darwin,amd64 LDFLAGS: -L../../libs -lJolt_darwin_amd64 -lstdc++ -lm
#include "jolt_wrapper.h"
#cgo noescape jolt_create_system
#cgo nocallback jolt_create_system
#cgo noescape jolt_destroy_system
#cgo nocallback jolt_destroy_system
#cgo noescape jolt_set_gravity
#cgo nocallback jolt_set_gravity
#cgo noescape jolt_step
#cgo nocallback jolt_step
#cgo noescape jolt_add_body
#cgo nocallback jolt_add_body
#cgo noescape jolt_remove_body
#cgo nocallback jolt_remove_body
#cgo noescape jolt_apply_force
#cgo nocallback jolt_apply_force
#cgo noescape jolt_apply_impulse
#cgo nocallback jolt_apply_impulse
#cgo noescape jolt_get_active_transforms
#cgo nocallback jolt_get_active_transforms
#cgo noescape jolt_raycast
#cgo nocallback jolt_raycast
#cgo noescape jolt_sphere_sweep
#cgo nocallback jolt_sphere_sweep
*/
import "C"
import (
	"kaijuengine.com/matrix"
	"runtime"
	"unsafe"
)

// CollisionHit holds the result of a ray or sphere sweep cast against the
// physics world.
type CollisionHit C.jolt_HitResult

// IsValid reports whether the cast produced a hit.
func (c CollisionHit) IsValid() bool { return c.valid != 0 }

// Point returns the world-space contact point of the hit.
func (c CollisionHit) Point() matrix.Vec3 {
	return matrix.NewVec3(
		matrix.Float(c.point[0]),
		matrix.Float(c.point[1]),
		matrix.Float(c.point[2]))
}

// Normal returns the world-space surface normal at the hit point.
func (c CollisionHit) Normal() matrix.Vec3 {
	return matrix.NewVec3(
		matrix.Float(c.normal[0]),
		matrix.Float(c.normal[1]),
		matrix.Float(c.normal[2]))
}

// BodyID returns the Jolt BodyID of the hit body.
func (c CollisionHit) BodyID() uint32 { return uint32(c.body_id) }

// defaultMaxBodies is the upper bound on simultaneous bodies used when
// creating a World with NewWorld.
const defaultMaxBodies = 1024

// World wraps a Jolt PhysicsSystem and its associated allocators.
type World struct{ ptr *C.jolt_PhysicsSystem }

// NewWorld creates a physics world with sensible defaults (up to 1024 bodies,
// Earth-standard gravity not yet set — call SetGravity after construction).
func NewWorld() *World {
	w := &World{
		ptr: C.jolt_create_system(C.int(defaultMaxBodies)),
	}
	runtime.AddCleanup(w, func(ptr *C.jolt_PhysicsSystem) {
		C.jolt_destroy_system(ptr)
	}, w.ptr)
	return w
}

// SetGravity sets the world-space gravity acceleration vector.
func (w *World) SetGravity(v matrix.Vec3) {
	C.jolt_set_gravity(w.ptr,
		C.float(v.X()), C.float(v.Y()), C.float(v.Z()))
}

// StepSimulation advances the simulation by timeStep seconds.
func (w *World) StepSimulation(timeStep float32) {
	C.jolt_step(w.ptr, C.float(timeStep))
}

// AddRigidBody registers body with the physics world, creating the underlying
// Jolt body and assigning its BodyID back to body.
func (w *World) AddRigidBody(body *RigidBody) {
	s := body.shape
	id := C.jolt_add_body(w.ptr,
		C.int(s.Type),
		C.float(s.HalfExtents.X()), C.float(s.HalfExtents.Y()),
		C.float(s.HalfExtents.Z()),
		C.float(s.Radius), C.float(s.Height),
		C.float(s.Normal.X()), C.float(s.Normal.Y()),
		C.float(s.Normal.Z()), C.float(s.Constant),
		C.float(body.mass), C.float(body.friction),
		C.float(body.position.X()), C.float(body.position.Y()),
		C.float(body.position.Z()),
		C.float(body.rotation.X()), C.float(body.rotation.Y()),
		C.float(body.rotation.Z()), C.float(body.rotation.W()))
	body.id = uint32(id)
}

// RemoveRigidBody deactivates, removes, and destroys body in the physics
// world.
func (w *World) RemoveRigidBody(body *RigidBody) {
	if body.id == invalidBodyID {
		return
	}
	C.jolt_remove_body(w.ptr, C.uint32_t(body.id))
	body.id = invalidBodyID
}

// GetActiveTransforms fills ids, positions (3 floats each), and rotations
// (4 floats each, order x y z w) for all currently active dynamic bodies.
// It returns the number of bodies written, which is at most len(ids).
func (w *World) GetActiveTransforms(
	ids       []uint32,
	positions []float32,
	rotations []float32,
) int {
	if len(ids) == 0 {
		return 0
	}
	var count C.int
	C.jolt_get_active_transforms(w.ptr,
		(*C.uint32_t)(unsafe.Pointer(&ids[0])),
		(*C.float)(&positions[0]),
		(*C.float)(&rotations[0]),
		C.int(len(ids)),
		&count)
	return int(count)
}

// Raycast casts a ray from from to to and returns the closest hit, if any.
func (w *World) Raycast(from, to matrix.Vec3) CollisionHit {
	return CollisionHit(C.jolt_raycast(w.ptr,
		C.float(from.X()), C.float(from.Y()), C.float(from.Z()),
		C.float(to.X()), C.float(to.Y()), C.float(to.Z())))
}

// SphereSweep casts a sphere of the given radius from from to to and returns
// the closest hit, if any.
func (w *World) SphereSweep(from, to matrix.Vec3, radius float32) CollisionHit {
	return CollisionHit(C.jolt_sphere_sweep(w.ptr,
		C.float(from.X()), C.float(from.Y()), C.float(from.Z()),
		C.float(to.X()), C.float(to.Y()), C.float(to.Z()),
		C.float(radius)))
}

// ApplyForceAtPoint adds a world-space force to body at the given world-space
// point.
func (w *World) ApplyForceAtPoint(body *RigidBody, force, point matrix.Vec3) {
	if body.id == invalidBodyID {
		return
	}
	C.jolt_apply_force(w.ptr, C.uint32_t(body.id),
		C.float(force.X()), C.float(force.Y()), C.float(force.Z()),
		C.float(point.X()), C.float(point.Y()), C.float(point.Z()))
}

// ApplyImpulseAtPoint adds a world-space impulse to body at the given
// world-space point.
func (w *World) ApplyImpulseAtPoint(body *RigidBody, force, point matrix.Vec3) {
	if body.id == invalidBodyID {
		return
	}
	C.jolt_apply_impulse(w.ptr, C.uint32_t(body.id),
		C.float(force.X()), C.float(force.Y()), C.float(force.Z()),
		C.float(point.X()), C.float(point.Y()), C.float(point.Z()))
}
