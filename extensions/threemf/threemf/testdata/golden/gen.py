"""Generate synthetic PrusaSlicer 2.x / 3.0 projects from a JSON spec.

usage: gen.py spec.json out.3mf
spec: {"fmt": "ps2"|"ps3", "app": "PrusaSlicer-2.9.6",
       "bed_shape": [[x,y],...], "max_print_height": 220,
       "containers": [{"bed_shape":..., "max_print_height":..., "beds": [[x,y],...]}],   # ps3
       "objects": [{"name": "P1", "parts": [{"type": "ModelPart", "box": [x0,y0,z0,x1,y1,z1]} |
                                            {"type":..., "verts": [[x,y,z]..], "tris": [[a,b,c]..]}],
                    "instances": [{"t": [12 numbers] | "at": [x,y,z], "printable": 1}]}]}
"""
import json, os, re, sys, zipfile

TEMPLATES = sys.argv[3] if len(sys.argv) > 3 else None
FIX = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..") + "/"


def box(b):
    x0, y0, z0, x1, y1, z1 = b
    v = [(x, y, z) for z in (z0, z1) for y in (y0, y1) for x in (x0, x1)]
    t = [(0, 2, 1), (1, 2, 3), (4, 5, 6), (5, 7, 6), (0, 1, 4), (1, 5, 4), (2, 6, 3), (3, 6, 7), (0, 4, 2), (2, 4, 6), (1, 3, 5), (3, 7, 5)]
    return v, t


def part_mesh(p):
    if 'box' in p:
        return box(p['box'])
    return [tuple(v) for v in p['verts']], [tuple(t) for t in p['tris']]


def transform(inst):
    if 't' in inst:
        return ' '.join(repr(float(x)) for x in inst['t'])
    x, y, z = inst.get('at', (0, 0, 0))
    return f'1 0 0 0 1 0 0 0 1 {x!r} {y!r} {z!r}'


def fmt_v(v):
    return ''.join(f'<vertex x="{a!r}" y="{b!r}" z="{c!r}"/>' for a, b, c in v)


def fmt_t(t, base=0):
    return ''.join(f'<triangle v1="{a+base}" v2="{b+base}" v3="{c+base}"/>' for a, b, c in t)


CT = ('<?xml version="1.0" encoding="UTF-8"?>\n<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
      '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
      '<Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>'
      '<Default Extension="png" ContentType="image/png"/><Default Extension="json" ContentType="application/json"/>'
      '<Default Extension="config" ContentType="text/plain"/><Default Extension="xml" ContentType="application/xml"/></Types>')
RELS = ('<?xml version="1.0" encoding="UTF-8"?>\n<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
        '<Relationship Target="/3D/3dmodel.model" Id="rel-1" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>{extra}</Relationships>')


def shape_text(shape):
    return ','.join(f'{x!r}x{y!r}'.replace('.0x', 'x') for x, y in shape)


def ps2(spec, out):
    cfg = zipfile.ZipFile(FIX + 'prusaslicer-2.9.6-multibed.3mf').read('Metadata/Slic3r_PE.config').decode()
    shape = spec.get('bed_shape', [[0, 0], [250, 0], [250, 210], [0, 210]])
    cfg = re.sub(r'^; bed_shape = .*$', '; bed_shape = ' + ','.join(f'{x:g}x{y:g}' for x, y in shape), cfg, flags=re.M)
    cfg = re.sub(r'^; max_print_height = .*$', f"; max_print_height = {spec.get('max_print_height', 220):g}", cfg, flags=re.M)
    cfg = re.sub(r'^; thumbnails = .*$', '; thumbnails = ', cfg, flags=re.M)
    objs, items, mc = [], [], []
    oid = 1
    for o in spec['objects']:
        verts, tris, vols = [], [], []
        for p in o['parts']:
            v, t = part_mesh(p)
            first = len(tris)
            tris += [(a + len(verts), b + len(verts), c + len(verts)) for a, b, c in t]
            verts += v
            vols.append((first, len(tris) - 1, p.get('type', 'ModelPart'), p.get('name', o['name'])))
        first_id = oid
        objs.append(f'<object id="{oid}" type="model"><mesh><vertices>{fmt_v(verts)}</vertices><triangles>{fmt_t(tris)}</triangles></mesh></object>')
        items.append((oid, o['instances'][0]))
        oid += 1
        for inst in o['instances'][1:]:
            objs.append(f'<object id="{oid}" type="model"><components><component objectid="{first_id}"/></components></object>')
            items.append((oid, inst))
            oid += 1
        vx = ''.join(
            f'<volume firstid="{a}" lastid="{b}"><metadata type="volume" key="name" value="{n}"/>'
            f'<metadata type="volume" key="volume_type" value="{ty}"/>'
            f'<metadata type="volume" key="matrix" value="1 0 0 0 0 1 0 0 0 0 1 0 0 0 0 1"/></volume>' for a, b, ty, n in vols)
        mc.append(f'<object id="{first_id}" instances_count="{len(o["instances"])}"><metadata type="object" key="name" value="{o["name"]}"/>{vx}</object>')
    build = ''.join(f'<item objectid="{i}" transform="{transform(inst)}" printable="{inst.get("printable", 1)}"/>' for i, inst in items)
    model = ('<?xml version="1.0" encoding="UTF-8"?>\n<model unit="millimeter" xml:lang="en-US" '
             'xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:slic3rpe="http://schemas.slic3r.org/3mf/2017/06">'
             f'<metadata name="slic3rpe:Version3mf">1</metadata><metadata name="Application">{spec.get("app", "PrusaSlicer-2.9.6")}</metadata>'
             f'<resources>{"".join(objs)}</resources><build>{build}</build></model>')
    with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as z:
        z.writestr('[Content_Types].xml', CT)
        z.writestr('_rels/.rels', RELS.format(extra=''))
        z.writestr('3D/3dmodel.model', model)
        z.writestr('Metadata/Slic3r_PE_model.config', '<?xml version="1.0" encoding="UTF-8"?>\n<config>' + ''.join(mc) + '</config>')
        z.writestr('Metadata/Slic3r_PE.config', cfg)


