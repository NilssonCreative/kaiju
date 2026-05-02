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
	"kaijuengine.com/engine_entity_data/engine_entity_data_empty"
	"kaijuengine.com/klib"
	"kaijuengine.com/matrix"
	"kaijuengine.com/platform/profiler/tracing"
	"kaijuengine.com/rendering"
	"kaijuengine.com/rendering/loaders"
	"kaijuengine.com/rendering/loaders/kaiju_mesh"
	"kaijuengine.com/rendering/loaders/load_result"
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

type meshImportPostProcData struct {
	mesh         load_result.Mesh
	meshes       []load_result.Mesh
	nodes        []load_result.Node
	isAnimated   bool
	textureBytes map[string][]byte
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
	if len(res.Meshes) == 0 {
		return p, NoMeshesInFileError{Path: src}
	}
	kms := kaiju_mesh.LoadedResultToKaijuMesh(res)
	postProcData := map[string]meshImportPostProcData{}
	for i := range kms {
		kd, err := kms[i].Serialize()
		if err != nil {
			return p, err
		}
		v := ImportVariant{
			Name: res.Meshes[i].Name,
			Data: kd,
		}
		p.Variants = append(p.Variants, v)
		isAnimated := res.IsTreeAnimated(int(res.Meshes[i].Node.Id))
		postProcData[v.Name] = meshImportPostProcData{
			mesh:         res.Meshes[i],
			meshes:       res.Meshes,
			nodes:        res.Nodes,
			isAnimated:   isAnimated,
			textureBytes: res.TextureBytes,
		}
	}
	p.postProcessData = postProcData
	for i := range res.Meshes {
		t := res.Meshes[i].Textures
		p.Dependencies = slices.Grow(p.Dependencies, len(p.Dependencies)+len(t))
		for k, v := range t {
			if strings.HasPrefix(v, "embedded_") {
				continue
			}
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
	meshes := proc.postProcessData.(map[string]meshImportPostProcData)
	cc, err := cache.Read(res.Id)
	if err != nil {
		return err
	}
	variant, ok := meshes[cc.Config.Name]
	if !ok {
		slog.Error("failed to locate the mesh in the post processing data", "name", cc.Config.Name)
		return nil
	}
	texKeyToDepId := make(map[string]string)
	texKeyToData := make(map[string][]byte)
	for i := range variant.meshes {
		for texType, texKey := range variant.meshes[i].Textures {
			if strings.HasPrefix(texKey, "embedded_") {
				if _, ok := texKeyToData[texKey]; !ok {
					texKeyToData[texKey] = variant.textureBytes[texKey]
				}
				variant.meshes[i].Textures[texType] = texKey
			}
		}
	}
	for texKey, data := range texKeyToData {
		ext := ".png"
		if len(data) > 0 {
			if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e && data[3] == 0x47 {
				ext = ".png"
			} else if data[0] == 0xff && data[1] == 0xd8 {
				ext = ".jpg"
			} else if data[0] == 0x42 && data[1] == 0x4d {
				ext = ".bmp"
			} else if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
				ext = ".webp"
			}
		}
		tf, err := os.CreateTemp("", "*-kaiju-texture"+ext)
		if err != nil {
			continue
		}
		if _, err := tf.Write(data); err != nil {
			tf.Close()
			os.Remove(tf.Name())
			continue
		}
		tf.Close()
		texRes, err := Import(tf.Name(), fs, cache, linkedId)
		if err != nil {
			os.Remove(tf.Name())
			continue
		}
		res.Dependencies = append(res.Dependencies, texRes[0])
		texKeyToDepId[texKey] = texRes[0].Id
		os.Remove(tf.Name())
	}
	for i := range variant.meshes {
		for texType, texKey := range variant.meshes[i].Textures {
			if depId, ok := texKeyToDepId[texKey]; ok {
				variant.meshes[i].Textures[texType] = depId
			} else if strings.HasPrefix(texKey, "embedded_") {
				variant.meshes[i].Textures[texType] = ""
			}
		}
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
	// Determine if a matching material already exists; if so, reuse its ID.
	matId := ""
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
			matId = options[i].Id()
			break
		}
	}
	if matId == "" {
		f, err := os.CreateTemp("", "*-kaiju-mat.material")
		if err != nil {
			return err
		}
		tempPath := f.Name()
		defer os.Remove(tempPath)
		if err = json.NewEncoder(f).Encode(mat); err != nil {
			f.Close()
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		matRes, err := Import(tempPath, fs, cache, linkedId)
		if err != nil {
			return err
		}
		res.Dependencies = append(res.Dependencies, matRes[0])
		matId = matRes[0].Id
		ccMat, err := cache.Read(matId)
		if err != nil {
			return err
		}
		_, err = cache.Rename(ccMat.Id(), fmt.Sprintf("%s_mat", cc.Config.Name), fs)
		if !errors.Is(err, CacheContentNameEqual) {
			return err
		}
	}
	return meshGenerateTemplates(proc, res, fs, cache, linkedId, variant, matId, cc.Config.SrcName)
}

// meshGenerateTemplates creates one .template file per GLTF scene root node
// once all mesh variants for the same source file have been imported.
func meshGenerateTemplates(_ ProcessedImport, res *ImportResult, fs *project_file_system.FileSystem, cache *Cache, linkedId string, variant meshImportPostProcData, matId, meshSrcName string) error {
	if len(variant.nodes) == 0 {
		return nil
	}
	// Build nodeNameToMeshId and nodeNameToMatId maps. For a multi-variant file
	// all variants share the same linkedId, so we can use ReadLinked to gather
	// the full set. For a single-variant file with no external dependencies
	// (linkedId == ""), we derive the IDs from the current result directly.
	nodeNameToMeshId := make(map[string]string)
	nodeNameToMatId := make(map[string]string)
	meshNodeCount := len(variant.meshes)

	if linkedId != "" {
		linkedAll, err := cache.ReadLinked(linkedId)
		if err != nil {
			return nil
		}
		linkedMeshCount := 0
		for _, lc := range linkedAll {
			switch lc.Config.Type {
			case Mesh{}.TypeName():
				linkedMeshCount++
				nodeNameToMeshId[lc.Config.SrcName] = lc.Id()
			case Material{}.TypeName():
				if strings.HasSuffix(lc.Config.Name, "_mat") {
					mn := strings.TrimSuffix(lc.Config.Name, "_mat")
					nodeNameToMatId[mn] = lc.Id()
				}
			}
		}
		// Not all meshes have been imported yet — defer template generation.
		if linkedMeshCount < meshNodeCount {
			return nil
		}
	} else {
		// Single-variant, no external-dependency case.
		nodeNameToMeshId[meshSrcName] = res.Id
		nodeNameToMatId[meshSrcName] = matId
	}

	// For the single-variant case the effective linkedId for the template is
	// the mesh's own content ID, giving a stable anchor for future lookups.
	templateLinkedId := linkedId
	if templateLinkedId == "" {
		templateLinkedId = res.Id
	}

	// Map from node ID → all mesh names for that node (a GLTF node may have
	// multiple primitives, each becoming a separate imported mesh).
	nodeIdToMeshNames := make(map[int32][]string, len(variant.meshes))
	for i := range variant.meshes {
		if variant.meshes[i].Node != nil {
			id := variant.meshes[i].Node.Id
			nodeIdToMeshNames[id] = append(nodeIdToMeshNames[id], variant.meshes[i].Name)
		}
	}

	// Build parent-to-children index.
	childrenByParent := make(map[int][]int, len(variant.nodes))
	for i, n := range variant.nodes {
		if n.Parent >= 0 {
			childrenByParent[n.Parent] = append(childrenByParent[n.Parent], i)
		}
	}

	emptyKey := engine_entity_data_empty.BindingKey()

	var buildDesc func(nodeIdx int) stages.EntityDescription
	buildDesc = func(nodeIdx int) stages.EntityDescription {
		n := variant.nodes[nodeIdx]
		desc := stages.EntityDescription{
			Name:     n.Name,
			Position: n.Position,
			Rotation: n.Rotation.ToEuler(),
			Scale:    n.Scale,
		}
		if meshNames, hasMesh := nodeIdToMeshNames[n.Id]; hasMesh {
			desc.Mesh = nodeNameToMeshId[meshNames[0]]
			desc.Material = nodeNameToMatId[meshNames[0]]
			// Additional primitives of the same GLTF node become zero-offset
			// child entities so that all materials are represented.
			for _, mn := range meshNames[1:] {
				child := stages.EntityDescription{
					Name:     mn,
					Position: matrix.Vec3Zero(),
					Rotation: matrix.Vec3Zero(),
					Scale:    matrix.Vec3One(),
					Mesh:     nodeNameToMeshId[mn],
					Material: nodeNameToMatId[mn],
				}
				desc.Children = append(desc.Children, child)
			}
		} else {
			desc.DataBinding = []stages.EntityDataBinding{
				{RegistraionKey: emptyKey},
			}
		}
		for _, childIdx := range childrenByParent[nodeIdx] {
			desc.Children = append(desc.Children, buildDesc(childIdx))
		}
		return desc
	}

	for i, n := range variant.nodes {
		if n.Parent != -1 {
			continue
		}
		desc := buildDesc(i)
		templateData, err := json.Marshal(desc)
		if err != nil {
			slog.Error("failed to marshal entity template", "node", n.Name, "error", err)
			continue
		}
		tf, err := os.CreateTemp("", "*-kaiju.template")
		if err != nil {
			slog.Error("failed to create temp template file", "error", err)
			continue
		}
		if _, err = tf.Write(templateData); err != nil {
			tf.Close()
			os.Remove(tf.Name())
			continue
		}
		tf.Close()
		tempPath := tf.Name()
		tmplRes, err := Import(tempPath, fs, cache, templateLinkedId)
		os.Remove(tempPath)
		if err != nil || len(tmplRes) == 0 {
			slog.Error("failed to import entity template", "node", n.Name, "error", err)
			continue
		}
		res.Dependencies = append(res.Dependencies, tmplRes[0])
		if _, err = cache.Rename(tmplRes[0].Id, n.Name, fs); err != nil && !errors.Is(err, CacheContentNameEqual) {
			slog.Error("failed to rename entity template", "node", n.Name, "error", err)
		}
	}
	return nil
}
