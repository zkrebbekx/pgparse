"use strict";

const EXAMPLES = {
  cte: `-- Top spenders this month, ranked
WITH big_spenders AS (
  SELECT user_id, sum(amount) AS spent
  FROM orders
  WHERE created_at > now() - interval '30 days'
  GROUP BY user_id
  HAVING sum(amount) > 1000
)
SELECT u.name,
       b.spent,
       rank() OVER (ORDER BY b.spent DESC) AS rank
FROM big_spenders b
JOIN users u ON u.id = b.user_id
ORDER BY rank
LIMIT 10;`,
  dml: `-- A data-modifying CTE: archive then delete in one statement
WITH moved AS (
  DELETE FROM events
  WHERE created_at < now() - interval '1 year'
  RETURNING *
)
INSERT INTO events_archive
SELECT * FROM moved;`,
  upsert: `-- Upsert with a conditional update
INSERT INTO inventory (sku, qty)
VALUES ('A-100', 5), ('B-200', 12)
ON CONFLICT (sku)
DO UPDATE SET qty = inventory.qty + excluded.qty
WHERE inventory.qty < 1000
RETURNING sku, qty;`,
  ddl: `-- Schema definition
CREATE TABLE IF NOT EXISTS accounts (
  id        bigint PRIMARY KEY,
  email     text NOT NULL UNIQUE,
  org_id    int REFERENCES orgs (id),
  balance   numeric(12,2) DEFAULT 0,
  status    text DEFAULT 'active',
  CHECK (balance >= 0)
);`,
  json: `-- JSON, arrays and quantified comparisons
SELECT id,
       data -> 'profile' ->> 'name'  AS name,
       tags @> ARRAY['vip']          AS is_vip
FROM users
WHERE settings ? 'beta'
  AND status = ANY (ARRAY['active', 'trial'])
ORDER BY id;`,
  gnarly: `SELECT c.name, count(*) FILTER (WHERE o.total > 100) AS big_orders
FROM customers c
LEFT JOIN LATERAL (
  SELECT * FROM orders o WHERE o.customer_id = c.id ORDER BY o.total DESC LIMIT 5
) o ON true
GROUP BY ROLLUP (c.name)
ORDER BY 2 DESC NULLS LAST;`,
};
const DICE = ["cte", "dml", "upsert", "ddl", "json", "gnarly"];

const $ = (id) => document.getElementById(id);
const ed = $("sql");
let ready = false, firstOk = false, benchTimer = null;

// wasm tells us when it is live.
window.onPgparseReady = () => {
  ready = true;
  $("loader").classList.add("gone");
  run();
};

// Boot the wasm module.
(function boot() {
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("pgparse.wasm"), go.importObject)
    .then((r) => go.run(r.instance))
    .catch((e) => {
      $("loader").innerHTML = "<span>failed to load wasm: " + e + "</span>";
    });
})();

// --- examples toolbar ---
$("examples").addEventListener("click", (e) => {
  const b = e.target.closest("button");
  if (!b) return;
  let key = b.dataset.ex;
  if (key === "dice") key = DICE[(Math.random() * DICE.length) | 0];
  ed.value = EXAMPLES[key];
  run();
  ed.focus();
});

// --- tabs ---
document.querySelectorAll(".tab").forEach((t) =>
  t.addEventListener("click", () => {
    document.querySelectorAll(".tab").forEach((x) => x.classList.remove("active"));
    document.querySelectorAll(".tabpane").forEach((x) => x.classList.remove("active"));
    t.classList.add("active");
    $("tab-" + t.dataset.tab).classList.add("active");
  })
);

// --- live parsing ---
let deb = null;
ed.addEventListener("input", () => {
  clearTimeout(deb);
  deb = setTimeout(run, 110);
});

