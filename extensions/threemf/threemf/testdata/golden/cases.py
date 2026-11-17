"""Write the golden probe specs. Each probe is one object; it is printed by the
command line iff the slicer puts it on the sliced bed and judges it inside."""
import json, math, os, sys

OUT = os.path.dirname(os.path.abspath(__file__))


def box(x0, y0, z0, x1, y1, z1, t='ModelPart'):
    return {'type': t, 'box': [x0, y0, z0, x1, y1, z1]}


def frustum(cx, cy, zb, hb, zt, ht):
    """Square frustum: bottom half-width hb at zb, top half-width ht at zt."""
    v = [[cx - hb, cy - hb, zb], [cx + hb, cy - hb, zb], [cx + hb, cy + hb, zb], [cx - hb, cy + hb, zb],
         [cx - ht, cy - ht, zt], [cx + ht, cy - ht, zt], [cx + ht, cy + ht, zt], [cx - ht, cy + ht, zt]]
    t = [[0, 2, 1], [0, 3, 2], [4, 5, 6], [4, 6, 7], [0, 1, 5], [0, 5, 4], [1, 2, 6], [1, 6, 5], [2, 3, 7], [2, 7, 6], [3, 0, 4], [3, 4, 7]]
    return {'type': 'ModelPart', 'verts': v, 'tris': t}


def rz(deg, x, y, z=0):
    c, s = math.cos(math.radians(deg)), math.sin(math.radians(deg))
    # 3MF row-vector layout: x' = x*m00 + y*m10 ...
    return [c, s, 0, -s, c, 0, 0, 0, 1, x, y, z]


def obj(name, parts, inst):
    return {'name': name, 'parts': parts, 'instances': inst if isinstance(inst, list) else [inst]}


def at(x, y, z=0, printable=1):
    return {'at': [x, y, z], 'printable': printable}


CUBE = [box(0, 0, 0, 10, 10, 10)]

rect_probes = [
    obj('r_in', CUBE, at(20, 20)),
    obj('r_edge_x', CUBE, at(240, 100)),
    obj('r_edge_corner', CUBE, at(240, 200)),
    obj('r_eps_x_in', CUBE, at(240.00005, 120)),
    obj('r_eps_x_out', CUBE, at(240.0002, 140)),
    obj('r_eps_neg_in', CUBE, at(-0.00005, 40)),
    obj('r_eps_neg_out', CUBE, at(-0.0002, 60)),
    obj('r_partly', CUBE, at(245, 160)),
    obj('r_not_printable', CUBE, at(60, 60, 0, 0)),
    obj('r_rot45_in', CUBE, {'t': rz(45, 100, 20)}),
    obj('r_rot45_corner_out', CUBE, {'t': rz(45, 245, 100)}),
    obj('r_mirror_in', CUBE, {'t': [-1, 0, 0, 0, 1, 0, 0, 0, 1, 250, 80, 0]}),
    obj('r_mirror_out', CUBE, {'t': [-1, 0, 0, 0, 1, 0, 0, 0, 1, 5, 80, 0]}),
    obj('r_scale2_in', CUBE, {'t': [2, 0, 0, 0, 2, 0, 0, 0, 2, 230, 20, 0]}),
    obj('r_scale2_out', CUBE, {'t': [2, 0, 0, 0, 2, 0, 0, 0, 2, 235, 40, 0]}),
    obj('r_tilt_in', CUBE, {'t': [1, 0, 0, 0, 0, 1, 0, -1, 0, 150, 150, 0]}),
    obj('r_height_exact', [box(0, 0, 0, 5, 5, 220)], at(150, 20)),
    obj('r_height_eps_in', [box(0, 0, 0, 5, 5, 220.00005)], at(160, 20)),
    obj('r_height_over', [box(0, 0, 0, 5, 5, 220.5)], at(170, 20)),
    obj('r_sink_frustum', [frustum(245, 100, -20, 17, 1, 4)], at(0, 0)),
    obj('r_sink_wide_ring', [frustum(245, 60, -1, 17, 20, 4)], at(0, 0)),
    obj('r_sink_box', [box(0, 0, -5, 10, 10, 5)], at(100, 180)),
    obj('r_modifier_first', [box(400, 0, 0, 410, 10, 10, 'ParameterModifier'), box(0, 0, 0, 10, 10, 10)], at(30, 120)),
    obj('r_negative_first', [box(-500, 0, 0, -490, 10, 10, 'NegativeVolume'), box(0, 0, 0, 10, 10, 10)], at(30, 140)),
    # first model part on bed 1 (x >= 347.95), second on bed 0: the instance belongs to bed 1.
    obj('r_first_on_bed1', [box(400, 0, 0, 410, 10, 10), box(0, 0, 0, 10, 10, 10)], at(30, 160)),
    # first model part on bed 0, second on bed 1: the instance belongs to bed 0 and is judged inside.
    obj('r_first_on_bed0', [box(0, 0, 0, 10, 10, 10), box(400, 0, 0, 410, 10, 10)], at(60, 160)),
    # first model part wholly below the bed: it is "on" bed 0, whatever its plane position.
    obj('r_below_first', [box(400, 0, -20, 410, 10, -10), box(400, 20, 0, 410, 30, 10)], at(0, 30)),
    # the first part off every bed, the second on bed 0: the second's bed, but partly outside.
    obj('r_first_off_all', [box(-2000, 0, 0, -1990, 10, 10), box(0, 0, 0, 10, 10, 10)], at(90, 190)),
]


