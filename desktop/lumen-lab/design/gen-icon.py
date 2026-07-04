#!/usr/bin/env python3
"""Generate premium 3D DNA icon for Lumen Science (Verdant Oasis palette)."""
import math
import sys

FOREST = "#047857"
FOREST_700 = "#065f46"
FOREST_200 = "#a7f3d0"
FOREST_50 = "#ecfdf5"
GOLD = "#b45309"
GOLD_50 = "#fffbeb"
GOLD_100 = "#fef3c7"
GOLD_700 = "#92400e"
GOLD_600 = "#d97706"
GOLD_800 = "#78350f"

cx = 512
y0, y1 = 184, 840
height = y1 - y0
cycles = 2.62
omega = 2 * math.pi * cycles / height
A = 126
n_rungs = 11
steps = 320


def sx(y, phase=0.0):
    return cx + A * math.sin(omega * (y - y0) + phase)


ys = [y0 + height * i / steps for i in range(steps + 1)]
left_pts = [(sx(y, 0.0), y) for y in ys]
right_pts = [(sx(y, math.pi), y) for y in ys]


def catmull_rom_path(points, tension=0.26):
    if len(points) < 2:
        return ""
    parts = [f"M {points[0][0]:.2f} {points[0][1]:.2f}"]
    for i in range(len(points) - 1):
        p0 = points[max(i - 1, 0)]
        p1 = points[i]
        p2 = points[i + 1]
        p3 = points[min(i + 2, len(points) - 1)]
        c1x = p1[0] + (p2[0] - p0[0]) * tension
        c1y = p1[1] + (p2[1] - p0[1]) * tension
        c2x = p2[0] - (p3[0] - p1[0]) * tension
        c2y = p2[1] - (p3[1] - p1[1]) * tension
        parts.append(
            f"C {c1x:.2f} {c1y:.2f} {c2x:.2f} {c2y:.2f} {p2[0]:.2f} {p2[1]:.2f}"
        )
    return " ".join(parts)


def slice_strand(points, ya, yb, phase):
    sub = [(x, y) for x, y in points if ya <= y <= yb]
    if len(sub) < 2:
        return []
    if sub[0][1] > ya:
        sub.insert(0, (sx(ya, phase), ya))
    if sub[-1][1] < yb:
        sub.append((sx(yb, phase), yb))
    return sub


rung_ys = [y0 + height * (i + 0.5) / n_rungs for i in range(n_rungs)]
bounds = [y0] + rung_ys + [y1]

back_paths, front_paths = [], []
for i in range(len(bounds) - 1):
    ya, yb = bounds[i], bounds[i + 1]
    left_seg = slice_strand(left_pts, ya, yb, 0.0)
    right_seg = slice_strand(right_pts, ya, yb, math.pi)
    if i % 2 == 0:
        if len(right_seg) >= 2:
            back_paths.append(catmull_rom_path(right_seg))
        if len(left_seg) >= 2:
            front_paths.append(catmull_rom_path(left_seg))
    else:
        if len(left_seg) >= 2:
            back_paths.append(catmull_rom_path(left_seg))
        if len(right_seg) >= 2:
            front_paths.append(catmull_rom_path(right_seg))


def tube_layers(path, width, body_grad, shadow_w=0, highlight=True, opacity=1.0):
    layers = []
    if shadow_w:
        layers.append(
            f'<path d="{path}" stroke="#021a14" stroke-width="{shadow_w}" '
            f'stroke-linecap="round" stroke-linejoin="round" fill="none" '
            f'opacity="{0.42 * opacity:.2f}" transform="translate(2.5, 4.2)"/>'
        )
    layers.append(
        f'<path d="{path}" stroke="{body_grad}" stroke-width="{width}" '
        f'stroke-linecap="round" stroke-linejoin="round" fill="none" '
        f'opacity="{opacity:.2f}" filter="url(#tubeDepth)"/>'
    )
    if highlight:
        layers.append(
            f'<path d="{path}" stroke="url(#tubeSpecular)" stroke-width="{max(4, width * 0.28):.1f}" '
            f'stroke-linecap="round" stroke-linejoin="round" fill="none" '
            f'opacity="{0.55 * opacity:.2f}"/>'
        )
    return "\n    ".join(layers)


