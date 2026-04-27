/******************************************************************************/
/* load_result.go                                                             */
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

// Package load_result defines the data structures returned by asset loaders
// (glTF, OBJ, etc.) after parsing a model file.
//
// The central type is [Result], which contains:
//   - [Node] slice – the full node hierarchy with transforms and flags
//   - [Mesh] slice – drawable mesh data associated with nodes
//   - [Animation] slice – skeletal/morph animation data
//   - [Joint] slice – skin inverse-bind matrices
//
// Blender Empties are represented as [Node] entries where [Node.IsEmpty] is
// true. They carry a full TRS transform and can be looked up by name with
// [Result.NodeByName] or enumerated with [Result.Empties].
package load_result

import (
	"fmt"
	"log/slog"

	"kaijuengine.com/matrix"
	"kaijuengine.com/rendering"
)

// AnimationPathType identifies which transform component an animation channel
// drives (translation, rotation, scale, or morph weights).
type AnimationPathType = int

// AnimationInterpolation identifies the interpolation method used between
// animation key-frames (linear, step, or cubic spline).
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

// Mesh holds the raw vertex/index data for one drawable primitive together
// with the texture map and a pointer to the owning [Node].
type Mesh struct {
	Node     *Node
	Name     string
	MeshName string
	Verts    []rendering.Vertex
	Indexes  []uint32
	Textures map[string]string
}

// AnimBone stores the transform value for a single node at one key-frame.
// Data holds either a Vec3 (translation/scale) or a Quaternion (rotation).
type AnimBone struct {
	NodeIndex     int
	PathType      AnimationPathType
	Interpolation AnimationInterpolation
	// Could be Vec3 or Quaternion, doing this because Go doesn't have a union
	Data [4]matrix.Float
}

// AnimKeyFrame groups all bone values that occur at the same point in time.
type AnimKeyFrame struct {
	Bones []AnimBone
	Time  float32
}

// Animation holds all key-frames for a named animation clip.
type Animation struct {
	Name   string
	Frames []AnimKeyFrame
}

// Node represents a single node in the scene graph parsed from a model file.
//
// Blender Empties (objects with no mesh, camera, or skin) become nodes where
// [Node.IsEmpty] is true. Their Position/Rotation/Scale hold the local-space
// transform defined in Blender, and [Node.Children] contains the indices of
// their direct child nodes in the [Result.Nodes] slice.
//
// Custom properties assigned in Blender ("Object Properties → Custom
// Properties") are available via the [Node.Attributes] map (requires the
// "Export Custom Properties" option in the glTF export dialog).
type Node struct {
	// Id is the index of this node in the source glTF node array.
	Id int32
	// Name is the object name as set in Blender.
	Name string
	// Parent is the index into Result.Nodes of the parent node, or -1 for
	// root-level nodes.
	Parent int
	// Children holds the indices into Result.Nodes of all direct children.
	Children []int32
	// Position is the local translation relative to the parent node.
	Position matrix.Vec3
	// Rotation is the local orientation relative to the parent node.
	Rotation matrix.Quaternion
	// Scale is the local scale relative to the parent node.
	Scale matrix.Vec3
	// Attributes contains any custom properties exported from Blender (via
	// "extras" in glTF).
	Attributes map[string]any
	// IsAnimated is true if this node or any of its ancestors are targeted by
	// an animation channel.
	IsAnimated bool
	// IsEmpty is true when the node has no mesh, camera, or skin – i.e. it
	// represents a Blender Empty (or any other marker/locator object).
	IsEmpty bool
}

// Joint holds the inverse-bind matrix for one skin joint.
type Joint struct {
	Id   int32
	Skin matrix.Mat4
}

// Result is the top-level output of a model loader.  It contains the full
// node hierarchy, mesh data, animations, and skin joints.
type Result struct {
	Nodes      []Node
	Meshes     []Mesh
	Animations []Animation
	Joints     []Joint
}

// IsTreeAnimated reports whether nodeIdx or any of its ancestors are targeted
// by an animation channel.
func (r *Result) IsTreeAnimated(nodeIdx int) bool {
	isAnimated := r.Nodes[nodeIdx].IsAnimated
	p := r.Nodes[nodeIdx].Parent
	for !isAnimated && p >= 0 {
		isAnimated = r.Nodes[p].IsAnimated
		p = r.Nodes[p].Parent
	}
	return isAnimated
}

// IsValid reports whether the result contains at least one renderable mesh.
func (r *Result) IsValid() bool { return len(r.Meshes) > 0 }

