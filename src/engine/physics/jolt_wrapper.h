/******************************************************************************/
/* jolt_wrapper.h                                                             */
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

#ifndef JOLT_WRAPPER_H
#define JOLT_WRAPPER_H

#ifdef __cplusplus
extern "C" {
#endif

#include <stdint.h>

typedef struct jolt_PhysicsSystem jolt_PhysicsSystem;

/* Shape type constants matching physics.ShapeType in Go. */
#define JOLT_SHAPE_BOX      0
#define JOLT_SHAPE_SPHERE   1
#define JOLT_SHAPE_CAPSULE  2
#define JOLT_SHAPE_CYLINDER 3
#define JOLT_SHAPE_CONE     4
#define JOLT_SHAPE_PLANE    5

/* jolt_HitResult holds the result of a ray or sphere sweep cast. */
typedef struct {
	float    point[3];
	float    normal[3];
	uint32_t body_id;
	int      valid;
} jolt_HitResult;

////////////////////////////////////////////////////////////////////////////////
// Physics system lifecycle                                                    //
////////////////////////////////////////////////////////////////////////////////

/* jolt_create_system creates a Jolt PhysicsSystem with internal allocators,
   a job thread pool, and default broadphase/object-layer filters.
   max_bodies is the upper bound on simultaneous bodies; 0 uses a default. */
jolt_PhysicsSystem* jolt_create_system(int max_bodies);

/* jolt_destroy_system tears down the physics system and frees all resources. */
void jolt_destroy_system(jolt_PhysicsSystem* sys);

/* jolt_set_gravity sets the world-space gravity vector. */
void jolt_set_gravity(jolt_PhysicsSystem* sys, float x, float y, float z);

/* jolt_step advances the simulation by dt seconds. */
void jolt_step(jolt_PhysicsSystem* sys, float dt);

////////////////////////////////////////////////////////////////////////////////
// Body management                                                             //
////////////////////////////////////////////////////////////////////////////////

/* jolt_add_body creates a rigid body with the given shape, mass, friction, and
   initial transform.  Returns the Jolt BodyID (0xFFFFFFFF = invalid).
   For static bodies set mass = 0. */
uint32_t jolt_add_body(jolt_PhysicsSystem* sys,
	int   shape_type,
	float he_x,  float he_y,  float he_z,
	float radius, float height,
	float n_x,   float n_y,   float n_z,  float constant,
	float mass,  float friction,
	float px,    float py,    float pz,
	float qx,    float qy,    float qz,   float qw);

/* jolt_remove_body deactivates, removes, and destroys the body. */
void jolt_remove_body(jolt_PhysicsSystem* sys, uint32_t body_id);

/* jolt_apply_force adds a world-space force at the given world-space point. */
void jolt_apply_force(jolt_PhysicsSystem* sys, uint32_t body_id,
	float fx, float fy, float fz,
	float px, float py, float pz);

/* jolt_apply_impulse adds a world-space impulse at the given world-space
   point. */
void jolt_apply_impulse(jolt_PhysicsSystem* sys, uint32_t body_id,
	float fx, float fy, float fz,
	float px, float py, float pz);

////////////////////////////////////////////////////////////////////////////////
// Batched transform extraction                                                //
////////////////////////////////////////////////////////////////////////////////

/* jolt_get_active_transforms fills out_ids, out_positions (3 floats/body) and
   out_rotations (4 floats/body, order: x y z w) for every active dynamic body.
   At most max_capacity bodies are written; *out_count receives the actual
   number written. */
void jolt_get_active_transforms(jolt_PhysicsSystem* sys,
	uint32_t* out_ids,
	float*    out_positions,
	float*    out_rotations,
	int       max_capacity,
	int*      out_count);

////////////////////////////////////////////////////////////////////////////////
// Queries                                                                     //
////////////////////////////////////////////////////////////////////////////////

/* jolt_raycast casts a ray from (fx,fy,fz) to (tx,ty,tz). */
jolt_HitResult jolt_raycast(jolt_PhysicsSystem* sys,
	float fx, float fy, float fz,
	float tx, float ty, float tz);

/* jolt_sphere_sweep casts a sphere of the given radius from (fx,fy,fz)
   to (tx,ty,tz). */
jolt_HitResult jolt_sphere_sweep(jolt_PhysicsSystem* sys,
	float fx, float fy, float fz,
	float tx, float ty, float tz,
	float radius);

#ifdef __cplusplus
}
#endif

#endif /* JOLT_WRAPPER_H */
