#!/usr/bin/env python3
"""Build docs/architecture/c4-set.html from docs/architecture/diagrams/manifest.json.

The page references the PNG screenshots by relative path, so it stays small and shows
exactly the images that were reviewed. Usage: python3 scripts/diagrams/build_page.py
"""
import html
import json
import struct
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ARCH = ROOT / "docs" / "architecture"
D = ARCH / "diagrams"
m = json.loads((D / "manifest.json").read_text())


def png_size(path):
    with open(path, "rb") as f:
        head = f.read(24)
    return struct.unpack(">II", head[16:24])


def esc(s):
    return html.escape(str(s), quote=True)


def code(s):
    """Escape, then turn `x` into <code>x</code>."""
    out, parts = [], esc(s).split("`")
    for i, p in enumerate(parts):
        out.append(f"<code>{p}</code>" if i % 2 else p)
    return "".join(out)


levels = m["levels"]
by_level = {lv["key"]: [d for d in m["diagrams"] if d["level"] == lv["key"]] for lv in levels}
missing = [d["id"] for d in m["diagrams"] if not (D / "png" / f"{d['id']}.png").exists()]
if missing:
    raise SystemExit(f"missing PNG for: {', '.join(missing)}")

nav = '<li><a href="#brief">Decisions</a><span>%d</span></li>' % len(m["decisions"])
nav += "".join(
    f'<li><a href="#{lv["key"]}">{esc(lv["label"])}</a><span>{len(by_level[lv["key"]])}</span></li>'
    for lv in levels if by_level[lv["key"]]
)

sheets = []
for lv in levels:
    items = by_level[lv["key"]]
    if not items:
        continue
    sheets.append(
        f'<section class="level" id="{lv["key"]}"><header class="level-head">'
        f'<p class="eyebrow">{esc(lv["prefix"])}</p><h2>{esc(lv["label"])}</h2>'
        f'<p class="level-meta">{esc(lv.get("note", ""))}</p></header>'
    )
    for n, d in enumerate(items, 1):
        w, h = png_size(D / "png" / f"{d['id']}.png")
        sheet_no = f'{lv["prefix"]}-{n:02d}'
        # The reviewed PNGs are part of the baseline; local interactive exports are not.
        link = f'diagrams/png/{d["id"]}.png'
        link_text = "Open full size"
        sheets.append(
            f'<article class="sheet" id="sheet-{esc(d["id"])}">'
            f'<div class="titleblock"><span class="sheet-no">{sheet_no}</span>'
            f'<h3>{code(d["title"])}</h3><span class="src">{esc(d["doc"])}</span></div>'
            f'<p class="caption">{code(d["caption"])}</p>'
            f'<figure class="plate"><a href="{link}" target="_blank" rel="noopener">'
            f'<img alt="{esc(sheet_no + " " + d["title"])}" width="{w // 2}" height="{h // 2}" '
            f'src="diagrams/png/{esc(d["id"])}.png"></a>'
            f'<figcaption><a href="{link}" target="_blank" rel="noopener">{link_text}</a>'
            f'<span>{esc(d["type"])} · {esc(d["id"])}</span></figcaption></figure></article>'
        )
    sheets.append("</section>")

decisions = "".join(
    f'<tr><td>{d["n"]}</td><td class="topic">{esc(d["topic"])}</td><td>{code(d["decision"])}</td></tr>'
    for d in m["decisions"]
)
phases = "".join(
    f'<tr><td class="phase">{esc(p["phase"])}</td><td class="phase">{esc(p["when"])}</td><td>{code(p["ships"])}</td></tr>'
    for p in m["phases"]
)
planes = "".join(f'<div><h3>{esc(p["title"])}</h3><p>{code(p["text"])}</p></div>' for p in m["planes"])

