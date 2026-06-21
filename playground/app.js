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
  upsert: `INSERT INTO inventory (sku, qty)
VALUES ('A-100', 5), ('B-200', 12)
ON CONFLICT (sku)
DO UPDATE SET qty = inventory.qty + excluded.qty
WHERE inventory.qty < 1000
RETURNING sku, qty;`,
  ddl: `CREATE TABLE IF NOT EXISTS accounts (
  id        bigint PRIMARY KEY,
  email     text NOT NULL UNIQUE,
  org_id    int REFERENCES orgs (id),
  balance   numeric(12,2) DEFAULT 0,
  status    text DEFAULT 'active',
  CHECK (balance >= 0)
);`,
  json: `SELECT id,
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
  recursive: `-- Recursive CTE with SEARCH (preserved through Deparse, v0.3.2)
WITH RECURSIVE tree AS (
  SELECT id, parent_id, name, 1 AS depth
  FROM nodes
  WHERE parent_id IS NULL
  UNION ALL
  SELECT n.id, n.parent_id, n.name, t.depth + 1
  FROM nodes n
  JOIN tree t ON n.parent_id = t.id
) SEARCH DEPTH FIRST BY name SET ord
SELECT * FROM tree ORDER BY ord;`,
};
const DICE = Object.keys(EXAMPLES);

const $ = (id) => document.getElementById(id);
const ed = $("sql");
let ready = false, benchTimer = null;

// --- theme (default dark, persisted) ---
const SUN = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M2 12h2M20 12h2M5 5l1.5 1.5M17.5 17.5L19 19M19 5l-1.5 1.5M6.5 17.5L5 19"/></svg>';
const MOON = '<svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/></svg>';
function applyTheme(t) {
  document.documentElement.dataset.theme = t;
  $("theme").innerHTML = t === "dark" ? MOON : SUN;
}
applyTheme(localStorage.getItem("pgparse-theme") || "dark");
$("theme").addEventListener("click", () => {
  const t = document.documentElement.dataset.theme === "dark" ? "light" : "dark";
  localStorage.setItem("pgparse-theme", t);
  applyTheme(t);
});

// --- wasm boot ---
window.onPgparseReady = () => {
  ready = true;
  $("loader").classList.add("gone");
  const mb = (typeof pgparseMaxInput === "number") ? Math.round(pgparseMaxInput / (1 << 20)) : 16;
  $("limit").textContent = mb + " MB";
  run();
};
(function boot() {
  const go = new Go();
  // no-cache forces revalidation so a redeployed wasm is picked up (the
  // filename is stable across deploys).
  WebAssembly.instantiateStreaming(fetch("pgparse.wasm?v=2", { cache: "no-cache" }), go.importObject)
    .then((r) => go.run(r.instance))
    .catch((e) => { $("loader").innerHTML = "<span>failed to load wasm: " + e + "</span>"; });
})();

// --- toolbar ---
$("examples").addEventListener("click", pick);
$("dice").addEventListener("click", pick);
function pick(e) {
  const b = e.target.closest("button");
  if (!b) return;
  let key = b.dataset.ex;
  if (key === "dice") key = DICE[(Math.random() * DICE.length) | 0];
  ed.value = EXAMPLES[key];
  run();
  ed.focus();
}

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
ed.addEventListener("input", () => { clearTimeout(deb); deb = setTimeout(run, 110); });

const CSS = { 0: "read", 1: "write", 2: "ddl", 3: "util", 4: "txn" };
const SEV = { 2: 4, 1: 3, 3: 2, 4: 1, 0: 0 };

function run() {
  if (!ready) return;
  const sql = ed.value.trim();
  if (!sql) {
    setVerdict("util", "—");
    $("meta").textContent = "type some SQL";
    $("speed").textContent = "— µs";
    $("tab-tree").innerHTML = ""; $("tab-sql").textContent = ""; $("err").hidden = true;
    return;
  }
  const res = pgparseAnalyze(sql);
  if (!res.ok) { showError(sql, res); return; }
  $("err").hidden = true;

  let top = res.statements[0];
  for (const s of res.statements) if (SEV[s.class] > SEV[top.class]) top = s;
  setVerdict(CSS[top.class], top.label);
  const n = res.count;
  $("meta").textContent = n + " statement" + (n === 1 ? "" : "s");

  renderTree(res.statements);
  $("tab-sql").textContent = res.statements.map((s) => formatSQL(s.deparsed)).join(";\n\n") + ";";
  scheduleBench(sql);
}

function showError(sql, res) {
  setVerdict("bad", "Invalid SQL");
  $("meta").textContent = ""; $("speed").textContent = "— µs";
  let msg = res.error || "syntax error";
  // line/col come straight from pgparse's SyntaxError.
  if (res.line) {
    msg = `line ${res.line}, column ${res.col}: ${res.message || res.error}`;
    if (typeof res.offset === "number") msg += "\n" + caret(sql, res.offset);
  }
  const el = $("err"); el.textContent = msg; el.hidden = false;
}

function caret(sql, off) {
  const start = sql.lastIndexOf("\n", off - 1) + 1;
  let end = sql.indexOf("\n", off); if (end < 0) end = sql.length;
  return sql.slice(start, end) + "\n" + " ".repeat(Math.max(0, off - start)) + "^";
}

function setVerdict(cls, label) {
  $("verdict").className = "pill " + cls;
  $("vlabel").textContent = label;
}

// --- AST tree ---
function renderTree(stmts) {
  const root = document.createElement("div");
  stmts.forEach((s, i) => {
    if (stmts.length > 1) {
      const sep = el("div", "stmt-sep", `statement ${i + 1} · ${s.label}`);
      root.appendChild(sep);
    }
    root.appendChild(buildNode(s.ast, null, true));
  });
  const host = $("tab-tree"); host.innerHTML = ""; host.appendChild(root);
}

function buildNode(value, fieldName, open) {
  const node = el("div", "node");
  const row = el("div", "row");
  const isObj = value && typeof value === "object" && !Array.isArray(value);
  const isArr = Array.isArray(value);

  if (isObj || isArr) {
    node.classList.add("collapsible");
    if (!open) node.classList.add("closed");
    row.appendChild(el("span", "twist", "▾"));
    if (fieldName) row.appendChild(el("span", "fname", fieldName + (isObj ? ":" : "")));
    row.appendChild(isObj ? el("span", "kind", value._kind || "{}")
                          : el("span", "count", "[" + value.length + "]"));
    node.appendChild(row);
    const kids = el("div", "children");
    if (isObj) for (const k of Object.keys(value)) { if (k !== "_kind") kids.appendChild(buildNode(value[k], k, false)); }
    else value.forEach((v, i) => kids.appendChild(buildNode(v, String(i), false)));
    node.appendChild(kids);
    row.addEventListener("click", () => node.classList.toggle("closed"));
  } else {
    row.appendChild(el("span", "twist", ""));
    if (fieldName) row.appendChild(el("span", "fname", fieldName + ":"));
    const cls = typeof value === "string" ? "val str" : (typeof value === "boolean" ? "val bool" : "val");
    row.appendChild(el("span", cls, typeof value === "string" ? JSON.stringify(value) : String(value)));
    node.appendChild(row);
  }
  return node;
}

function el(tag, cls, text) {
  const e = document.createElement(tag); e.className = cls;
  if (text != null) e.textContent = text; return e;
}

// --- pretty-printer ---
// Stack-based, quote-aware. Subqueries (CTE bodies, IN/EXISTS) and CREATE
// column lists are broken onto indented lines; clause keywords break at the
// statement/subquery level (but not inside ordinary expression parentheses, so
// e.g. OVER (ORDER BY ...) stays inline).
const BREAK_KW = ["UNION ALL", "UNION", "INTERSECT", "EXCEPT", "FROM", "WHERE",
  "GROUP BY", "HAVING", "WINDOW", "ORDER BY", "LIMIT", "OFFSET", "RETURNING",
  "ON CONFLICT", "SET"];
const JOIN_KW = ["LEFT JOIN", "RIGHT JOIN", "FULL JOIN", "CROSS JOIN",
  "INNER JOIN", "NATURAL JOIN", "JOIN"];

function formatSQL(sql) {
  const isCreate = /^\s*CREATE\b/i.test(sql);
  const stack = []; // 'sub' | 'list' | 'expr'
  const indentN = () => stack.reduce((n, s) => n + (s === "expr" ? 0 : 1), 0);
  const IND = (n) => "  ".repeat(n);
  const atStmt = () => stack.length === 0 || stack[stack.length - 1] === "sub";
  const atList = () => stack.length > 0 && stack[stack.length - 1] === "list";
  const upWord = (pos) => { let j = pos; while (sql[j] === " ") j++; return sql.slice(j, j + 7).toUpperCase(); };
  const trim = () => { out = out.replace(/[ \t]+$/, ""); };
  const matchAt = (list) => {
    for (const kw of list) if (sql.startsWith(kw, i)) {
      const a = sql[i + kw.length];
      if (a === undefined || a === " " || a === "(" || a === ")") return kw;
    }
    return null;
  };
  let out = "", i = 0, inStr = false;
  while (i < sql.length) {
    const c = sql[i];
    if (inStr) { out += c; if (c === "'") { if (sql[i + 1] === "'") { out += "'"; i += 2; continue; } inStr = false; } i++; continue; }
    if (c === "'") { inStr = true; out += c; i++; continue; }

    if (c === "(") {
      const w = upWord(i + 1);
      if (/^(SELECT|WITH|VALUES|INSERT|UPDATE|DELETE)\b/.test(w)) {
        stack.push("sub"); trim(); out += " (\n" + IND(indentN());
        i++; while (sql[i] === " ") i++; continue;
      }
      const prev = out.replace(/\s+$/, "");
      // CREATE TABLE column list, multi-column UPDATE SET (cols), and its value
      // list ") = (" all break one item per line.
      if ((isCreate && stack.length === 0) || /\bSET$/.test(prev) || /\)\s*=$/.test(prev)) {
        stack.push("list"); trim(); out += " (\n" + IND(indentN());
        i++; while (sql[i] === " ") i++; continue;
      }
      stack.push("expr"); out += "("; i++; continue;
    }
    if (c === ")") {
      const t = stack.pop();
      if (t === "sub" || t === "list") { trim(); out += "\n" + IND(indentN()) + ")"; }
      else out += ")";
      i++; continue;
    }
    if (c === "," && atList()) {
      trim(); out += ",\n" + IND(indentN());
      i++; while (sql[i] === " ") i++; continue;
    }
    if (atStmt() && /\)\s$/.test(out)) {
      const sk = matchAt(["SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "VALUES"]);
      if (sk) { trim(); out += "\n" + IND(indentN()) + sk; i += sk.length; continue; }
    }
    if (atStmt() && (out === "" || /[ \n(]$/.test(out))) {
      const jk = matchAt(JOIN_KW);
      if (jk && !out.endsWith("(")) { trim(); out += "\n" + IND(indentN()) + "  " + jk; i += jk.length; continue; }
      const bk = matchAt(BREAK_KW);
      // keep "DELETE FROM" together rather than breaking before FROM
      const afterDelete = bk === "FROM" && /\bDELETE$/.test(out.replace(/\s+$/, ""));
      if (bk && !afterDelete && out.trim() !== "" && !out.endsWith("(")) { trim(); out += "\n" + IND(indentN()) + bk; i += bk.length; continue; }
    }
    out += c; i++;
  }
  return out;
}

// --- live parse-time badge (real in-wasm benchmark) ---
function scheduleBench(sql) {
  clearTimeout(benchTimer);
  benchTimer = setTimeout(() => {
    const ns = pgparseBench(sql, 3000);
    if (!ns) return;
    const us = ns / 1000;
    const b = $("speed");
    b.textContent = (us < 10 ? us.toFixed(2) : us.toFixed(1)) + " µs";
    b.classList.add("flash"); setTimeout(() => b.classList.remove("flash"), 150);
  }, 320);
}

ed.value = EXAMPLES.cte;
