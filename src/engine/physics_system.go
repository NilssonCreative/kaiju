/******************************************************************************/
/* physics_system.go                                                          */
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

package engine

import (
	"kaijuengine.com/engine/physics"
	"kaijuengine.com/klib"
	"kaijuengine.com/matrix"
	"kaijuengine.com/platform/concurrent"
	"kaijuengine.com/platform/profiler/tracing"
	"log/slog"
	"sync"
)

// StagePhysicsEntry pairs an engine Entity with its Jolt RigidBody.
type StagePhysicsEntry struct {
	Entity *Entity
	Body   *physics.RigidBody
}

// StagePhysics manages a Jolt physics world for a single stage.  The
// pre-allocated slices (activeIDs, positions, rotations) are grown on demand
// and reused every frame to avoid per-frame allocations.
type StagePhysics struct {
	world     *physics.World
	entities  []StagePhysicsEntry
	activeIDs []uint32
	positions []float32 // 3 floats per body
	rotations []float32 // 4 floats per body (x, y, z, w)
}

// IsActive reports whether the physics world has been started.
func (p *StagePhysics) IsActive() bool { return p.world != nil }

// World returns the underlying physics.World.
func (p *StagePhysics) World() *physics.World { return p.world }

// growTransformBuffers ensures the pre-allocated buffers hold at least n
// entries.  It reuses the underlying array when possible to avoid extra
// allocations.
func (p *StagePhysics) growTransformBuffers(n int) {
	if n <= len(p.activeIDs) {
		return
	}
	if n <= cap(p.activeIDs) {
		p.activeIDs = p.activeIDs[:n]
		p.positions = p.positions[:n*3]
		p.rotations = p.rotations[:n*4]
		return
	}
	p.activeIDs = make([]uint32, n)
	p.positions = make([]float32, n*3)
	p.rotations = make([]float32, n*4)
}

// FindCollision returns the StagePhysicsEntry whose body was hit by hit, if
// any.
func (p *StagePhysics) FindCollision(hit physics.CollisionHit) (*StagePhysicsEntry, bool) {
	defer tracing.NewRegion("StagePhysics.FindCollision").End()
	if !hit.IsValid() {
		return nil, false
	}
	id := hit.BodyID()
	for i := range p.entities {
		if p.entities[i].Body.ID() == id {
			return &p.entities[i], true
		}
	}
	return nil, false
}

// Start initialises the Jolt world with standard Earth gravity.  It is safe
// to call only once per stage lifetime.
func (p *StagePhysics) Start() {
	defer tracing.NewRegion("StagePhysics.StagePhysics").End()
	if p.world != nil {
		slog.Error("Stage physics has already started, can not start again")
		return
	}
	p.world = physics.NewWorld()
	p.world.SetGravity(matrix.NewVec3(0, -9.81, 0))
}

// Destroy removes all bodies and releases the physics world.
func (p *StagePhysics) Destroy() {
	defer tracing.NewRegion("StagePhysics.Destroy").End()
	for i := range p.entities {
		p.world.RemoveRigidBody(p.entities[i].Body)
	}
	p.entities = klib.WipeSlice(p.entities)
	p.activeIDs = p.activeIDs[:0]
	p.positions = p.positions[:0]
	p.rotations = p.rotations[:0]
	p.world = nil
}

// AddEntity adds entity and its body to the physics world and registers a
// cleanup callback that removes the body when entity is destroyed.
func (p *StagePhysics) AddEntity(entity *Entity, body *physics.RigidBody) {
	defer tracing.NewRegion("StagePhysics.AddEntity").End()
	p.entities = append(p.entities, StagePhysicsEntry{
		Entity: entity,
		Body:   body,
	})
	p.world.AddRigidBody(body)
	// Grow pre-allocated transform buffers to match the new entity count.
	p.growTransformBuffers(len(p.entities))
	entity.OnDestroy.Add(func() {
		cIdx := -1
		for i := range p.entities {
			if p.entities[i].Entity == entity {
				cIdx = i
				break
			}
		}
		if cIdx != -1 {
			p.entities = klib.RemoveUnordered(p.entities, cIdx)
			p.world.RemoveRigidBody(body)
		}
	})
}

// Update steps the simulation and applies the resulting transforms to all
// active entity components.  It makes exactly one CGO call to retrieve the
// batched active-body transforms; no CGO occurs inside the worker closures.
func (p *StagePhysics) Update(threads *concurrent.Threads, deltaTime float64) {
	defer tracing.NewRegion("StagePhysics.Update").End()
	p.world.StepSimulation(float32(deltaTime))

	n := len(p.entities)
	p.growTransformBuffers(n)
	ids := p.activeIDs[:n]
	pos := p.positions[:n*3]
	rot := p.rotations[:n*4]

	// Single CGO call: populate ids, pos, rot for every active body.
	activeCount := p.world.GetActiveTransforms(ids, pos, rot)
	if activeCount == 0 {
		return
	}

	// Build a BodyID → entity index map for O(1) lookup in the workers.
	bodyIdx := make(map[uint32]int, n)
	for i := range p.entities {
		bodyIdx[p.entities[i].Body.ID()] = i
	}

	wg := sync.WaitGroup{}
	works := make([]func(threadId int), 0, activeCount)
	for i := 0; i < activeCount; i++ {
		idx, ok := bodyIdx[ids[i]]
		if !ok {
			continue
		}
		entity := p.entities[idx].Entity
		// Capture loop-local copies so each closure has its own values.
		pBase := i * 3
		rBase := i * 4
		newPos := matrix.NewVec3(
			matrix.Float(pos[pBase]),
			matrix.Float(pos[pBase+1]),
			matrix.Float(pos[pBase+2]))
		newRot := matrix.QuaternionFromVec4(matrix.NewVec4(
			matrix.Float(rot[rBase]),
			matrix.Float(rot[rBase+1]),
			matrix.Float(rot[rBase+2]),
			matrix.Float(rot[rBase+3])))
		wg.Add(1)
		works = append(works, func(_ int) {
			entity.Transform.SetPosition(newPos)
			entity.Transform.SetRotation(newRot.ToEuler())
			wg.Done()
		})
	}
	threads.AddWork(works)
	wg.Wait()
}
