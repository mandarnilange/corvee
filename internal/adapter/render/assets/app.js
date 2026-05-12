// corvee dashboard client. No third-party libraries.
//
//   * Theme cycle: auto → light → dark → auto, persisted to
//     localStorage under "corvee:theme".
//   * Right-side drawer for item details. Card / tree-node clicks
//     are intercepted; modifier-click (cmd/ctrl/middle) still
//     opens the standalone page in a new tab. URL hash #item=ID
//     keeps deep-linking working.
//   * Board filters: status / priority / kind / assignee chips and
//     a text search. Filter state lives in the URL hash so it
//     survives reload and is shareable.
//   * Tree expand/collapse: [e] / [c].
(function () {
  "use strict";

  // -------------------------------------------------------------------------
  // Theme
  // -------------------------------------------------------------------------
  var STORAGE_KEY = "corvee:theme";
  var THEME_MODES = ["auto", "light", "dark"];

  function applyTheme(mode) {
    document.documentElement.setAttribute("data-theme", mode);
    var btn = document.querySelector("[data-theme-toggle]");
    if (btn) btn.setAttribute("data-theme-mode", mode);
  }

  function initTheme() {
    var stored;
    try { stored = localStorage.getItem(STORAGE_KEY); } catch (e) { stored = null; }
    if (stored && THEME_MODES.indexOf(stored) >= 0) {
      applyTheme(stored);
      return;
    }
    var current = document.documentElement.getAttribute("data-theme") || "auto";
    if (THEME_MODES.indexOf(current) < 0) current = "auto";
    applyTheme(current);
  }

  function cycleTheme() {
    var current = document.documentElement.getAttribute("data-theme") || "auto";
    var idx = THEME_MODES.indexOf(current);
    var next = THEME_MODES[(idx + 1) % THEME_MODES.length];
    try { localStorage.setItem(STORAGE_KEY, next); } catch (e) { /* private mode etc. */ }
    applyTheme(next);
  }

  // -------------------------------------------------------------------------
  // Drawer
  // -------------------------------------------------------------------------
  function getDrawer() { return document.querySelector("[data-drawer]"); }
  function getDrawerBody() { return document.querySelector("[data-drawer-body]"); }
  function getDrawerBackdrop() { return document.querySelector("[data-drawer-backdrop]"); }

  function clearChildren(node) {
    while (node && node.firstChild) node.removeChild(node.firstChild);
  }

  function openItem(id, pushHash) {
    var tmpl = document.getElementById("item-" + id);
    if (!tmpl) return false;
    var body = getDrawerBody();
    var drawer = getDrawer();
    var backdrop = getDrawerBackdrop();
    if (!body || !drawer || !backdrop) return false;
    clearChildren(body);
    body.appendChild(tmpl.content ? tmpl.content.cloneNode(true) : cloneTemplateFallback(tmpl));
    drawer.setAttribute("data-visible", "true");
    drawer.hidden = false;
    backdrop.setAttribute("data-visible", "true");
    backdrop.hidden = false;
    var link = document.querySelector("[data-drawer-permalink]");
    if (link) link.setAttribute("href", tmpl.getAttribute("data-permalink") || "#");
    body.scrollTop = 0;
    if (pushHash !== false) {
      // Use replaceState so the back button closes the drawer rather
      // than walking through every previous selection.
      try {
        if (history.replaceState) history.replaceState(null, "", "#item=" + id);
        else location.hash = "item=" + id;
      } catch (e) { location.hash = "item=" + id; }
    }
    return true;
  }

  function cloneTemplateFallback(tmpl) {
    var frag = document.createDocumentFragment();
    var children = tmpl.childNodes;
    for (var i = 0; i < children.length; i++) frag.appendChild(children[i].cloneNode(true));
    return frag;
  }

  function closeDrawer(updateHash) {
    var drawer = getDrawer();
    var backdrop = getDrawerBackdrop();
    if (drawer) {
      drawer.removeAttribute("data-visible");
      drawer.hidden = true;
    }
    if (backdrop) {
      backdrop.removeAttribute("data-visible");
      backdrop.hidden = true;
    }
    if (updateHash !== false && location.hash.indexOf("#item=") === 0) {
      try {
        if (history.replaceState) history.replaceState(null, "", location.pathname + location.search);
        else location.hash = "";
      } catch (e) { location.hash = ""; }
    }
  }

  function handleItemLinkClick(ev) {
    var target = ev.target;
    while (target && target !== document) {
      if (target.getAttribute && target.getAttribute("data-item-link")) {
        if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.button === 1) return;
        var id = target.getAttribute("data-item-link");
        if (openItem(id, true)) ev.preventDefault();
        return;
      }
      if (target.classList && target.classList.contains("card") && target.getAttribute("data-id")) {
        if (ev.metaKey || ev.ctrlKey || ev.shiftKey || ev.button === 1) return;
        var cardId = target.getAttribute("data-id");
        if (openItem(cardId, true)) ev.preventDefault();
        return;
      }
      target = target.parentNode;
    }
  }

  function initDrawer() {
    var backdrop = getDrawerBackdrop();
    if (backdrop) backdrop.addEventListener("click", function () { closeDrawer(true); });
    var closeBtn = document.querySelector("[data-drawer-close]");
    if (closeBtn) closeBtn.addEventListener("click", function () { closeDrawer(true); });
    document.addEventListener("click", handleItemLinkClick);
    document.addEventListener("keydown", function (ev) {
      if (ev.key === "Escape") closeDrawer(true);
    });
    var match = (location.hash || "").match(/^#item=(.+)$/);
    if (match && match[1]) openItem(decodeURIComponent(match[1]), false);
  }

  // -------------------------------------------------------------------------
  // Filters (board view)
  // -------------------------------------------------------------------------
  var filterState = {
    status: new Set(),
    priority: new Set(),
    kind: new Set(),
    assignee: new Set(),
    search: "",
    showAbandoned: false
  };

  function readHashFilters() {
    var query = (location.hash || "").replace(/^#/, "");
    var params = new URLSearchParams(query);
    ["status", "priority", "kind", "assignee"].forEach(function (k) {
      var v = params.get(k);
      filterState[k] = v ? new Set(v.split(",").filter(Boolean)) : new Set();
    });
    filterState.search = params.get("q") || "";
    filterState.showAbandoned = params.get("abandoned") === "1";
  }

  function writeHashFilters() {
    var parts = [];
    ["status", "priority", "kind", "assignee"].forEach(function (k) {
      if (filterState[k].size) parts.push(k + "=" + Array.from(filterState[k]).join(","));
    });
    if (filterState.search) parts.push("q=" + encodeURIComponent(filterState.search));
    if (filterState.showAbandoned) parts.push("abandoned=1");
    var openMatch = (location.hash || "").match(/item=([^&]+)/);
    if (openMatch) parts.push("item=" + openMatch[1]);
    var next = parts.length ? "#" + parts.join("&") : location.pathname + location.search;
    try {
      if (history.replaceState) history.replaceState(null, "", next);
      else location.hash = parts.join("&");
    } catch (e) { location.hash = parts.join("&"); }
  }

  function cardMatches(card) {
    if (filterState.status.size && !filterState.status.has(card.dataset.status)) return false;
    if (filterState.priority.size && !filterState.priority.has(card.dataset.priority)) return false;
    if (filterState.kind.size && !filterState.kind.has(card.dataset.kind)) return false;
    if (filterState.assignee.size && !filterState.assignee.has(card.dataset.assignee)) return false;
    if (!filterState.showAbandoned && card.dataset.status === "abandoned") return false;
    if (filterState.search) {
      var q = filterState.search.toLowerCase();
      if ((card.dataset.search || "").indexOf(q) < 0) return false;
    }
    return true;
  }

  function isFiltering() {
    return filterState.status.size > 0 ||
      filterState.priority.size > 0 ||
      filterState.kind.size > 0 ||
      filterState.assignee.size > 0 ||
      filterState.search.length > 0 ||
      filterState.showAbandoned;
  }

  function applyFilters() {
    var cards = document.querySelectorAll(".card[data-id]");
    var visibleCounts = {};
    cards.forEach(function (card) {
      if (cardMatches(card)) {
        card.removeAttribute("data-hidden");
        var col = card.closest(".column");
        if (col) visibleCounts[col.dataset.status] = (visibleCounts[col.dataset.status] || 0) + 1;
      } else {
        card.setAttribute("data-hidden", "true");
      }
    });
    document.querySelectorAll(".epic-group").forEach(function (group) {
      if (group.querySelector(".card:not([data-hidden])")) group.removeAttribute("data-hidden");
      else group.setAttribute("data-hidden", "true");
    });
    document.querySelectorAll(".column").forEach(function (col) {
      if (col.querySelector(".card:not([data-hidden])")) col.removeAttribute("data-hidden");
      else col.setAttribute("data-hidden", "true");
      var countEl = col.querySelector("[data-column-count]");
      if (countEl) countEl.textContent = visibleCounts[col.dataset.status] || 0;
    });
    // Toggle the body.filtering class so saved-collapse state is
    // overridden visually while a filter is active.
    if (isFiltering()) document.body.classList.add("filtering");
    else document.body.classList.remove("filtering");
    var any = document.querySelector(".card:not([data-hidden])");
    var emptyMsg = document.querySelector("[data-filter-empty]");
    if (emptyMsg) emptyMsg.hidden = !!any;
  }

  function initEpicCollapse() {
    document.querySelectorAll(".epic-header").forEach(function (header) {
      header.addEventListener("click", function () {
        var group = header.parentElement;
        if (!group) return;
        if (group.getAttribute("data-collapsed") === "true") group.removeAttribute("data-collapsed");
        else group.setAttribute("data-collapsed", "true");
      });
    });
  }

  function refreshChipUI() {
    document.querySelectorAll("[data-filter-group]").forEach(function (group) {
      var dim = group.dataset.filterGroup;
      group.querySelectorAll(".chip").forEach(function (chip) {
        var v = chip.dataset.filterValue;
        if (filterState[dim] && filterState[dim].has(v)) chip.classList.add("active");
        else chip.classList.remove("active");
      });
    });
    var search = document.querySelector("[data-filter-search]");
    if (search) search.value = filterState.search;
    var toggle = document.querySelector('[data-filter-toggle="show-abandoned"]');
    if (toggle) toggle.checked = filterState.showAbandoned;
  }

  function initFilters() {
    if (!document.querySelector(".filterbar")) return;
    readHashFilters();
    refreshChipUI();
    applyFilters();

    document.querySelectorAll("[data-filter-group]").forEach(function (group) {
      var dim = group.dataset.filterGroup;
      group.addEventListener("click", function (ev) {
        var chip = ev.target.closest && ev.target.closest(".chip");
        if (!chip) return;
        var v = chip.dataset.filterValue;
        if (filterState[dim].has(v)) filterState[dim].delete(v);
        else filterState[dim].add(v);
        refreshChipUI();
        applyFilters();
        writeHashFilters();
      });
    });

    var search = document.querySelector("[data-filter-search]");
    if (search) {
      var t;
      search.addEventListener("input", function () {
        filterState.search = search.value;
        clearTimeout(t);
        t = setTimeout(function () { applyFilters(); writeHashFilters(); }, 80);
      });
    }

    var toggle = document.querySelector('[data-filter-toggle="show-abandoned"]');
    if (toggle) {
      toggle.addEventListener("change", function () {
        filterState.showAbandoned = toggle.checked;
        applyFilters();
        writeHashFilters();
      });
    }

    var reset = document.querySelector("[data-filter-reset]");
    if (reset) {
      reset.addEventListener("click", function () {
        filterState.status.clear();
        filterState.priority.clear();
        filterState.kind.clear();
        filterState.assignee.clear();
        filterState.search = "";
        filterState.showAbandoned = false;
        refreshChipUI();
        applyFilters();
        writeHashFilters();
      });
    }
  }

  var keyHandlers = {
    e: function () { document.querySelectorAll("details.node").forEach(function (n) { n.open = true; }); },
    c: function () { document.querySelectorAll("details.node").forEach(function (n) { n.open = false; }); }
  };

  document.addEventListener("DOMContentLoaded", function () {
    initTheme();
    var btn = document.querySelector("[data-theme-toggle]");
    if (btn) btn.addEventListener("click", cycleTheme);
    initDrawer();
    initFilters();
    initEpicCollapse();
  });

  document.addEventListener("keydown", function (ev) {
    if (ev.target && (ev.target.tagName === "INPUT" || ev.target.tagName === "TEXTAREA")) return;
    var fn = keyHandlers[ev.key];
    if (fn) { fn(); ev.preventDefault(); }
  });
})();