function run() {
  if (!ready) return;
  const sql = ed.value.trim();
  if (!sql) {
    setVerdict("idle", "🐘", "Type some SQL…", "parsed live as you type");
    $("tab-tree").innerHTML = "";
    $("tab-sql").textContent = "";
    $("err").hidden = true;
    return;
  }
  const res = pgparseAnalyze(sql);
  if (!res.ok) {
    showError(sql, res);
    return;
  }
  $("err").hidden = true;

  // verdict = the most consequential statement in the script
  // (DDL > write > utility > transaction > read).
  const SEV = { 2: 4, 1: 3, 3: 2, 4: 1, 0: 0 };
  const CSS = { 0: "read", 1: "write", 2: "ddl", 3: "util", 4: "util" };
  let top = res.statements[0];
  for (const s of res.statements) if (SEV[s.class] > SEV[top.class]) top = s;
  const n = res.count;
  setVerdict(CSS[top.class], top.emoji, top.label,
    n + " statement" + (n === 1 ? "" : "s") + " · parsed cleanly ✓", true);

  renderTree(res.statements);
  $("tab-sql").textContent = res.statements.map((s) => formatSQL(s.deparsed)).join(";\n\n") + ";";

  if (!firstOk) { firstOk = true; confettiBurst(); }
  scheduleBench(sql);
}

function showError(sql, res) {
  setVerdict("write", "🤔", "Not valid (yet)", "fix the syntax and it'll light up", true);
  let msg = res.error || "syntax error";
  if (typeof res.offset === "number") {
    const upto = sql.slice(0, res.offset);
    const line = upto.split("\n").length;
    const col = res.offset - upto.lastIndexOf("\n");
    msg = `line ${line}, col ${col}: ${res.message || res.error}\n` +
          caret(sql, res.offset);
  }
  const el = $("err");
  el.textContent = msg;
  el.hidden = false;
}

function caret(sql, off) {
  const start = sql.lastIndexOf("\n", off - 1) + 1;
  let end = sql.indexOf("\n", off);
  if (end < 0) end = sql.length;
  const lineText = sql.slice(start, end);
  return lineText + "\n" + " ".repeat(Math.max(0, off - start)) + "▲";
}

function setVerdict(cls, emoji, label, sub, pop) {
  const v = $("verdict");
  v.className = "verdict " + cls + (pop ? " pop" : "");
  $("vemoji").textContent = emoji;
  $("vlabel").textContent = label;
  $("vsub").textContent = sub;
  if (pop) setTimeout(() => v.classList.remove("pop"), 500);
}

// --- AST tree ---
function renderTree(stmts) {
  const root = document.createElement("div");
  stmts.forEach((s, i) => {
    if (stmts.length > 1) {
      const sep = document.createElement("div");
      sep.className = "stmt-sep";
      sep.textContent = `▸ statement ${i + 1} — ${s.emoji} ${s.label}`;
      root.appendChild(sep);
    }
    root.appendChild(buildNode(s.ast, null, i < 1));
  });
  const host = $("tab-tree");
  host.innerHTML = "";
  host.appendChild(root);
}

function buildNode(value, fieldName, open) {
  const node = document.createElement("div");
  node.className = "node";
  const row = document.createElement("div");
  row.className = "row";

  if (value && typeof value === "object" && !Array.isArray(value)) {
    node.classList.add("collapsible");
    if (!open) node.classList.add("closed");
    const tw = el("span", "twist", "▾");
    row.appendChild(tw);
    if (fieldName) row.appendChild(el("span", "fname", fieldName + ":"));
    row.appendChild(el("span", "kind", value._kind || "{}"));
    node.appendChild(row);
    const kids = el("div", "children");
    for (const k of Object.keys(value)) {
      if (k === "_kind") continue;
      kids.appendChild(buildNode(value[k], k, false));
    }
    node.appendChild(kids);
    row.addEventListener("click", () => node.classList.toggle("closed"));
  } else if (Array.isArray(value)) {
    node.classList.add("collapsible");
    if (!open) node.classList.add("closed");
    row.appendChild(el("span", "twist", "▾"));
    if (fieldName) row.appendChild(el("span", "fname", fieldName));
    row.appendChild(el("span", "val", "[" + value.length + "]"));
    node.appendChild(row);
    const kids = el("div", "children");
    value.forEach((v, i) => kids.appendChild(buildNode(v, String(i), false)));
    node.appendChild(kids);
    row.addEventListener("click", () => node.classList.toggle("closed"));
  } else {
    row.appendChild(el("span", "twist", " "));
    if (fieldName) row.appendChild(el("span", "fname", fieldName + ":"));
    const cls = typeof value === "string" ? "val str" : (typeof value === "boolean" ? "val bool" : "val");
    const text = typeof value === "string" ? JSON.stringify(value) : String(value);
    row.appendChild(el("span", cls, text));
    node.appendChild(row);
  }
  return node;
}

