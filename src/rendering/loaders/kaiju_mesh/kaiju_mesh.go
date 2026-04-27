/******************************************************************************/
/* kaiju_mesh.go                                                              */
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

package kaiju_mesh

import (
	"bytes"
	"encoding/gob"
	"log/slog"
	"slices"
	"sync"

	"kaijuengine.com/debug"
	"kaijuengine.com/engine"
	"kaijuengine.com/engine/collision"
	"kaijuengine.com/matrix"
	"kaijuengine.com/platform/concurrent"
	"kaijuengine.com/rendering"
	"kaijuengine.com/rendering/loaders/load_result"
)

type AnimationPathType = int
type AnimationInterpolation = int

const (
	AnimPathInvalid AnimationPathType = iota - 1
	AnimPathTranslation
	AnimPathRotation
	AnimPathScale
	AnimPathWeights
)

const (
	AnimInterpolateInvalid AnimationInterpolation = iota - 1
	AnimInterpolateLinear
	AnimInterpolateStep
	AnimInterpolateCubicSpline
)

// KaijuMesh is a base primitive representing a single mesh. This is the
// archived format of the authored meshes. Typically this structure is created
// by loading in a mesh using something like [loaders.GLTF] and then converting
// the result of that using [LoadedResultToKaijuMesh]. From this point, it is
// typically serialized and stored into the content database. When reading a
// mesh from the content database, it will return a KaijuMesh.
//
// When IsEmpty is true the mesh carries no geometry. It represents a Blender
// Empty (or any other marker/locator node) and its local-space transform is
// stored in Position, Rotation, and Scale.  Verts and Indexes will be empty.
type KaijuMesh struct {
	Name       string
	Verts      []rendering.Vertex
	Indexes    []uint32
	Animations []KaijuMeshAnimation
	Joints     []KaijuMeshJoint
	// IsEmpty is true when this entry was imported from a Blender Empty
	// (a node with no mesh, camera, or skin).
	IsEmpty bool
	// Position, Rotation, Scale hold the local-space transform of the Empty
	// as exported from Blender.  These fields are zero/identity for regular
	// mesh entries.
	Position matrix.Vec3
	Rotation matrix.Quaternion
	Scale    matrix.Vec3
}

// LoadedResultToKaijuMesh will take in a [load_result.Result] and convert every
// mesh contained within the structure to our built in version known as
// [KaijuMesh]. This is typically used for the editor, but games/applications
// may find some use for it.
func LoadedResultToKaijuMesh(res load_result.Result) []KaijuMesh {
	out := make([]KaijuMesh, 0, len(res.Meshes))
	for i := range res.Meshes {
		m := &res.Meshes[i]
		km := KaijuMesh{
			Name:       m.MeshName,
			Verts:      slices.Clone(m.Verts),
			Indexes:    slices.Clone(m.Indexes),
			Animations: make([]KaijuMeshAnimation, len(res.Animations)),
			Joints:     make([]KaijuMeshJoint, len(res.Joints)),
		}
		for i := range res.Joints {
			km.Joints[i].fromLoadResult(&res, &res.Joints[i])
		}
		for i := range res.Animations {
			km.Animations[i].fromLoadResult(&res.Animations[i])
		}
		out = append(out, km)
	}
	debug.Ensure(len(out) == len(res.Meshes))
	return out
}

// EmptiesFromResult returns one [KaijuMesh] entry (with [KaijuMesh.IsEmpty]
// set to true) for each empty node in the loaded result.  Empty nodes are
// glTF nodes that carry no mesh, camera, or skin – typically Blender Empties
// used as locator / attach points (e.g. wheel hub references on a vehicle).
//
// The returned entries hold the local-space TRS transform of the empty as
// authored in Blender.  Verts, Indexes, and animation data are all empty.
func EmptiesFromResult(res load_result.Result) []KaijuMesh {
	empties := res.Empties()
	out := make([]KaijuMesh, 0, len(empties))
	for _, n := range empties {
		out = append(out, KaijuMesh{
			Name:     n.Name,
			IsEmpty:  true,
			Position: n.Position,
			Rotation: n.Rotation,
			Scale:    n.Scale,
		})
	}
	return out
}

// database or later use. This serialization uses the built-in [pod.Encoder]
func (k KaijuMesh) Serialize() ([]byte, error) {
	w := bytes.NewBuffer([]byte{})
	enc := gob.NewEncoder(w)
	err := enc.Encode(k)
	return w.Bytes(), err
}

// Deserialize will construct a [KaijuMesh] from the given array of bytes. This
// deserialization uses the built-in [pod.Decoder]
func Deserialize(data []byte) (KaijuMesh, error) {
	r := bytes.NewReader(data)
	dec := gob.NewDecoder(r)
	var km KaijuMesh
	err := dec.Decode(&km)
	return km, err
}

func ReadMesh(id string, host *engine.Host) (KaijuMesh, error) {
	data, err := host.AssetDatabase().Read(id)
	if err != nil {
		slog.Error("failed to read the mesh", "id", id, "error", err)
		return KaijuMesh{}, err
	}
	return Deserialize(data)
}

func (k KaijuMesh) GenerateBVH(threads *concurrent.Threads, transform *matrix.Transform, data any) *collision.BVH {
	tris := make([]collision.HitObject, len(k.Indexes)/3)
	group := sync.WaitGroup{}
	construct := func(from, to int) {
		for i := from; i < to; i += 3 {
			points := [3]matrix.Vec3{
				k.Verts[k.Indexes[i]].Position,
				k.Verts[k.Indexes[i+1]].Position,
				k.Verts[k.Indexes[i+2]].Position,
			}
			tris[i/3] = collision.DetailedTriangleFromPoints(points)
		}
		group.Done()
	}
	work := make([]func(int), len(tris))
	group.Add(len(work))
	for i := range work {
		work[i] = func(int) { construct(i*3, (i+1)*3) }
	}
	threads.AddWork(work)
	group.Wait()
	return collision.NewBVH(tris, transform, data)
}