def ps3(spec, out):
    tpl = json.loads(zipfile.ZipFile(FIX + 'prusaslicer-3.0.0-alpha12-two-setups.3mf').read('Metadata/PrusaSlicer3_project.json'))
    base = tpl['config_containers'][0]
    containers = []
    for c in spec['containers']:
        cc = json.loads(json.dumps(base))
        ps = cc['configuration']['printer_settings']
        ps['bed_shape'] = [[float(x), float(y)] for x, y in c.get('bed_shape', [[0, 0], [250, 0], [250, 220], [0, 220]])]
        ps['max_print_height'] = float(c.get('max_print_height', 270))
        ps['binary_gcode'] = False
        cc['configuration']['print_settings']['wipe_tower'] = False
        cc['beds'] = [{'custom_gcode': None, 'position_x': float(x), 'position_y': float(y), 'wipe_tower': None} for x, y in c['beds']]
        containers.append(cc)
    res, items, jobjs = [], [], []
    nid = 1
    meshes, vols_all = [], []
    for o in spec['objects']:
        vol_ids = []
        for p in o['parts']:
            v, t = part_mesh(p)
            res.append(f'<object id="{nid}"><mesh><vertices>{fmt_v(v)}</vertices><triangles>{fmt_t(t)}</triangles></mesh></object>')
            vol_ids.append((nid, p.get('type', 'ModelPart'), p.get('name', o['name'])))
            nid += 1
        o['_vols'] = []
        for mid, ty, name in vol_ids:
            res.append(f'<object id="{nid}" name="{name}"><components><component objectid="{mid}"/></components></object>')
            o['_vols'].append((nid, ty))
            nid += 1
    for o in spec['objects']:
        comps = ''.join(f'<component objectid="{vid}"/>' for vid, _ in o['_vols'])
        res.append(f'<object id="{nid}" name="{o["name"]}"><components>{comps}</components></object>')
        insts = []
        for inst in o['instances']:
            if not inst.get('printable', 1):
                insts.append({'ord': len(items), 'printable': False})
            items.append((nid, inst))
        jo = {'id': nid, 'volumes': [{'id': vid, 'type': ty, 'volume_settings': {'wipe_into_infill': False}} for vid, ty in o['_vols']], 'object_settings': {'extruder': 0, 'wipe_into_objects': False}}
        if insts:
            jo['instances'] = insts
        jobjs.append(jo)
        nid += 1
    build = ''.join(f'<item objectid="{i}" transform="{transform(inst)}"/>' for i, inst in items)
    model = ('<?xml version="1.0" encoding="UTF-8"?>\n<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">'
             f'<metadata name="Application">{spec.get("app", "PrusaSlicer-3.0.0-alpha12")}</metadata>'
             f'<resources>{"".join(res)}</resources><build>{build}</build></model>')
    doc = {'objects': jobjs, 'project': tpl['project'], 'config_containers': containers}
    with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as z:
        z.writestr('_rels/.rels', RELS.format(extra='<Relationship Target="/Metadata/PrusaSlicer3_project.json" Id="rel-2" Type="http://schemas.prusa3d.cz/package/2024/relationships/metadata/projectfile"/>'))
        z.writestr('[Content_Types].xml', CT)
        z.writestr('3D/3dmodel.model', model)
        z.writestr('Metadata/PrusaSlicer3_project.json', json.dumps(doc))


if __name__ == '__main__':
    spec = json.load(open(sys.argv[1]))
    (ps3 if spec.get('fmt') == 'ps3' else ps2)(spec, sys.argv[2])
