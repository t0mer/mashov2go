// Renders /api/openapi.json as a list grouped by tag. Text is set with textContent,
// so the document cannot inject markup into the page.
(function () {
  "use strict";
  var out = document.getElementById("out");

  function cell(text, tag) {
    var el = document.createElement(tag || "td");
    if (tag === "code") {
      var c = document.createElement("code");
      c.textContent = text;
      el.appendChild(c);
    } else {
      el.textContent = text;
    }
    return el;
  }

  fetch("/api/openapi.json", { credentials: "same-origin" })
    .then(function (r) {
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.json();
    })
    .then(function (doc) {
      out.textContent = "";
      var byTag = {};
      Object.keys(doc.paths).forEach(function (path) {
        Object.keys(doc.paths[path]).forEach(function (method) {
          var op = doc.paths[path][method];
          var tag = (op.tags && op.tags[0]) || "other";
          (byTag[tag] = byTag[tag] || []).push({ method: method.toUpperCase(), path: path, op: op });
        });
      });
      Object.keys(byTag).sort().forEach(function (tag) {
        var h = document.createElement("h2");
        h.textContent = tag;
        out.appendChild(h);
        var table = document.createElement("table");
        var head = document.createElement("tr");
        ["Method", "Path", "Summary", "Access"].forEach(function (x) {
          head.appendChild(cell(x, "th"));
        });
        table.appendChild(head);
        byTag[tag].forEach(function (e) {
          var tr = document.createElement("tr");
          var access = e.op.security && e.op.security.length === 0 ? "public" : (e.op["x-scope"] || "session");
          tr.appendChild(cell(e.method));
          tr.appendChild(cell(e.path, "code"));
          tr.appendChild(cell(e.op.summary || ""));
          tr.appendChild(cell(access));
          table.appendChild(tr);
        });
        out.appendChild(table);
      });
    })
    .catch(function (err) {
      out.textContent = "Could not load the API description (" + err.message + ").";
    });
})();
