/******************************************************************************/
/* content_database_mesh.go                                                   */
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

package content_database

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"kaijuengine.com/editor/project/project_file_system"
	"kaijuengine.com/engine/assets"
	"kaijuengine.com/engine/stages"
	"kaijuengine.com/klib"
	"kaijuengine.com/matrix"
	"kaijuengine.com/platform/profiler/tracing"
	"kaijuengine.com/rendering"
	"kaijuengine.com/rendering/loaders"
	"kaijuengine.com/rendering/loaders/kaiju_mesh"
	"kaijuengine.com/rendering/loaders/load_result"

	"github.com/KaijuEngine/uuid"
)

func init() { addCategory(Mesh{}) }

// Mesh is a [ContentCategory] represented by a file with a ".gltf" or ".glb"
// extension. This file can contain multiple meshes as well as the textures that
// are assigned to the meshes. The textures will be imported as dependencies.
type Mesh struct{}
type MeshConfig struct{}

// See the documentation for the interface [ContentCategory] to learn more about
// the following functions

func (Mesh) Path() string       { return project_file_system.ContentMeshFolder }
func (Mesh) TypeName() string   { return "Mesh" }
func (Mesh) ExtNames() []string { return []string{".gltf", ".glb", ".obj"} }

// meshImportPostProcData holds per-variant metadata needed during material
// creation in PostImportProcessing.
type meshImportPostProcData struct {
	mesh       load_result.Mesh
	isAnimated bool
	isEmpty    bool
}

// meshImportState aggregates the data that is shared across all variants
// produced by a single GLTF/OBJ import.  It is stored as the postProcessData
// value on ProcessedImport and accessed via a type assertion in
// PostImportProcessing.
type meshImportState struct {
	// variants maps each variant Name to its per-variant data.
	variants map[string]meshImportPostProcData

	// nodeVariants maps each load_result Node ID to the ImportVariant Name
	// created for that node.  Populated for both mesh nodes and empty nodes.
	// Nodes that produce no variant (camera, skin-only, etc.) have no entry.
	nodeVariants map[int32]string

	// result is the full parsed load_result.Result, needed for the hierarchy
	// walk when building the scene template.
	result load_result.Result

	// baseName is the file-name-without-extension part used when composing
	// all variant names (e.g. "my_car" for "my_car.gltf").
	baseName string

	// totalVariants is the count of all Mesh-category variants this import
	// produces (both actual mesh nodes and empty nodes).  We compare it
	// against the number of Mesh items already indexed in
	// PostImportProcessing to detect the last call.
	totalVariants int
}

func (Mesh) Import(src string, _ *project_file_system.FileSystem) (ProcessedImport, error) {
	defer tracing.NewRegion("Mesh.Import").End()
	ext := filepath.Ext(src)
	p := ProcessedImport{}
	var res load_result.Result
	switch ext {
	case ".gltf":
		fallthrough
	case ".glb":
		adb, err := assets.NewFileDatabase(filepath.Dir(src))
		if err != nil {
			return p, err
		}
		if res, err = loaders.GLTF(filepath.Base(src), adb); err != nil {
			return p, err
		}
	case ".obj":
		adb, err := assets.NewFileDatabase(filepath.Dir(src))
		if err != nil {
			return p, err
		}
		if res, err = loaders.OBJ(filepath.Base(src), adb); err != nil {
			return p, err
		}
	}
	if len(res.Meshes) == 0 && len(res.Empties()) == 0 {
		return p, NoMeshesInFileError{Path: src}
	}
	baseName := fileNameNoExt(src)
	state := &meshImportState{
		variants:     make(map[string]meshImportPostProcData),
		nodeVariants: make(map[int32]string),
		result:       res,
		baseName:     baseName,
	}
	kms := kaiju_mesh.LoadedResultToKaijuMesh(res)
	for i := range kms {
		kd, err := kms[i].Serialize()
		if err != nil {
			return p, err
		}
		parts := strings.Split(kms[i].Name, "/")
		v := ImportVariant{
			Name: parts[len(parts)-1],
			Data: kd,
		}
		p.Variants = append(p.Variants, v)
		state.variants[v.Name] = meshImportPostProcData{res.Meshes[i], res.IsTreeAnimated(int(res.Meshes[i].Node.Id)), false}
		// Map node ID → variant name so the template builder can locate each
		// mesh's content item from the hierarchy.
		if res.Meshes[i].Node != nil {
			state.nodeVariants[res.Meshes[i].Node.Id] = v.Name
		}
	}
	// Import empty nodes (Blender Empties / locator objects) as additional
	// mesh-category variants.  They carry no geometry but a full TRS
	// transform that can be used as attach-points in the stage.
	kes := kaiju_mesh.EmptiesFromResult(res)
	emptyNodes := res.Empties()
	for i := range kes {
		kd, err := kes[i].Serialize()
		if err != nil {
			return p, err
		}
		v := ImportVariant{
			Name: kes[i].Name,
			Data: kd,
		}
		p.Variants = append(p.Variants, v)
		state.variants[v.Name] = meshImportPostProcData{mesh: load_result.Mesh{}, isAnimated: false, isEmpty: true}
		// Map empty node ID → variant name.
		if i < len(emptyNodes) {
			state.nodeVariants[emptyNodes[i].Id] = v.Name
		}
	}
	state.totalVariants = len(state.variants)
	p.postProcessData = state
	for i := range res.Meshes {
		t := res.Meshes[i].Textures
		p.Dependencies = slices.Grow(p.Dependencies, len(p.Dependencies)+len(t))
		for k, v := range t {
			tp := v
			if _, err := os.Stat(tp); err != nil {
				tp = filepath.Join(filepath.Dir(src), v)
			}
			if _, err := os.Stat(tp); err != nil {
				return p, MeshInvalidTextureError{src, v, tp}
			}
			p.Dependencies = klib.AppendUnique(p.Dependencies, tp)
			t[k] = tp
		}
	}
	return p, nil
}