page = f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>aiops-platform C4 Set</title>
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=IBM+Plex+Sans+Condensed:wght@500;600;700&family=IBM+Plex+Sans:ital,wght@0,400;0,500;0,600;1,400&family=IBM+Plex+Mono:wght@400;500&display=swap">
<style>
/* Layout: an engineering drawing set. Sticky sheet index on the left, sheets with title blocks on the right, diagrams on white plates in both themes. */
:root {{
  --paper: #f3f4f1; --ink: #1c2126; --muted: #59626b; --rule: #d3d8d6; --signal: #a8460c;
  --plate: #ffffff; --wash: #e8ebe7;
  --display: "IBM Plex Sans Condensed", "Arial Narrow", "Helvetica Neue", Arial, sans-serif;
  --body: "IBM Plex Sans", "Helvetica Neue", Arial, sans-serif;
  --mono: "IBM Plex Mono", ui-monospace, "SFMono-Regular", Menlo, monospace;
}}
@media (prefers-color-scheme: dark) {{
  :root:not([data-theme="light"]) {{ --paper: #14181b; --ink: #e2e6e8; --muted: #9aa4ab; --rule: #2c3338; --signal: #f0904a; --plate: #ffffff; --wash: #1c2226; color-scheme: dark; }}
}}
:root[data-theme="dark"] {{ --paper: #14181b; --ink: #e2e6e8; --muted: #9aa4ab; --rule: #2c3338; --signal: #f0904a; --plate: #ffffff; --wash: #1c2226; color-scheme: dark; }}
* {{ box-sizing: border-box; }}
body {{ margin: 0; background: var(--paper); color: var(--ink); font: 16px/1.6 var(--body); }}
code {{ font: 0.86em var(--mono); background: var(--wash); padding: 0.05em 0.3em; border-radius: 3px; overflow-wrap: anywhere; }}
h1, h2, h3 {{ font-family: var(--display); text-wrap: balance; line-height: 1.15; margin: 0; }}
a {{ color: var(--signal); }}
a:focus-visible {{ outline: 2px solid var(--signal); outline-offset: 2px; }}
.wrap {{ max-width: 1400px; margin: 0 auto; padding-block: 40px 80px; padding-left: max(16px, env(safe-area-inset-left, 0px)); padding-right: max(16px, env(safe-area-inset-right, 0px)); }}
.eyebrow {{ font: 500 12px/1 var(--mono); letter-spacing: 0.08em; text-transform: uppercase; color: var(--signal); margin: 0 0 10px; }}
.masthead {{ border-bottom: 2px solid var(--ink); padding-bottom: 28px; display: grid; gap: 14px; }}
.masthead h1 {{ font-size: clamp(34px, 5vw, 56px); font-weight: 700; letter-spacing: -0.01em; }}
.lede {{ max-width: 68ch; font-size: 18px; margin: 0; }}
.stamp {{ display: flex; flex-wrap: wrap; gap: 8px 24px; font: 13px var(--mono); color: var(--muted); }}
.stamp b {{ color: var(--ink); font-weight: 500; }}
.layout {{ display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 48px; margin-top: 36px; }}
nav.index {{ position: sticky; top: 20px; align-self: start; }}
nav.index h2 {{ font: 500 13px var(--mono); letter-spacing: 0.08em; text-transform: uppercase; color: var(--muted); margin-bottom: 10px; }}
nav.index ol {{ list-style: none; margin: 0; padding: 0; display: grid; gap: 2px; }}
nav.index li {{ display: flex; justify-content: space-between; gap: 8px; border-top: 1px solid var(--rule); padding: 8px 0; }}
nav.index a {{ color: var(--ink); text-decoration: none; font-family: var(--display); font-weight: 600; font-size: 16px; }}
nav.index a:hover {{ color: var(--signal); }}
nav.index span {{ font: 13px var(--mono); color: var(--muted); font-variant-numeric: tabular-nums; }}
main {{ min-width: 0; display: grid; gap: 64px; }}
.brief {{ display: grid; gap: 28px; }}
.brief h2, .level-head h2 {{ font-size: clamp(26px, 3vw, 34px); font-weight: 600; }}
.headline {{ margin: 0; padding-left: 1.2em; max-width: 78ch; display: grid; gap: 6px; }}
.flow {{ display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border: 1px solid var(--rule); background: var(--plate); color: #1c2126; }}
.flow div {{ padding: 16px 18px; border-left: 1px solid #d3d8d6; min-width: 0; }}
.flow div:first-child {{ border-left: 0; }}
.flow h3 {{ font-size: 18px; margin-bottom: 6px; color: #1c2126; }}
.flow p {{ margin: 0; font-size: 14px; color: #3d454c; }}
.flow code {{ background: #eef0ec; color: #1c2126; }}
.tablebox {{ overflow-x: auto; border-top: 2px solid var(--ink); }}
table {{ border-collapse: collapse; width: 100%; font-size: 14.5px; }}
th, td {{ text-align: left; vertical-align: top; padding: 10px 12px 10px 0; border-bottom: 1px solid var(--rule); }}
th {{ font: 500 12px var(--mono); letter-spacing: 0.06em; text-transform: uppercase; color: var(--muted); }}
td:first-child {{ font: 13px var(--mono); color: var(--muted); font-variant-numeric: tabular-nums; width: 2.5em; }}
td.topic {{ font-family: var(--display); font-weight: 600; font-size: 15.5px; width: 13em; color: var(--ink); }}
td.phase {{ white-space: nowrap; font: 13px var(--mono); width: auto; color: var(--ink); }}
.level {{ display: grid; gap: 40px; scroll-margin-top: 24px; }}
.level-head {{ border-top: 2px solid var(--ink); padding-top: 18px; display: grid; gap: 6px; }}
.level-meta {{ margin: 0; color: var(--muted); font-size: 14px; max-width: 78ch; }}
.sheet {{ display: grid; gap: 12px; scroll-margin-top: 24px; }}
.titleblock {{ display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: baseline; gap: 6px 16px; border-bottom: 1px solid var(--rule); padding-bottom: 8px; }}
.sheet-no {{ font: 500 13px var(--mono); color: var(--paper); background: var(--ink); padding: 3px 8px; border-radius: 2px; font-variant-numeric: tabular-nums; }}
.titleblock h3 {{ font-size: 21px; font-weight: 600; }}
.src {{ font: 12.5px var(--mono); color: var(--muted); }}
.caption {{ max-width: 78ch; color: var(--muted); font-size: 15px; margin: 0; }}
.plate {{ margin: 0; background: var(--plate); border: 1px solid var(--rule); }}
.plate > a {{ display: block; padding: 8px; }}
.plate img {{ display: block; width: 100%; max-width: 100%; height: auto; margin: 0 auto; }}
figcaption {{ display: flex; flex-wrap: wrap; justify-content: space-between; gap: 8px 12px; border-top: 1px solid #e1e4e2; padding: 8px 12px; font: 12.5px var(--mono); color: #59626b; background: var(--plate); }}
figcaption a {{ color: #a8460c; }}
footer {{ border-top: 1px solid var(--rule); padding-top: 18px; color: var(--muted); font-size: 13px; max-width: 80ch; }}
@media (max-width: 900px) {{
  .layout {{ grid-template-columns: minmax(0, 1fr); gap: 28px; }}
  nav.index {{ position: static; }}
  nav.index ol {{ grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 0 16px; }}
  .flow {{ grid-template-columns: minmax(0, 1fr); }}
  .flow div {{ border-left: 0; border-top: 1px solid #d3d8d6; }}
  .flow div:first-child {{ border-top: 0; }}
  .titleblock {{ grid-template-columns: auto minmax(0, 1fr); }}
  .src {{ grid-column: 1 / -1; }}
}}
@media (prefers-reduced-motion: no-preference) {{ html {{ scroll-behavior: smooth; }} }}
</style>
</head>
<body>
<div class="wrap">
  <header class="masthead">
    <p class="eyebrow">Architecture drawing set · design phase · {esc(m["generated"])}</p>
    <h1>aiops-platform C4 set</h1>
    <p class="lede">{code(m["lede"])}</p>
    <div class="stamp"><span><b>{len(m["diagrams"])}</b> sheets</span><span>Source <b>docs/architecture/*.md</b></span><span>Canonical record <b>00-decisions.md</b></span><span>Drawn with <b>archify</b>, geometry-checked</span></div>
  </header>
  <div class="layout">
    <nav class="index" aria-label="Sheet index"><h2>Sheet index</h2><ol>{nav}</ol></nav>
    <main>
      <section class="brief" id="brief">
        <div><p class="eyebrow">Read this first</p><h2>{esc(m["brief_title"])}</h2></div>
        <div class="flow">{planes}</div>
        <ul class="headline">{"".join(f"<li>{code(x)}</li>" for x in m["headline"])}</ul>
        <div class="tablebox"><table><thead><tr><th>#</th><th>Topic</th><th>Decision</th></tr></thead><tbody>{decisions}</tbody></table></div>
        <div class="tablebox"><table><thead><tr><th>Phase</th><th>When</th><th>Ships</th></tr></thead><tbody>{phases}</tbody></table></div>
      </section>
      {"".join(sheets)}
      <footer>The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates. Each image is a screenshot of the diagram source under <code>docs/architecture/diagrams/src</code>; the Markdown documents win when they differ.</footer>
    </main>
  </div>
</div>
</body>
</html>
"""
(ARCH / "c4-set.html").write_text(page)
print(json.dumps({"sheets": len(m["diagrams"]), "bytes": len(page)}))