def circle(r, n=72):
    return [[round(r * math.cos(2 * math.pi * i / n), 6), round(r * math.sin(2 * math.pi * i / n), 6)] for i in range(n)]


a = math.radians(2.5)
circ_probes = [
    obj('c_in', CUBE, at(-5, -5)),
    obj('c_corner_in', CUBE, at(60, 60)),
    obj('c_corner_out', CUBE, at(61, 61)),
    # a vertex between two polygon corners, outside the polygon, inside the circle
    obj('c_chord', [box(90, 0, 0, 99.945 * math.cos(a), 99.945 * math.sin(a), 5)], at(0, 0)),
    obj('c_chord_out', [box(90, 0, 0, 100.05 * math.cos(a), 100.05 * math.sin(a), 5)], at(0, -20)),
]

neg_probes = [
    obj('n_in', CUBE, at(-120, -100)),
    obj('n_edge', CUBE, at(115, 95)),
    obj('n_partly', CUBE, at(120, 50)),
    obj('n_origin', CUBE, at(-5, -5)),
]

L = [[0, 0], [250, 0], [250, 100], [125, 100], [125, 210], [0, 210]]
l_probes = [
    obj('l_in', CUBE, at(20, 20)),
    obj('l_notch', CUBE, at(200, 150)),
    obj('l_notch_edge', CUBE, at(130, 95)),
]

specs = {
    'ps2_rect': {'fmt': 'ps2', 'objects': rect_probes},
    'ps2_circle': {'fmt': 'ps2', 'bed_shape': circle(100), 'max_print_height': 200, 'objects': circ_probes},
    'ps2_neg': {'fmt': 'ps2', 'bed_shape': [[-125, -105], [125, -105], [125, 105], [-125, 105]], 'objects': neg_probes},
    'ps2_l': {'fmt': 'ps2', 'bed_shape': L, 'objects': l_probes},
}


def ps3_from(objs, beds, shape=None, h=220, extra=None):
    c = {'beds': beds, 'max_print_height': h}
    if shape:
        c['bed_shape'] = shape
    out = {'fmt': 'ps3', 'containers': [c] + (extra or []), 'objects': objs}
    return out


rect3 = [o for o in rect_probes if o['name'] not in ('r_first_on_bed1', 'r_first_on_bed0', 'r_below_first')]
specs.update({
    'ps3_rect': ps3_from(rect3, [[0, 0]], [[0, 0], [250, 0], [250, 210], [0, 210]]),
    # the same probes, the sliced bed at an offset: coordinates must come back bed-local
    'ps3_offset': ps3_from([dict(o, instances=[dict(i, **({'at': [i['at'][0] + 500, i['at'][1] + 300, i['at'][2]]} if 'at' in i else {'t': i['t'][:9] + [i['t'][9] + 500, i['t'][10] + 300, i['t'][11]]})) for i in o['instances']]) for o in rect3],
                           [[500, 300]], [[0, 0], [250, 0], [250, 210], [0, 210]]),
    'ps3_circle': ps3_from(circ_probes, [[0, 0]], circle(100), 200),
    'ps3_l': ps3_from(l_probes, [[0, 0]], L),
    # two setups whose first beds overlap: an instance inside both goes to the first setup
    'ps3_order': ps3_from([obj('o_both', CUBE, at(20, 20)), obj('o_second_only', CUBE, at(300, 20))],
                          [[0, 0]], [[0, 0], [250, 0], [250, 210], [0, 210]],
                          extra=[{'beds': [[0, 0]], 'bed_shape': [[0, 0], [400, 0], [400, 210], [0, 210]]}]),
    'ps3_below': ps3_from([obj('b_below', [box(0, 0, -20, 10, 10, -10)], at(20, 20)), obj('b_ok', CUBE, at(50, 20))],
                          [[0, 0]], [[0, 0], [250, 0], [250, 210], [0, 210]]),
})

for name, spec in specs.items():
    with open(os.path.join(OUT, name + '.json'), 'w') as f:
        json.dump(spec, f)
print(' '.join(specs))