back_tubes = "\n    ".join(
    tube_layers(p, 14, "url(#strandBack3d)", shadow_w=18, highlight=True, opacity=0.88)
    for p in back_paths
)
front_tubes = "\n    ".join(
    tube_layers(p, 18, "url(#strandFront3d)", shadow_w=23, highlight=True, opacity=1.0)
    for p in front_paths
)

rung_svg = []
for y in rung_ys:
    x1, x2 = sx(y, 0.0), sx(y, math.pi)
    if x1 > x2:
        x1, x2 = x2, x1
    mx = (x1 + x2) / 2
    rung_svg.append(
        f'<line x1="{x1:.1f}" y1="{y+2.2:.1f}" x2="{x2:.1f}" y2="{y+2.2:.1f}" '
        f'stroke="#021a14" stroke-width="5.2" stroke-linecap="round" opacity="0.28"/>'
    )
    rung_svg.append(
        f'<line x1="{x1:.1f}" y1="{y:.1f}" x2="{x2:.1f}" y2="{y:.1f}" '
        f'stroke="url(#rung3d)" stroke-width="4.2" stroke-linecap="round" filter="url(#rungGlow)"/>'
    )
    rung_svg.append(
        f'<line x1="{x1+8:.1f}" y1="{y-0.8:.1f}" x2="{x2-8:.1f}" y2="{y-0.8:.1f}" '
        f'stroke="{GOLD_50}" stroke-width="1.2" stroke-linecap="round" opacity="0.45"/>'
    )

node_svg = []
for y in rung_ys:
    for x in (sx(y, 0.0), sx(y, math.pi)):
        node_svg.append(
            f'<ellipse cx="{x:.1f}" cy="{y+3.6:.1f}" rx="7.8" ry="2.6" fill="#000000" opacity="0.22"/>'
        )
        node_svg.append(
            f'<circle cx="{x:.1f}" cy="{y:.1f}" r="8.4" fill="url(#sphereBase)"/>'
        )
        node_svg.append(
            f'<circle cx="{x:.1f}" cy="{y:.1f}" r="7.2" fill="url(#sphereMid)"/>'
        )
        node_svg.append(
            f'<circle cx="{x-2.4:.1f}" cy="{y-2.6:.1f}" r="3.1" fill="url(#sphereSpec)"/>'
        )
        node_svg.append(
            f'<circle cx="{x-3.1:.1f}" cy="{y-3.4:.1f}" r="1.15" fill="#ffffff" opacity="0.92"/>'
        )

