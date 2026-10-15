const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const html = fs.readFileSync(path.join(__dirname, "web", "index.html"), "utf8");
const script = html.match(/<script>([\s\S]*?)<\/script>/)?.[1];
assert.ok(script, "embedded UI script exists");

class Element {
  constructor(tag) {
    this.tagName = tag;
    this.children = [];
    this.listeners = {};
    this.attributes = {};
    this.style = {};
    this.value = "";
    this.innerHTML = "";
    this.textContent = "";
  }
  append(...children) { this.children.push(...children); }
  appendChild(child) { this.children.push(child); return child; }
  replaceChildren(...children) { this.children = children; }
  addEventListener(type, listener) { this.listeners[type] = listener; }
  setAttribute(key, value) { this.attributes[key] = value; }
  remove() {}
}

async function setup() {
  const elements = new Map();
  function get(id) {
    if (!elements.has(id)) elements.set(id, new Element(id));
    return elements.get(id);
  }
  const document = {
    documentElement: {},
    getElementById: get,
    querySelector: get,
    createElement: tag => new Element(tag),
  };
  const context = vm.createContext({
    document,
    navigator: { language: "en", languages: ["en"] },
    localStorage: { getItem: () => "en", setItem: () => {} },
    window: { confirm: () => false },
    fetch: async url => ({ ok: true, json: async () => url === "/api/families" ? (context.fixture || []) : [] }),
    setInterval: () => 0,
    setTimeout: () => 0,
    clearTimeout: () => {},
  });
  vm.runInContext(script, context);
  await vm.runInContext("refresh()", context);
  return { context, get };
}

function all(root, className) {
  return [
    ...(root.className?.split(" ").includes(className) ? [root] : []),
    ...root.children.flatMap(child => all(child, className)),
  ];
}

function family(id, dlcCount, unknownCount = 0) {
  return {
    id,
    title: "Example " + id,
    packageCount: dlcCount + unknownCount + 2,
    totalSize: 1000,
    packages: [
      { id: id + "-base", name: "Base game", packageType: "game", size: 200, metadataParsed: true },
      { id: id + "-patch", name: "Patch", packageType: "patch", size: 100, metadataParsed: true },
      ...Array.from({ length: dlcCount }, (_, i) => ({
        id: id + "-dlc-" + i,
        name: "DLC Item " + i,
        packageType: "dlc",
        size: 10,
        metadataParsed: true,
      })),
      ...Array.from({ length: unknownCount }, (_, i) => ({
        id: id + "-unknown-" + i,
        name: "Unknown Item " + i,
        packageType: "unknown",
        size: 10,
        metadataParsed: false,
      })),
    ],
  };
}

test("DLC groups start collapsed independently and persist across rerenders", async () => {
  const { context, get } = await setup();
  context.fixture = [family("A", 5), family("B", 3)];
  vm.runInContext("state.families=fixture;render()", context);
  assert.equal(all(get("packageGrid"), "package-row").length, 4);
  let toggles = all(get("packageGrid"), "dlc-toggle");
  assert.equal(toggles.length, 2);
  assert.equal(toggles[0].attributes["aria-expanded"], "false");
  assert.equal(toggles[0].textContent, "Show DLC (5)");

  toggles[0].listeners.click();
  assert.equal(all(get("packageGrid"), "package-row").length, 9);
  toggles = all(get("packageGrid"), "dlc-toggle");
  assert.equal(toggles[0].attributes["aria-expanded"], "true");
  assert.equal(toggles[1].attributes["aria-expanded"], "false");

  await vm.runInContext("refresh()", context);
  assert.equal(all(get("packageGrid"), "package-row").length, 9);
  all(get("packageGrid"), "dlc-toggle")[0].listeners.click();
  assert.equal(all(get("packageGrid"), "package-row").length, 4);
});

test("a DLC search is not hidden by collapsed state", async () => {
  const { context, get } = await setup();
  context.fixture = [family("A", 5)];
  vm.runInContext('state.families=fixture;state.query="DLC Item 3";render()', context);
  const rows = all(get("packageGrid"), "package-row");
  assert.equal(rows.length, 1);
  assert.equal(all(get("packageGrid"), "dlc-toggle").length, 0);
  assert.equal(all(rows[0], "package-name")[0].textContent, "DLC Item 3");
  vm.runInContext('state.query="";render()', context);
  assert.equal(all(get("packageGrid"), "package-row").length, 2);
});