// --- light pretty-printer for the deparsed (canonical, single-line) SQL ---
// Breaks before top-level clause keywords, tracking paren depth and string
// literals so it never breaks inside a subquery or a quoted value.
const BREAK_KW = ["UNION ALL", "UNION", "INTERSECT", "EXCEPT", "FROM", "WHERE",
  "GROUP BY", "HAVING", "WINDOW", "ORDER BY", "LIMIT", "OFFSET", "RETURNING",
  "ON CONFLICT", "VALUES", "SET"];
const JOIN_KW = ["LEFT JOIN", "RIGHT JOIN", "FULL JOIN", "CROSS JOIN",
  "INNER JOIN", "NATURAL JOIN", "JOIN"];

function formatSQL(sql) {
  let out = "", depth = 0, inStr = false, i = 0;
  const n = sql.length;
  const matchAt = (list) => {
    for (const kw of list) {
      if (sql.startsWith(kw, i)) {
        const after = sql[i + kw.length];
        if (after === undefined || after === " " || after === "(") return kw;
      }
    }
    return null;
  };
  while (i < n) {
    const c = sql[i];
    if (inStr) {
      out += c;
      if (c === "'") {
        if (sql[i + 1] === "'") { out += "'"; i += 2; continue; }
        inStr = false;
      }
      i++; continue;
    }
    if (c === "'") { inStr = true; out += c; i++; continue; }
    if (c === "(") { depth++; out += c; i++; continue; }
    if (c === ")") { depth = Math.max(0, depth - 1); out += c; i++; continue; }
    if (depth === 0 && (out === "" || out.endsWith(" "))) {
      const jk = matchAt(JOIN_KW);
      if (jk && out !== "") { out = out.replace(/ $/, "") + "\n  " + jk; i += jk.length; continue; }
      const bk = matchAt(BREAK_KW);
      if (bk && out !== "") { out = out.replace(/ $/, "") + "\n" + bk; i += bk.length; continue; }
    }
    out += c; i++;
  }
  return out;
}

function el(tag, cls, text) {
  const e = document.createElement(tag);
  e.className = cls;
  if (text != null) e.textContent = text;
  return e;
}

// --- speed badge (real in-wasm benchmark, idle-throttled) ---
function scheduleBench(sql) {
  clearTimeout(benchTimer);
  benchTimer = setTimeout(() => {
    const ns = pgparseBench(sql, 3000);
    if (!ns) return;
    const us = ns / 1000;
    const b = $("speed");
    b.textContent = "⚡ " + (us < 10 ? us.toFixed(2) : us.toFixed(1)) + " µs / parse";
    b.classList.add("flash");
    setTimeout(() => b.classList.remove("flash"), 160);
  }, 350);
}

// --- confetti (tiny, vanilla) ---
function confettiBurst() {
  const cv = $("confetti"), ctx = cv.getContext("2d");
  cv.width = innerWidth; cv.height = innerHeight;
  const colors = ["#7c5cff", "#3ddc97", "#ffb02e", "#ff5d73", "#9aa7ff", "#ff8ad4"];
  const N = 140, parts = [];
  for (let i = 0; i < N; i++) {
    parts.push({
      x: innerWidth / 2, y: 120,
      vx: (Math.random() - 0.5) * 13, vy: Math.random() * -12 - 3,
      s: 5 + Math.random() * 7, c: colors[(Math.random() * colors.length) | 0],
      rot: Math.random() * 6.28, vr: (Math.random() - 0.5) * 0.4, life: 0,
    });
  }
  let raf;
  (function frame() {
    ctx.clearRect(0, 0, cv.width, cv.height);
    let alive = false;
    for (const p of parts) {
      p.vy += 0.32; p.x += p.vx; p.y += p.vy; p.rot += p.vr; p.life++;
      if (p.y < cv.height + 30) alive = true;
      ctx.save(); ctx.translate(p.x, p.y); ctx.rotate(p.rot);
      ctx.fillStyle = p.c; ctx.globalAlpha = Math.max(0, 1 - p.life / 120);
      ctx.fillRect(-p.s / 2, -p.s / 2, p.s, p.s * 0.6); ctx.restore();
    }
    if (alive) raf = requestAnimationFrame(frame);
    else ctx.clearRect(0, 0, cv.width, cv.height);
  })();
}

// seed with a fun default
ed.value = EXAMPLES.cte;