svg = f'''<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 1024 1024" fill="none">
  <defs>
    <linearGradient id="bg" x1="100" y1="60" x2="940" y2="980" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="{GOLD_700}"/>
      <stop offset="0.22" stop-color="#7c2d12"/>
      <stop offset="0.42" stop-color="{FOREST_700}"/>
      <stop offset="0.68" stop-color="{FOREST}"/>
      <stop offset="1" stop-color="#022c22"/>
    </linearGradient>
    <radialGradient id="stageGlow" cx="512" cy="470" r="360" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="{GOLD_600}" stop-opacity="0.34"/>
      <stop offset="0.55" stop-color="{GOLD}" stop-opacity="0.1"/>
      <stop offset="1" stop-color="{FOREST_700}" stop-opacity="0"/>
    </radialGradient>
    <radialGradient id="vignette" cx="512" cy="540" r="640" gradientUnits="userSpaceOnUse">
      <stop offset="0.58" stop-color="#000000" stop-opacity="0"/>
      <stop offset="1" stop-color="#000000" stop-opacity="0.48"/>
    </radialGradient>
    <linearGradient id="strandFront3d" x1="280" y1="160" x2="760" y2="160" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="{GOLD_800}"/>
      <stop offset="0.22" stop-color="{GOLD_700}"/>
      <stop offset="0.42" stop-color="{GOLD}"/>
      <stop offset="0.52" stop-color="{GOLD_50}"/>
      <stop offset="0.58" stop-color="#fde68a"/>
      <stop offset="0.72" stop-color="{GOLD_600}"/>
      <stop offset="1" stop-color="{GOLD_800}"/>
    </linearGradient>
    <linearGradient id="strandBack3d" x1="280" y1="160" x2="760" y2="160" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#0f766e" stop-opacity="0.65"/>
      <stop offset="0.35" stop-color="{FOREST_200}" stop-opacity="0.55"/>
      <stop offset="0.52" stop-color="{FOREST_50}"/>
      <stop offset="0.65" stop-color="#ecfdf5"/>
      <stop offset="1" stop-color="#0d9488" stop-opacity="0.6"/>
    </linearGradient>
    <linearGradient id="tubeSpecular" x1="300" y1="200" x2="700" y2="200" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="#ffffff" stop-opacity="0"/>
      <stop offset="0.45" stop-color="#fffef7" stop-opacity="0.55"/>
      <stop offset="0.55" stop-color="#ffffff" stop-opacity="0"/>
      <stop offset="1" stop-color="#ffffff" stop-opacity="0"/>
    </linearGradient>
    <linearGradient id="rung3d" x1="340" y1="0" x2="680" y2="0" gradientUnits="userSpaceOnUse">
      <stop offset="0" stop-color="{GOLD_700}"/>
      <stop offset="0.5" stop-color="{GOLD_600}"/>
      <stop offset="1" stop-color="{GOLD_700}"/>
    </linearGradient>
    <radialGradient id="sphereBase" cx="0" cy="0" r="1" gradientUnits="userSpaceOnUse" gradientTransform="translate(0 1) scale(9)">
      <stop offset="0" stop-color="{GOLD_800}"/>
      <stop offset="1" stop-color="#451a03"/>
    </radialGradient>
    <radialGradient id="sphereMid" cx="0" cy="0" r="1" gradientUnits="userSpaceOnUse" gradientTransform="translate(-2 -2) scale(8)">
      <stop offset="0" stop-color="#fde68a"/>
      <stop offset="0.38" stop-color="{GOLD_600}"/>
      <stop offset="0.72" stop-color="{GOLD}"/>
      <stop offset="1" stop-color="{GOLD_700}"/>
    </radialGradient>
    <radialGradient id="sphereSpec" cx="0" cy="0" r="1" gradientUnits="userSpaceOnUse" gradientTransform="translate(-1.5 -1.5) scale(4)">
      <stop offset="0" stop-color="#ffffff"/>
      <stop offset="0.55" stop-color="{GOLD_50}"/>
      <stop offset="1" stop-color="#ffffff" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="sheen" x1="180" y1="140" x2="880" y2="900" gradientUnits="userSpaceOnUse">
      <stop offset="0.4" stop-color="#ffffff" stop-opacity="0"/>
      <stop offset="0.5" stop-color="{GOLD}" stop-opacity="0.16"/>
      <stop offset="0.6" stop-color="#ffffff" stop-opacity="0"/>
    </linearGradient>
    <filter id="tubeDepth" x="-20%" y="-10%" width="140%" height="125%">
      <feDropShadow dx="0" dy="5" stdDeviation="4" flood-color="#000000" flood-opacity="0.45"/>
      <feDropShadow dx="-2" dy="-3" stdDeviation="1.5" flood-color="#fde68a" flood-opacity="0.22"/>
    </filter>
    <filter id="rungGlow" x="-10%" y="-80%" width="120%" height="260%">
      <feDropShadow dx="0" dy="2" stdDeviation="1.5" flood-color="{GOLD_700}" flood-opacity="0.35"/>
    </filter>
    <clipPath id="floorClip">
      <rect x="0" y="700" width="1024" height="324"/>
    </clipPath>
  </defs>

  <rect width="1024" height="1024" fill="url(#bg)"/>
  <rect width="1024" height="1024" fill="url(#stageGlow)"/>
  <ellipse cx="512" cy="900" rx="280" ry="42" fill="#000000" opacity="0.22"/>
  <rect width="1024" height="1024" fill="url(#vignette)"/>

  <g id="dna" opacity="0.07" clip-path="url(#floorClip)" transform="translate(0, 1480) scale(1, -1)">
    {back_tubes}
    {front_tubes}
  </g>

  <g id="dna-main">
    {back_tubes}
    <g id="rungs">{chr(10).join("    " + x for x in rung_svg)}</g>
    {front_tubes}
    <g id="nodes">{chr(10).join("    " + x for x in node_svg)}</g>
  </g>

  <rect width="1024" height="1024" fill="url(#sheen)" style="mix-blend-mode:soft-light"/>
</svg>
'''

out = sys.argv[1] if len(sys.argv) > 1 else "/Users/lei/lumen/desktop/lumen-science/design/icon.svg"
with open(out, "w", encoding="utf-8") as f:
    f.write(svg)
print(out)