func (c Mesh) Reimport(id string, cache *Cache, fs *project_file_system.FileSystem) (ProcessedImport, error) {
	defer tracing.NewRegion("Mesh.Reimport").End()
	return reimportByNameMatching(c, id, cache, fs)
}

func (Mesh) PostImportProcessing(proc ProcessedImport, res *ImportResult, fs *project_file_system.FileSystem, cache *Cache, linkedId string) error {
	defer tracing.NewRegion("Mesh.PostImportProcessing").End()
	state := proc.postProcessData.(*meshImportState)
	cc, err := cache.Read(res.Id)
	if err != nil {
		return err
	}
	variant, ok := state.variants[cc.Config.Name]
	if !ok {
		slog.Error("failed to locate the mesh in the post processing data", "name", cc.Config.Name)
		return nil
	}

	// After all per-variant work is done (including the material creation below
	// for mesh variants), check whether this is the last Mesh-typed variant to
	// be indexed.  If so, and if the source file has a multi-node hierarchy,
	// generate a scene Template that reproduces that hierarchy.
	defer meshTryBuildSceneTemplate(state, res, cache, fs, linkedId)

	// Empty nodes (Blender Empties) carry no geometry or material; skip
	// material creation entirely.
	if variant.isEmpty {
		return nil
	}
	matchTexture := func(srcPath string) rendering.MaterialTextureData {
		for i := range res.Dependencies {
			cc, err := cache.Read(res.Dependencies[i].Id)
			if err != nil {
				continue
			}
			if fs.NormalizePath(srcPath) == filepath.ToSlash(cc.Config.SrcPath) {
				return rendering.MaterialTextureData{Texture: res.Dependencies[i].Id, Filter: "Linear"}
			}
		}
		return rendering.MaterialTextureData{}
	}
	var mat rendering.MaterialData
	if _, ok := variant.mesh.Textures["metallicRoughness"]; ok {
		mat = rendering.MaterialData{
			Shader:          "pbr.shader",
			RenderPass:      "opaque.renderpass",
			ShaderPipeline:  "basic.shaderpipeline",
			Textures:        make([]rendering.MaterialTextureData, 0, len(variant.mesh.Textures)),
			IsLit:           true,
			ReceivesShadows: true,
			CastsShadows:    true,
		}
		if variant.isAnimated {
			mat.Shader = "pbr_skinned.shader"
		}
		if t, ok := variant.mesh.Textures["baseColor"]; ok {
			mat.Textures = append(mat.Textures, matchTexture(t))
			delete(variant.mesh.Textures, "baseColor")
		} else {
			mat.Textures = append(mat.Textures, rendering.MaterialTextureData{
				Texture: assets.TextureSquare, Filter: "Linear"})
		}
		if t, ok := variant.mesh.Textures["normal"]; ok {
			mat.Textures = append(mat.Textures, matchTexture(t))
			delete(variant.mesh.Textures, "normal")
		} else {
			mat.Textures = append(mat.Textures, rendering.MaterialTextureData{
				Texture: assets.TextureSquare, Filter: "Linear"})
		}
		if t, ok := variant.mesh.Textures["metallicRoughness"]; ok {
			mat.Textures = append(mat.Textures, matchTexture(t))
			delete(variant.mesh.Textures, "metallicRoughness")
		} else {
			mat.Textures = append(mat.Textures, rendering.MaterialTextureData{
				Texture: assets.TextureSquare, Filter: "Linear"})
		}
		if t, ok := variant.mesh.Textures["emissive"]; ok {
			mat.Textures = append(mat.Textures, matchTexture(t))
			delete(variant.mesh.Textures, "emissive")
		} else {
			mat.Textures = append(mat.Textures, rendering.MaterialTextureData{
				Texture: assets.TextureSquare, Filter: "Linear"})
		}
		for _, t := range variant.mesh.Textures {
			mat.Textures = append(mat.Textures, matchTexture(t))
		}
	} else {
		mat = rendering.MaterialData{
			Shader:         "basic.shader",
			RenderPass:     "opaque.renderpass",
			ShaderPipeline: "basic.shaderpipeline",
			Textures:       make([]rendering.MaterialTextureData, 0, len(variant.mesh.Textures)),
		}
		if variant.isAnimated {
			mat.Shader = "basic_skinned.shader"
		}
		for _, t := range variant.mesh.Textures {
			mat.Textures = append(mat.Textures, matchTexture(t))
		}
	}
	// Determine if a matching material already exists
	options := cache.ListByType(Material{}.TypeName())
	// Searching reverse here as the latest additions are more likely to collide
	for i := len(options) - 1; i >= 0; i-- {
		d, err := fs.ReadFile(options[i].ContentPath())
		if err != nil {
			continue
		}
		var dm rendering.MaterialData
		if err = json.Unmarshal(d, &dm); err != nil {
			continue
		}
		same := mat.Shader == dm.Shader &&
			mat.RenderPass == dm.RenderPass &&
			mat.ShaderPipeline == dm.ShaderPipeline &&
			mat.PrepassMaterial == dm.PrepassMaterial &&
			mat.IsLit == dm.IsLit &&
			mat.ReceivesShadows == dm.ReceivesShadows &&
			mat.CastsShadows == dm.CastsShadows &&
			len(mat.Textures) == len(dm.Textures)
		if !same {
			continue
		}
		for j := 0; j < len(mat.Textures) && same; j++ {
			same = mat.Textures[j] == dm.Textures[j]
		}
		if same {
			return nil
		}
	}
	f, err := os.CreateTemp("", "*-kaiju-mat.material")
	if err != nil {
		return err
	}
	if err = json.NewEncoder(f).Encode(mat); err != nil {
		return err
	}
	f.Close()
	matRes, err := Import(f.Name(), fs, cache, linkedId)
	if err != nil {
		return err
	}
	res.Dependencies = append(res.Dependencies, matRes[0])
	ccMat, err := cache.Read(matRes[0].Id)
	if err != nil {
		return err
	}
	_, err = cache.Rename(ccMat.Id(), fmt.Sprintf("%s_mat", cc.Config.Name), fs)
	if !errors.Is(err, CacheContentNameEqual) {
		return err
	}
	return nil
}