test("Unknown packages share the default-collapsed DLC section without being relabeled", async () => {
  const { context, get } = await setup();
  context.fixture = [family("A", 2, 3), family("B", 1, 1)];
  vm.runInContext("state.families=fixture;render()", context);

  let toggles = all(get("packageGrid"), "dlc-toggle");
  assert.equal(toggles.length, 2);
  assert.equal(toggles[0].textContent, "Show DLC (5)");
  assert.equal(toggles[1].textContent, "Show DLC (2)");
  assert.equal(all(get("packageGrid"), "package-row").length, 4);

  toggles[0].listeners.click();
  let rows = all(get("packageGrid"), "package-row");
  assert.equal(rows.length, 9);
  const chips = rows.flatMap(row => all(row, "chip").map(chip => chip.textContent));
  assert.equal(chips.filter(label => label === "Unknown").length, 3);
  assert.equal(chips.filter(label => label === "DLC").length, 2);
  assert.equal(context.fixture[0].packages[4].packageType, "unknown");

  await vm.runInContext("refresh()", context);
  toggles = all(get("packageGrid"), "dlc-toggle");
  assert.equal(toggles[0].attributes["aria-expanded"], "true");
  assert.equal(toggles[1].attributes["aria-expanded"], "false");

  vm.runInContext('state.lang="zh";render()', context);
  assert.equal(all(get("packageGrid"), "dlc-toggle")[0].textContent, "收起 DLC (5)");
  assert.equal(all(get("packageGrid"), "dlc-toggle")[1].textContent, "展开 DLC (2)");
  rows = all(get("packageGrid"), "package-row");
  assert.equal(rows.flatMap(row => all(row, "chip")).filter(chip => chip.textContent === "未知").length, 3);
});

test("Unknown-only groups default to folded and search reveals unknown matches", async () => {
  const { context, get } = await setup();
  context.fixture = [{
    id: "unknown-only",
    title: "Unclassified files",
    packageCount: 2,
    totalSize: 20,
    packages: [
      { id: "unknown-1", name: "Hidden Alpha", packageType: "unknown", size: 10 },
      { id: "unknown-2", name: "Hidden Beta", size: 10 },
    ],
  }];
  vm.runInContext("state.families=fixture;render()", context);
  assert.equal(all(get("packageGrid"), "package-row").length, 0);
  assert.equal(all(get("packageGrid"), "dlc-toggle")[0].textContent, "Show DLC (2)");

  vm.runInContext('state.query="Hidden Beta";render()', context);
  const rows = all(get("packageGrid"), "package-row");
  assert.equal(rows.length, 1);
  assert.equal(all(get("packageGrid"), "dlc-toggle").length, 0);
  assert.equal(all(rows[0], "package-name")[0].textContent, "Hidden Beta");
  assert.equal(all(rows[0], "chip")[0].textContent, "Unknown");
  vm.runInContext('state.query="";render()', context);
  assert.equal(all(get("packageGrid"), "package-row").length, 0);
  assert.equal(all(get("packageGrid"), "dlc-toggle")[0].attributes["aria-expanded"], "false");
});

test("PS5 is the only connectivity label with green/red state classes", async () => {
  const { context } = await setup();
  const inspect = snapshot => {
    context.fixture = snapshot;
    return vm.runInContext("discoveryStatus(fixture)", context);
  };
  const online = inspect({ listening: true, consoles: [{ configured: true, online: true }] });
  assert.equal(online.kind, "online");
  assert.match(online.html, /PS5 .*online/);
  assert.doesNotMatch(online.html, /NAS/);

  const offline = inspect({ listening: true, consoles: [{ configured: true, online: false }, { configured: false, online: true }] });
  assert.equal(offline.kind, "offline");
  assert.match(offline.html, /PS5 .*offline/);
  assert.equal(inspect(null).kind, "offline");
  assert.match(html, /\.status-text\.online,\.status-word\.online\{color:#63d297\}/);
  assert.match(html, /\.status-text\.offline,\.status-text\.error,\.status-word\.offline\{color:#ff6b75\}/);
  assert.match(html, /\.status-text\{font-weight:800\}/);
});

test("Chinese DLC and PS5 status labels are localized", async () => {
  const { context, get } = await setup();
  context.fixture = [family("A", 2)];
  vm.runInContext('state.lang="zh";state.families=fixture;render()', context);
  assert.equal(all(get("packageGrid"), "dlc-toggle")[0].textContent, "展开 DLC (2)");
  all(get("packageGrid"), "dlc-toggle")[0].listeners.click();
  assert.equal(all(get("packageGrid"), "dlc-toggle")[0].textContent, "收起 DLC (2)");
  context.fixture = { consoles: [{ configured: true, online: true }] };
  assert.match(vm.runInContext("discoveryStatus(fixture).html", context), /PS5 .*在线/);
  assert.match(vm.runInContext("discoveryStatus(null).html", context), /PS5 .*离线/);
});
