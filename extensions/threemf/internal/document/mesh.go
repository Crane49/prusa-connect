package document

import "github.com/prusa3d/threemf/threemf"

// MeshOf renders one object's mesh for the mesh route.
func MeshOf(object int, mesh *threemf.Mesh) ProjectMesh {
	out := ProjectMesh{
		Object:          object,
		Vertices:        mesh.Vertices,
		Triangles:       mesh.Triangles,
		Parts:           make([]ProjectMeshPart, 0, len(mesh.Parts)),
		SourceTriangles: mesh.SourceTriangles,
		Box:             mesh.Box,
	}

	if out.Vertices == nil {
		out.Vertices = []float32{}
	}

	if out.Triangles == nil {
		out.Triangles = []uint32{}
	}

	for _, part := range mesh.Parts {
		out.Parts = append(out.Parts, ProjectMeshPart{
			Name: part.Name, Extruder: part.Extruder, Triangles: part.Triangles,
		})
	}

	return out
}