// meshTryBuildSceneTemplate is deferred from PostImportProcessing.  It checks
// whether every Mesh-typed variant from this import has been indexed, and if
// so (and if the source has a multi-node hierarchy), writes a scene Template
// that reproduces the full node tree so that dragging the template into the
// stage recreates the complete hierarchy in one step.
func meshTryBuildSceneTemplate(state *meshImportState, res *ImportResult, cache *Cache, fs *project_file_system.FileSystem, linkedId string) {
	if len(state.result.Nodes) <= 1 {
		// Single-node file – no hierarchy to preserve.
		return
	}
	allLinked, err := cache.ReadLinked(res.Id)
	if err != nil {
		return
	}
	// Count Mesh-typed items already in the cache for this import group.
	meshCount := 0
	for _, lc := range allLinked {
		if lc.Config.Type == (Mesh{}).TypeName() {
			meshCount++
		}
	}
	if meshCount < state.totalVariants {
		// Not the last variant yet – template will be built on the next call.
		return
	}
	// Check whether a template for this source was already created (e.g. on
	// re-import).  Avoid creating duplicates.
	for _, lc := range allLinked {
		if lc.Config.Type == (Template{}).TypeName() {
			return
		}
	}
	if err := meshBuildSceneTemplate(state, allLinked, fs, cache, linkedId); err != nil {
		slog.Error("failed to generate GLTF scene template", "error", err)
	}
}

