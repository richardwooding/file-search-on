# Recipes — 3D models

file-search-on detects three 3D model formats and surfaces triage-grade geometry metadata — no mesh library, no third-party deps.

| Content type | Extensions | Notes |
|---|---|---|
| `model3d/stl` | `.stl` | ASCII + binary STL (3D printing) |
| `model3d/obj` | `.obj` | Wavefront OBJ (line-oriented ASCII mesh) |
| `model3d/gltf` | `.gltf`, `.glb` | glTF 2.0 (JSON + binary container) |

The umbrella `is_3d_model` matches any of them; per-format predicates are `is_stl`, `is_obj`, `is_gltf`.

## Attributes

| Attribute | Type | Source |
|---|---|---|
| `model3d_format` | string | `stl` / `obj` / `gltf` |
| `vertex_count` | int | STL: 3 × triangle count (O(1) from binary header). OBJ: `v ` lines. glTF: Σ POSITION accessor counts |
| `face_count` | int | STL: triangle count. OBJ: `f ` lines. glTF: indices ÷ 3 |
| `has_normals` | bool | STL facets always; OBJ `vn `; glTF primitive NORMAL attribute |
| `has_textures` | bool | OBJ `mtllib` reference; glTF embedded `images` / `textures` |
| `materials` | list | OBJ `usemtl` names; glTF `materials[].name` |
| `bounding_box` | list | `[minX, minY, minZ, maxX, maxY, maxZ]` |

```sh
D=~/Models
```

## Find + triage

```sh
# Every 3D model under a tree
file-search-on 'is_3d_model' -d $D

# Big STLs — printer-bed / slicer-load audit
file-search-on 'is_stl && size > 104857600' -d $D --sort size --order desc

# Print-friendly STLs (bounded triangle count)
file-search-on 'is_stl && face_count > 0 && face_count < 1000000' -d $D

# Heavyweight meshes by face count (across all formats)
file-search-on 'is_3d_model && face_count > 5000000' -d $D --sort face_count --order desc

# Models that ship with materials vs mesh-only exports
file-search-on 'is_3d_model && size(materials) > 0' -d $D
file-search-on 'is_3d_model && size(materials) == 0' -d $D    # mesh-only
```

## Textures + normals

```sh
# OBJ models with a material library (textured / shaded)
file-search-on 'is_obj && has_textures' -d $D

# glTF/GLB game assets that embed textures
file-search-on 'is_gltf && has_textures' -d $D

# Meshes MISSING normals (need normal recomputation before rendering)
file-search-on 'is_3d_model && !has_normals' -d $D

# Find a specific material across a model library
file-search-on 'is_3d_model && "Steel" in materials' -d $D
```

## Bounding box — scale queries

`bounding_box` is `[minX, minY, minZ, maxX, maxY, maxZ]`. Compute extents in CEL with index arithmetic:

```sh
# Models wider than 200 units on X (e.g. mm — won't fit a 200mm printer bed)
file-search-on 'is_stl && size(bounding_box) == 6 && (bounding_box[3] - bounding_box[0]) > 200.0' -d $D

# Dump each model's X/Y/Z extents as JSON
file-search-on 'is_3d_model' -d $D -o json | \
  jq -r 'select(.bounding_box) | "\(.path | split("/")[-1])  \(.bounding_box[3]-.bounding_box[0]) x \(.bounding_box[4]-.bounding_box[1]) x \(.bounding_box[5]-.bounding_box[2])"'
```

## Stats

```sh
# Models per format
file-search-on stats 'is_3d_model' --group-by content_type -d $D

# Total vertices across the library (top-N densest)
file-search-on 'is_3d_model' -d $D --sort vertex_count --order desc --limit 10 -o json | \
  jq -r '"\(.vertex_count)\t\(.path | split("/")[-1])"'
```

## Organize a model library

Pairs with the `organize` subcommand to build a sorted view:

```sh
# Bucket models by format, then materials-vs-mesh-only
file-search-on organize 'is_3d_model' \
  --link-into '~/sorted-models/{model3d_format}/{basename}' -d $D
```

## Known limitations

- **Triage, not validation.** No manifold / watertight / self-intersection checks — the use case is finding and sorting models, not verifying printability beyond face-count heuristics.
- **glTF counts are best-effort.** vertex / face counts come from the accessor table assuming triangle-list primitives; exotic primitive modes (line / point / strip) aren't decoded. The bounding box comes from the POSITION accessor's declared min/max — accurate when the exporter wrote them (the spec requires it for POSITION).
- **OBJ `has_textures` means "has a material library".** OBJ itself references textures indirectly through the `.mtl` file; a `mtllib` line sets the flag. The `.mtl` is not parsed for the actual texture-image paths.
- **COFF `.obj` files** (Windows compiled objects) share the extension and detect as `model3d/obj`, but produce empty attrs (no `v `/`f ` lines).
- **Out of scope**: FBX (proprietary), PLY, USDZ, and texture-image extraction.