// Add appends a new mesh entry to the result.
func (r *Result) Add(name, meshName string, verts []rendering.Vertex, indexes []uint32, textures map[string]string, node *Node) {
	if node != nil {
		// TODO:  This breaks Sudoku, but seems like something that should be done...
		//mat := node.Transform.CalcWorldMatrix()
		//if !mat.IsIdentity() {
		//	for i := range verts {
		//		verts[i].Position = mat.TransformPoint(verts[i].Position)
		//	}
		//}
	}
	r.Meshes = append(r.Meshes, Mesh{
		Name:     name,
		MeshName: meshName,
		Verts:    verts,
		Indexes:  indexes,
		Textures: textures,
		Node:     node,
	})
}

// NodeByName returns the first node whose Name matches, or nil if not found.
func (r *Result) NodeByName(name string) *Node {
	for i := range r.Nodes {
		if r.Nodes[i].Name == name {
			return &r.Nodes[i]
		}
	}
	return nil
}

// Empties returns a slice of pointers to all nodes that are marked as empties
// (i.e. nodes with no mesh, camera, or skin – Blender Empties or locators).
// The returned pointers are stable for the lifetime of the Result.
func (r *Result) Empties() []*Node {
	var out []*Node
	for i := range r.Nodes {
		if r.Nodes[i].IsEmpty {
			out = append(out, &r.Nodes[i])
		}
	}
	return out
}

// NodeWorldTransform computes the world-space position, rotation, and scale
// for the node at nodeIdx by multiplying up the parent chain.
//
// This is useful when you want to place a Kaiju entity at the exact world
// position of an Empty that lives inside a hierarchy of other nodes.
//
//	pos, rot, scale := result.NodeWorldTransform(nodeIdx)
//	entity.Transform.SetPosition(pos)
//	entity.Transform.SetRotation(rot.ToEuler())
//	entity.Transform.SetScale(scale)
//
// NodeWorldTransform panics if nodeIdx is out of range.
func (r *Result) NodeWorldTransform(nodeIdx int) (matrix.Vec3, matrix.Quaternion, matrix.Vec3) {
	if nodeIdx < 0 || nodeIdx >= len(r.Nodes) {
		panic(fmt.Sprintf("load_result: NodeWorldTransform index %d out of range [0, %d)", nodeIdx, len(r.Nodes)))
	}
	node := &r.Nodes[nodeIdx]
	pos := node.Position
	rot := node.Rotation
	scale := node.Scale
	p := node.Parent
	for p >= 0 {
		parent := &r.Nodes[p]
		// Combine scale
		scale = matrix.Vec3{
			scale.X() * parent.Scale.X(),
			scale.Y() * parent.Scale.Y(),
			scale.Z() * parent.Scale.Z(),
		}
		// Rotate the local position by the parent orientation and add the
		// parent translation.
		pos = parent.Rotation.Rotate(matrix.Vec3{
			pos.X() * parent.Scale.X(),
			pos.Y() * parent.Scale.Y(),
			pos.Z() * parent.Scale.Z(),
		}).Add(parent.Position)
		// Combine rotations (parent * child).
		rot = parent.Rotation.Multiply(rot)
		p = parent.Parent
	}
	return pos, rot, scale
}

// Extract returns a new Result containing only the named nodes and the meshes
// that belong to them.  Animations and joints are not copied.
func (r *Result) Extract(names ...string) Result {
	if len(r.Animations) > 0 || len(r.Joints) > 0 {
		slog.Error("extracting animation entries from a mesh load result isn't yet supported")
	}
	res := Result{}
	for i := range names {
		for j := range r.Nodes {
			if r.Nodes[j].Name == names[i] {
				res.Nodes = append(res.Nodes, r.Nodes[j])
				for k := range r.Meshes {
					m := &r.Meshes[k]
					if m.Node == &r.Nodes[j] {
						res.Add(names[i], m.Name, m.Verts, m.Indexes, m.Textures, &res.Nodes[len(res.Nodes)-1])
					}
				}
			}
		}
	}
	return res
}

// ScaledRadius returns the bounding-sphere radius of the mesh after applying
// the given scale.
func (mesh *Mesh) ScaledRadius(scale matrix.Vec3) matrix.Float {
	rad := matrix.Float(0)
	// TODO:  Take scale into consideration
	for i := range mesh.Verts {
		pt := mesh.Verts[i].Position.Multiply(scale)
		rad = max(rad, pt.Length())
	}
	return rad
}
