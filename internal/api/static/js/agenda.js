// Agenda des créneaux de correction, en édition locale : poser, déplacer,
// étirer et retirer ne touchent que l'écran. Les règles (chevauchement, zones
// passées) sont vérifiées immédiatement en local. Rien ne part chez 42 avant
// le bouton « Enregistrer », qui envoie tout le lot à /ui/slots/sync — le
// serveur y décale de 15 min les créneaux en conflit côté intra — puis le
// calendrier se recharge depuis la source de vérité.
(function () {
  var drag = null;       // geste en cours (création, déplacement, étirement)
  var deletedIds = [];   // granules 42 supprimées localement, à pousser au save
  var saving = false;
  var pendingFlash = null; // message à réafficher après le rechargement

  function grid() { return document.querySelector('.ag-grid'); }
  function px15() { return parseInt(grid().dataset.px15, 10); }
  function gStart() { return parseInt(grid().dataset.start, 10); }
  function gEnd() { return parseInt(grid().dataset.end, 10); }
  function week() { return grid().dataset.week; }

  function fmt(m) { return ('0' + Math.floor(m / 60)).slice(-2) + ':' + ('0' + (m % 60)).slice(-2); }
  function minToPx(m) { return (m - gStart()) * px15() / 15; }

  // Position verticale du curseur → minutes depuis minuit, calée sur 15 min.
  function yToMin(col, clientY) {
    var rect = col.getBoundingClientRect();
    var y = Math.min(Math.max(clientY - rect.top, 0), rect.height);
    return gStart() + Math.round(y / px15()) * 15;
  }
  function colMin(col) { return parseInt(col.dataset.min, 10) || gStart(); }
  function clamp(m, lo, hi) { return Math.max(lo, Math.min(hi, m)); }

  // Colonne (jour) sous une abscisse donnée — pour le déplacement inter-jours.
  function colAtX(clientX) {
    var cols = document.querySelectorAll('.ag-col');
    for (var i = 0; i < cols.length; i++) {
      var r = cols[i].getBoundingClientRect();
      if (clientX >= r.left && clientX < r.right) return cols[i];
    }
    return null;
  }

  function flash(msg, ok) {
    var el = document.getElementById('ag-flash');
    if (!el) return;
    el.textContent = msg;
    el.classList.toggle('ok', !!ok);
    el.hidden = false;
    clearTimeout(el._t);
    el._t = setTimeout(function () { el.hidden = true; }, ok ? 6000 : 5000);
  }

  // Un créneau [a,b] chevauche-t-il un autre bloc de la même colonne ?
  function overlaps(col, a, b, exclude) {
    var blocks = col.querySelectorAll('.ag-block');
    for (var i = 0; i < blocks.length; i++) {
      var el = blocks[i];
      if (el === exclude || el.classList.contains('ghost')) continue;
      var s = parseInt(el.dataset.start, 10);
      var e = parseInt(el.dataset.end, 10);
      if (a < e && b > s) return true;
    }
    return false;
  }

  // (Re)positionne un bloc d'après ses data-start/data-end.
  function place(el) {
    var s = parseInt(el.dataset.start, 10);
    var e = parseInt(el.dataset.end, 10);
    el.style.top = minToPx(s) + 'px';
    el.style.height = (minToPx(e) - minToPx(s) - 2) + 'px';
    var label = el.querySelector('.ag-block-label');
    if (label) label.textContent = fmt(s) + ' – ' + fmt(e);
  }

  function delSvg() {
    return '<svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round">' +
      '<line x1="5" y1="5" x2="19" y2="19"></line><line x1="19" y1="5" x2="5" y2="19"></line></svg>';
  }

  function makeBlock(a, b) {
    var el = document.createElement('div');
    el.className = 'ag-block free';
    el.dataset.start = a;
    el.dataset.end = b;
    el.dataset.ids = '';
    el.dataset.pending = 'new';
    el.innerHTML = '<span class="ag-handle top" aria-hidden="true"></span>' +
      '<span class="ag-block-label"></span>' +
      '<button type="button" class="ag-del" aria-label="Supprimer ce créneau">' + delSvg() + '</button>' +
      '<span class="ag-handle bottom" aria-hidden="true"></span>';
    bindDel(el.querySelector('.ag-del'));
    return el;
  }

  // --- lot de modifications locales ---

  function pendingCount() {
    return document.querySelectorAll('.ag-block.free[data-pending]').length + deletedIds.length;
  }

  function updateBar() {
    var bar = document.getElementById('ag-savebar');
    if (!bar) return;
    var n = pendingCount();
    bar.hidden = n === 0 && !saving;
    var count = document.getElementById('ag-savecount');
    if (count) count.textContent = n + ' modification' + (n > 1 ? 's' : '') + ' non enregistrée' + (n > 1 ? 's' : '');
    var btn = document.getElementById('ag-save');
    if (btn) {
      btn.disabled = saving || n === 0;
      btn.textContent = saving ? 'Enregistrement…' : 'Enregistrer';
    }
    var discard = document.getElementById('ag-discard');
    if (discard) discard.disabled = saving;
    updateTotal();
  }

  function refreshCalendar() {
    htmx.ajax('GET', '/ui/slots?week=' + week(), { target: '#agenda-root', swap: 'innerHTML' });
  }

  function saveAll() {
    if (saving) return;
    var payload = { week: week(), delete: deletedIds.slice(), create: [] };
    document.querySelectorAll('.ag-block.free[data-pending]').forEach(function (el) {
      (el.dataset.ids || '').split(',').forEach(function (raw) {
        var id = parseInt(raw, 10);
        if (id > 0) payload.delete.push(id);
      });
      payload.create.push({
        day: el.parentElement.dataset.day,
        start: parseInt(el.dataset.start, 10),
        end: parseInt(el.dataset.end, 10),
      });
    });
    if (!payload.delete.length && !payload.create.length) return;

    saving = true;
    updateBar();
    fetch('/ui/slots/sync', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
      .then(function (res) {
        return res.text().then(function (text) {
          if (!res.ok) throw new Error(text || 'Erreur réseau');
          return JSON.parse(text);
        });
      })
      .then(function (res) {
        var parts = [];
        if (res.saved) parts.push(res.saved + ' créneau' + (res.saved > 1 ? 'x' : '') + ' enregistré' + (res.saved > 1 ? 's' : ''));
        (res.shifted || []).forEach(function (s) { parts.push('↷ ' + s); });
        (res.failed || []).forEach(function (f) { parts.push('✗ ' + f); });
        pendingFlash = { msg: parts.join(' · ') || 'Rien à enregistrer.', ok: !(res.failed || []).length };
        deletedIds = [];
        refreshCalendar();
      })
      .catch(function (err) {
        var msg = (err && err.message) || 'Enregistrement impossible.';
        flash(msg.length > 200 ? 'Enregistrement impossible (API 42).' : msg, false);
      })
      .finally(function () {
        saving = false;
        updateBar();
      });
  }

  function discardAll() {
    if (saving) return;
    deletedIds = [];
    refreshCalendar();
  }

  // Total d'heures de dispo posées sur la semaine affichée (brouillon inclus).
  function updateTotal() {
    var el = document.getElementById('ag-total');
    if (!el) return;
    var mins = 0;
    document.querySelectorAll('.ag-block.free').forEach(function (b) {
      mins += parseInt(b.dataset.end, 10) - parseInt(b.dataset.start, 10);
    });
    el.textContent = mins ? Math.floor(mins / 60) + ' h ' + ('0' + (mins % 60)).slice(-2) + ' de dispo' : '';
  }

  // Recopie les dispos de la semaine précédente EN BROUILLON : un appel API
  // (mis en cache) pour lire l'ancienne semaine, zéro écriture avant le save.
  function copyPrevWeek() {
    if (saving) return;
    fetch('/ui/slots/copy?week=' + week())
      .then(function (res) {
        return res.text().then(function (text) {
          if (!res.ok) throw new Error(text || 'Erreur réseau');
          return JSON.parse(text);
        });
      })
      .then(function (res) {
        var added = 0;
        var skipped = 0;
        (res.create || []).forEach(function (r) {
          var col = document.querySelector('.ag-col[data-day="' + r.day + '"]');
          if (!col || col.classList.contains('past')) { skipped++; return; }
          var a = Math.max(r.start, colMin(col));
          var b = r.end;
          if (b - a < 15 || overlaps(col, a, b, null)) { skipped++; return; }
          var el = makeBlock(a, b);
          col.appendChild(el);
          place(el);
          added++;
        });
        updateBar();
        if (added) {
          flash(added + ' créneau(x) recopié(s) en brouillon — pense à Enregistrer.', true);
        } else if (skipped) {
          flash(skipped + ' créneau(x) ignoré(s) : déjà passés ou en chevauchement.');
        } else {
          flash('Aucune dispo à recopier sur la semaine précédente.');
        }
      })
      .catch(function (err) {
        var msg = (err && err.message) || 'Recopie impossible.';
        flash(msg.length > 200 ? 'Recopie impossible (API 42).' : msg);
      });
  }

  // --- geste en cours ---

  function onMove(e) {
    if (!drag) return;
    var m = clamp(yToMin(drag.col, e.clientY), gStart(), gEnd());

    if (drag.kind === 'create') {
      m = Math.max(drag.min, m);
      var a = Math.min(drag.anchor, m);
      var b = Math.max(drag.anchor, m);
      if (b - a < 15) b = Math.min(gEnd(), a + 15);
      if (b - a < 15) a = b - 15;
      drag.cur = [a, b];
      drag.ghost.style.top = minToPx(a) + 'px';
      drag.ghost.style.height = (minToPx(b) - minToPx(a) - 2) + 'px';
      drag.ghost.textContent = fmt(a) + ' – ' + fmt(b);
      return;
    }

    var ns = drag.origStart;
    var ne = drag.origEnd;
    if (drag.kind === 'move') {
      // Déplacement horizontal : la colonne sous le curseur devient le jour
      // cible (si elle n'est pas révolue) — le bloc y est reparenté à la volée.
      var target = colAtX(e.clientX);
      if (target && target !== drag.col && !target.classList.contains('past')) {
        target.appendChild(drag.el);
        drag.col = target;
        drag.min = colMin(target);
      }
      var delta = m - drag.grab;
      ns = drag.origStart + delta;
      ne = drag.origEnd + delta;
      if (ns < drag.min) { ne += drag.min - ns; ns = drag.min; }
      if (ne > gEnd()) { ns -= ne - gEnd(); ne = gEnd(); }
    } else if (drag.kind === 'top') {
      ns = clamp(Math.min(m, drag.origEnd - 15), drag.min, drag.origEnd - 15);
    } else if (drag.kind === 'bottom') {
      ne = clamp(Math.max(m, drag.origStart + 15), drag.origStart + 15, gEnd());
    }
    drag.cur = [ns, ne];
    drag.el.dataset.start = ns;
    drag.el.dataset.end = ne;
    place(drag.el);
  }

  function onUp() {
    // Les écouteurs sont armés sur la colonne d'origine (pointer capture) —
    // même si le bloc a changé de jour en route.
    var col = drag ? (drag.homeCol || drag.col) : null;
    if (col) {
      col.removeEventListener('pointermove', onMove);
      col.removeEventListener('pointerup', onUp);
      col.removeEventListener('pointercancel', onCancel);
    }
    finish();
  }

  function onCancel() { cancelGesture(); }

  function finish() {
    if (!drag) return;
    var d = drag;
    drag = null;

    if (d.kind === 'create') {
      d.ghost.remove();
      if (!d.cur) return;
      if (overlaps(d.col, d.cur[0], d.cur[1], null)) {
        flash('Ce créneau en chevauche un autre.');
        return;
      }
      var el = makeBlock(d.cur[0], d.cur[1]);
      d.col.appendChild(el);
      place(el);
      updateBar();
      return;
    }

    d.el.classList.remove('dragging');
    var r = d.cur || [d.origStart, d.origEnd];
    var sameSpot = r[0] === d.origStart && r[1] === d.origEnd && d.col === d.homeCol;
    if (sameSpot) { place(d.el); return; } // pas bougé
    if (overlaps(d.col, r[0], r[1], d.el)) {
      // Rollback complet : plage ET jour d'origine.
      if (d.homeCol && d.el.parentElement !== d.homeCol) d.homeCol.appendChild(d.el);
      d.el.dataset.start = d.origStart;
      d.el.dataset.end = d.origEnd;
      place(d.el);
      flash('Ce créneau en chevauche un autre.');
      return;
    }
    // Modification purement locale : un bloc déjà connu de 42 devient « edit »
    // (ses granules seront remplacées au save), un bloc local reste « new ».
    if (!d.el.dataset.pending) d.el.dataset.pending = 'edit';
    updateBar();
  }

  function cancelGesture() {
    if (!drag) return;
    if (drag.kind === 'create') drag.ghost.remove();
    else {
      drag.el.classList.remove('dragging');
      if (drag.homeCol && drag.el.parentElement !== drag.homeCol) drag.homeCol.appendChild(drag.el);
      drag.el.dataset.start = drag.origStart;
      drag.el.dataset.end = drag.origEnd;
      place(drag.el);
    }
    drag = null;
  }

  function onDown(e) {
    if (drag || saving) return;
    if (e.button !== undefined && e.button !== 0) return;
    if (e.target.closest('.ag-del')) return; // suppression : gérée au clic

    var col = e.currentTarget;
    var block = e.target.closest('.ag-block');
    if (block && block.dataset.booked) return; // corrections figées

    if (!block && col.classList.contains('past')) {
      flash('Ce jour est passé — rien à poser ici.');
      return;
    }

    e.preventDefault();
    try { col.setPointerCapture(e.pointerId); } catch (err) { /* tactile ancien */ }

    if (block) {
      var handle = e.target.closest('.ag-handle');
      drag = {
        kind: handle ? (handle.classList.contains('top') ? 'top' : 'bottom') : 'move',
        col: col,
        homeCol: col,
        el: block,
        origStart: parseInt(block.dataset.start, 10),
        origEnd: parseInt(block.dataset.end, 10),
        grab: yToMin(col, e.clientY),
        min: colMin(col),
      };
      block.classList.add('dragging');
    } else {
      var mn = colMin(col);
      var ghost = document.createElement('div');
      ghost.className = 'ag-block ghost';
      col.appendChild(ghost);
      drag = { kind: 'create', col: col, min: mn, anchor: Math.max(mn, yToMin(col, e.clientY)), ghost: ghost };
    }
    armCol(col);
    onMove(e);
  }

  function armCol(col) {
    col.addEventListener('pointermove', onMove);
    col.addEventListener('pointerup', onUp);
    col.addEventListener('pointercancel', onCancel);
  }

  function bindDel(btn) {
    if (!btn || btn.dataset.agReady) return;
    btn.dataset.agReady = '1';
    btn.addEventListener('click', function (e) {
      e.preventDefault();
      e.stopPropagation();
      if (saving) return;
      var block = btn.closest('.ag-block');
      if (!block) return;
      // Suppression locale : les granules 42 rejoignent le lot, un bloc encore
      // jamais enregistré disparaît simplement.
      (block.dataset.ids || '').split(',').forEach(function (raw) {
        var id = parseInt(raw, 10);
        if (id > 0) deletedIds.push(id);
      });
      block.remove();
      updateBar();
    });
  }

  // Le calendrier est re-rendu à chaque navigation/rechargement htmx : on
  // (ré)arme tout après chaque swap. Le swap repart de l'état serveur : le
  // lot local est réinitialisé (c'est le comportement voulu pour Annuler).
  function setup() {
    deletedIds = [];
    document.querySelectorAll('.ag-col').forEach(function (col) {
      if (col.dataset.agReady) return;
      col.dataset.agReady = '1';
      col.addEventListener('pointerdown', onDown);
    });
    document.querySelectorAll('.ag-del').forEach(bindDel);
    var save = document.getElementById('ag-save');
    if (save && !save.dataset.agReady) {
      save.dataset.agReady = '1';
      save.addEventListener('click', saveAll);
    }
    var discard = document.getElementById('ag-discard');
    if (discard && !discard.dataset.agReady) {
      discard.dataset.agReady = '1';
      discard.addEventListener('click', discardAll);
    }
    var copy = document.getElementById('ag-copy');
    if (copy && !copy.dataset.agReady) {
      copy.dataset.agReady = '1';
      copy.addEventListener('click', copyPrevWeek);
    }
    updateBar();
    if (pendingFlash) {
      flash(pendingFlash.msg, pendingFlash.ok);
      pendingFlash = null;
    }
  }

  document.body.addEventListener('htmx:afterSwap', setup);
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', setup);
  } else {
    setup();
  }

  // Changer de semaine avec des modifications en attente les perdrait :
  // on demande confirmation avant de laisser htmx naviguer.
  document.body.addEventListener('htmx:confirm', function (e) {
    if (!pendingCount()) return;
    if (!e.detail.elt || !e.detail.elt.closest('.ag-nav')) return;
    e.preventDefault();
    if (confirm('Des modifications ne sont pas enregistrées. Les abandonner ?')) {
      e.detail.issueRequest(true);
    }
  });

  window.addEventListener('beforeunload', function (e) {
    if (pendingCount()) {
      e.preventDefault();
      e.returnValue = '';
    }
  });

  // Rafraîchissement doux : les réservations de défenses arrivent côté 42 sans
  // prévenir — on recharge toutes les 2 min, mais jamais pendant un geste, un
  // enregistrement, des modifications en attente ou onglet caché.
  setInterval(function () {
    if (document.hidden || drag || saving) return;
    if (!grid()) return;
    if (pendingCount()) return;
    refreshCalendar();
  }, 120000);
})();
