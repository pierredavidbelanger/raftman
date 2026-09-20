// raftman web UI: two POSTs against api/stat and api/list, nothing else.
(function () {
  "use strict";

  var $ = function (id) { return document.getElementById(id); };
  var fromInput = $("from"), toInput = $("to"), messageInput = $("message"), followInput = $("follow"),
      prevButton = $("prev"), nextButton = $("next"), statBody = $("stat-body"), listBody = $("list-body"),
      listPane = $("list"), status = $("status");

  var statRequest = { Limit: 500 };
  var listRequest = { Limit: 50 };
  var selected = key("*", "*");

  function post(endpoint, body) {
    return fetch("api/" + endpoint, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body)
    }).then(function (res) {
      return res.json();
    }).then(function (data) {
      if (data.Error) { throw new Error(data.Error); }
      return data;
    });
  }

  function showError(err) { status.textContent = err.message || String(err); }
  function clearError() { status.textContent = ""; }

  function pad(n) { return (n < 10 ? "0" : "") + n; }

  function formatTimestamp(s) {
    var d = new Date(s);
    if (isNaN(d)) { return s; }
    return d.getFullYear() + "-" + pad(d.getMonth() + 1) + "-" + pad(d.getDate()) + " " +
      pad(d.getHours()) + ":" + pad(d.getMinutes()) + ":" + pad(d.getSeconds());
  }

  function row(cells, k) {
    var tr = document.createElement("tr");
    cells.forEach(function (c) {
      var td = document.createElement("td");
      td.textContent = c;
      tr.appendChild(td);
    });
    if (k !== undefined) { tr.dataset.key = k; }
    return tr;
  }

  // key joins hostname and application with a separator no hostname contains.
  function key(host, app) { return host + "" + app; }

  // updateStat rebuilds the sidebar and re-selects the current row, which in
  // turn refreshes the list.
  function updateStat() {
    return post("stat", statRequest).then(function (data) {
      var rows = [row(["*", "*", ""], key("*", "*"))];
      var stat = data.Stat || {};
      Object.keys(stat).forEach(function (host) {
        rows.push(row([host, "*", ""], key(host, "*")));
        Object.keys(stat[host]).forEach(function (app) {
          rows.push(row([host, app, stat[host][app]], key(host, app)));
        });
      });
      statBody.replaceChildren.apply(statBody, rows);
      if (!select(selected)) { select(key("*", "*")); }
      clearError();
    }).catch(showError);
  }

  function select(k) {
    var found = false;
    Array.prototype.forEach.call(statBody.children, function (tr) {
      var match = tr.dataset.key === k;
      tr.classList.toggle("selected", match);
      found = found || match;
    });
    if (!found) { return false; }
    selected = k;
    var parts = k.split("");
    listRequest.Hostname = parts[0] !== "*" ? parts[0] : undefined;
    listRequest.Application = parts[1] !== "*" ? parts[1] : undefined;
    listRequest.Offset = 0;
    updateList();
    return true;
  }

  // updateList shows the page oldest first, newest at the bottom.
  function updateList() {
    return post("list", listRequest).then(function (data) {
      var entries = (data.Entries || []).slice().reverse();
      listBody.replaceChildren.apply(listBody, entries.map(function (e) {
        return row([formatTimestamp(e.Timestamp), e.Hostname, e.Application, e.Message]);
      }));
      listPane.scrollTop = listPane.scrollHeight;
      clearError();
    }).catch(showError);
  }

  function schedule() {
    setTimeout(function () {
      (followInput.checked ? updateStat() : Promise.resolve()).then(schedule, schedule);
    }, 5000);
  }

  function toISO(value) {
    if (!value) { return undefined; }
    var d = new Date(value);
    return isNaN(d) ? undefined : d.toISOString();
  }

  fromInput.addEventListener("change", function () {
    statRequest.FromTimestamp = listRequest.FromTimestamp = toISO(fromInput.value);
    listRequest.Offset = 0;
    updateStat();
  });

  toInput.addEventListener("change", function () {
    statRequest.ToTimestamp = listRequest.ToTimestamp = toISO(toInput.value);
    listRequest.Offset = 0;
    updateStat();
  });

  // "change" fires on blur, "search" on Enter and on clearing the field;
  // both may fire for one edit, so only react when the value really changed.
  function applyMessage() {
    var value = messageInput.value || undefined;
    if (value === statRequest.Message) { return; }
    statRequest.Message = listRequest.Message = value;
    listRequest.Offset = 0;
    updateStat();
  }
  messageInput.addEventListener("change", applyMessage);
  messageInput.addEventListener("search", applyMessage);
  messageInput.addEventListener("keydown", function (e) {
    if (e.key === "Enter") { applyMessage(); }
  });

  followInput.addEventListener("change", function () {
    if (followInput.checked) {
      listRequest.Offset = 0;
      updateStat();
    }
  });

  prevButton.addEventListener("click", function () {
    followInput.checked = false;
    listRequest.Offset = (listRequest.Offset || 0) + listRequest.Limit;
    updateList();
  });

  nextButton.addEventListener("click", function () {
    followInput.checked = false;
    listRequest.Offset = (listRequest.Offset || 0) - listRequest.Limit;
    if (listRequest.Offset <= 0) {
      listRequest.Offset = 0;
      followInput.checked = true;
      updateStat();
    } else {
      updateList();
    }
  });

  statBody.addEventListener("click", function (e) {
    var tr = e.target.closest("tr");
    if (tr && tr.dataset.key !== undefined) { select(tr.dataset.key); }
  });

  updateStat().then(schedule);
})();