// meshBuildSceneTemplate builds and imports a Template content item that
// represents the full node hierarchy of the source GLTF/GLB file.
//
// Each node in the hierarchy becomes one EntityDescription:
//   - Empty nodes (Blender Empties) receive Mesh = stages.EmptyMeshId.
//   - Mesh nodes receive Mesh = content-ID of their imported KaijuMesh, plus
//     the content-ID of the material that was created during PostImportProcessing.
//   - Nodes with no variant (cameras, skin-only, etc.) receive Mesh = "".
//
// If the source has multiple root nodes they are wrapped in a synthetic root
// entity named after the file's base name.
func meshBuildSceneTemplate(state *meshImportState, allLinked []CachedContent, fs *project_file_system.FileSystem, cache *Cache, linkedId string) error {
	defer tracing.NewRegion("meshBuildSceneTemplate").End()

	// Build lookup maps from variant name → content-ID and → material ID.
	variantToContentId := make(map[string]string, len(allLinked))
	variantToMaterialId := make(map[string]string)
	for _, lc := range allLinked {
		switch lc.Config.Type {
		case Mesh{}.TypeName():
			variantToContentId[lc.Config.SrcName] = lc.Id()
		case Material{}.TypeName():
			// Materials are named "{variantName}_mat" (see PostImportProcessing).
			if base, ok := strings.CutSuffix(lc.Config.Name, "_mat"); ok {
				variantToMaterialId[base] = lc.Id()
			}
		}
	}

	// Recursive builder: converts a node and all its descendants into an
	// EntityDescription tree.
	var buildDesc func(nodeIdx int32) stages.EntityDescription
	buildDesc = func(nodeIdx int32) stages.EntityDescription {
		node := &state.result.Nodes[nodeIdx]
		desc := stages.EntityDescription{
			Id:       uuid.NewString(),
			Name:     node.Name,
			Position: node.Position,
			Rotation: node.Rotation.ToEuler(),
			Scale:    node.Scale,
		}
		if variantName, ok := state.nodeVariants[nodeIdx]; ok {
			if state.variants[variantName].isEmpty {
				desc.Mesh = stages.EmptyMeshId
			} else {
				desc.Mesh = variantToContentId[variantName]
				desc.Material = variantToMaterialId[variantName]
			}
		}
		// Nodes with no variant (cameras, skin-only, etc.) leave desc.Mesh
		// as the zero value ("").  importEntityByDescription skips mesh
		// loading when Mesh is empty, so the entity is still created in the
		// hierarchy with its correct transform.
		for _, childIdx := range node.Children {
			desc.Children = append(desc.Children, buildDesc(childIdx))
		}
		return desc
	}

	// Collect root nodes (Parent == -1).
	var roots []int32
	for i := range state.result.Nodes {
		if state.result.Nodes[i].Parent == -1 {
			roots = append(roots, int32(i))
		}
	}

	var root stages.EntityDescription
	switch len(roots) {
	case 0:
		slog.Error("meshBuildSceneTemplate: no root nodes found in hierarchy",
			"baseName", state.baseName)
		return nil
	case 1:
		root = buildDesc(roots[0])
	default:
		// Multiple root nodes: wrap them in a synthetic empty so the template
		// has exactly one root EntityDescription.
		root = stages.EntityDescription{
			Id:    uuid.NewString(),
			Name:  state.baseName,
			Scale: matrix.Vec3One(),
		}
		for _, r := range roots {
			root.Children = append(root.Children, buildDesc(r))
		}
	}

	data, err := json.Marshal(root)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "*.template")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	f.Close()
	tplRes, err := Import(f.Name(), fs, cache, linkedId)
	if err != nil || len(tplRes) != 1 {
		return err
	}
	// Name the template after the base name of the source file.
	tplCC, err := cache.Read(tplRes[0].Id)
	if err != nil {
		return err
	}
	tplCC.Config.Name = state.baseName
	return WriteConfig(tplCC.Path, tplCC.Config, fs)
}
