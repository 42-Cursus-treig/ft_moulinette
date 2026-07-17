// Alertes de défense du dashboard : notification navigateur 15 min avant une
// correction et à la révélation du login du corrigé. Aucune requête réseau :
// on lit les lignes que la carte « Corrections à venir » rafraîchit déjà en
// htmx, et on garde en localStorage ce qui a déjà été notifié.
(function () {
  var PREF = 'ft-moul-notify';        // 'on' quand l'utilisateur a activé la cloche
  var SEEN = 'ft-moul-notify-seen';   // état par défense : {id: {soon: bool, who: "login"}}

  function enabled() {
    return localStorage.getItem(PREF) === 'on' &&
      'Notification' in window && Notification.permission === 'granted';
  }

  function seen() {
    try { return JSON.parse(localStorage.getItem(SEEN) || '{}'); } catch (e) { return {}; }
  }

  function saveSeen(s) {
    // On ne garde que les défenses encore affichées : pas de fuite mémoire.
    localStorage.setItem(SEEN, JSON.stringify(s));
  }

  function notify(title, body) {
    try { new Notification(title, { body: body, icon: '/static/moon.svg' }); } catch (e) { /* bloqué */ }
  }

  // Parcourt les lignes de la carte et déclenche ce qui doit l'être.
  function scan() {
    if (!enabled()) return;
    var rows = document.querySelectorAll('#def-list .def-row');
    if (!rows.length) return;
    var state = seen();
    var next = {};
    var now = Date.now();

    rows.forEach(function (row) {
      var id = row.dataset.defId;
      if (!id) return;
      var prev = state[id] || {};
      var cur = { soon: !!prev.soon, who: prev.who || '' };
      var begin = Date.parse(row.dataset.begin || '');
      var minutes = (begin - now) / 60000;
      var project = row.dataset.project || 'défense';
      var who = row.dataset.who || '';

      if (!cur.soon && minutes > 0 && minutes <= 15) {
        cur.soon = true;
        notify('Correction dans ' + Math.max(1, Math.round(minutes)) + ' min', project + (who ? ' — ' + who : ''));
      }
      if (who && who !== cur.who) {
        cur.who = who;
        if (minutes > -60) notify('Corrigé révélé : ' + who, project);
      }
      next[id] = cur;
    });
    saveSeen(next);
  }

  function updateBell() {
    var bell = document.getElementById('def-bell');
    if (!bell) return;
    if (!('Notification' in window)) { bell.hidden = true; return; }
    var on = enabled();
    bell.textContent = on ? '🔔 Alertes ON' : '🔕 Alertes OFF';
    bell.classList.toggle('on', on);
    if (!bell.dataset.ready) {
      bell.dataset.ready = '1';
      bell.addEventListener('click', function () {
        if (enabled()) {
          localStorage.setItem(PREF, 'off');
          updateBell();
          return;
        }
        // La demande de permission exige un geste utilisateur : c'est ici.
        Notification.requestPermission().then(function (perm) {
          localStorage.setItem(PREF, perm === 'granted' ? 'on' : 'off');
          updateBell();
          if (perm === 'granted') scan();
        });
      });
    }
  }

  function setup() {
    updateBell();
    scan();
  }

  document.body.addEventListener('htmx:afterSwap', setup);
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', setup);
  } else {
    setup();
  }
  // Le seuil des 15 min peut être franchi entre deux rafraîchissements htmx :
  // re-scan local (aucun réseau) toutes les 30 s.
  setInterval(scan, 30000);
})();

// Éditeur de disposition : à gauche l'inventaire (widgets rangés), à droite
// un APERÇU visuel du dashboard — une mini-grille fidèle à la vraie (12
// colonnes). On glisse une carte d'un panneau à l'autre, on la déplace dans
// l'aperçu pour réordonner, et on la redimensionne en tirant son bord droit,
// avec accrochage sur trois tailles : Petite (⅓), Moyenne (½), Grande
// (pleine largeur). Écouteurs de drag posés sur window + délégation sur
// l'éditeur : une carte qui change de panneau reste manipulable dans les
// deux sens.
(function () {
  var editor = document.getElementById('dash-editor');
  if (!editor) return;
  var preview = document.getElementById('pane-active');
  var reserve = document.getElementById('pane-reserve');

  var SPANS = [4, 6, 12];
  var LABELS = { 4: 'Petite', 6: 'Moyenne', 12: 'Grande' };

  function msg(text) {
    var el = document.getElementById('dash-layout-msg');
    if (el) el.textContent = text || '';
  }

  function snap(n) {
    n = parseInt(n, 10);
    return SPANS.indexOf(n) >= 0 ? n : 6;
  }

  // Met une carte en conformité avec son panneau : dans l'aperçu elle porte
  // sa largeur, son étiquette de taille et sa poignée ; dans l'inventaire non.
  function decorate(mini) {
    var placed = mini.parentElement === preview;
    mini.classList.toggle('placed', placed);
    SPANS.forEach(function (sp) { mini.classList.remove('span-' + sp); });
    var label = mini.querySelector('.widget-size-label');
    var handle = mini.querySelector('.widget-resize');
    if (placed) {
      var span = snap(mini.dataset.span);
      mini.dataset.span = span;
      mini.classList.add('span-' + span);
      if (!label) {
        label = document.createElement('span');
        label.className = 'widget-size-label';
        mini.appendChild(label);
      }
      label.textContent = LABELS[span];
      if (!handle) {
        handle = document.createElement('span');
        handle.className = 'widget-resize';
        handle.title = 'Redimensionner';
        mini.appendChild(handle);
      }
    } else {
      if (label) label.remove();
      if (handle) handle.remove();
    }
  }

  editor.querySelectorAll('.widget-mini').forEach(decorate);

  var drag = null; // { mini, mode: 'move' | 'resize' }

  // Largeur tirée → taille accrochée (seuils à ~42 % et ~80 % de la grille).
  function spanFromWidth(px) {
    var total = preview.getBoundingClientRect().width;
    var frac = total > 0 ? px / total : 0;
    if (frac < 0.42) return 4;
    if (frac < 0.8) return 6;
    return 12;
  }

  function onMove(e) {
    if (!drag) return;

    if (drag.mode === 'resize') {
      var r = drag.mini.getBoundingClientRect();
      var span = spanFromWidth(e.clientX - r.left);
      if (span !== parseInt(drag.mini.dataset.span, 10)) {
        drag.mini.dataset.span = span;
        decorate(drag.mini);
      }
      return;
    }

    var under = document.elementFromPoint(e.clientX, e.clientY);
    if (!under) return;
    var over = under.closest('.widget-mini');
    if (over === drag.mini) over = null;

    if (over && preview.contains(over)) {
      // Insertion dans l'ordre de lecture de la grille : avant si le curseur
      // est au-dessus du centre, ou sur la même ligne à gauche du centre.
      var r2 = over.getBoundingClientRect();
      var before = e.clientY < r2.top + r2.height / 2 ||
        (e.clientY < r2.bottom && e.clientX < r2.left + r2.width / 2);
      preview.insertBefore(drag.mini, before ? over : over.nextSibling);
      decorate(drag.mini);
    } else if (over && reserve.contains(over)) {
      reserve.insertBefore(drag.mini, over);
      decorate(drag.mini);
    } else if (under.closest('#pane-active') && !preview.contains(drag.mini)) {
      preview.appendChild(drag.mini);
      decorate(drag.mini);
    } else if (under.closest('[data-pane="reserve"]') && !reserve.contains(drag.mini)) {
      reserve.appendChild(drag.mini);
      decorate(drag.mini);
    }
  }

  function onUp() {
    if (!drag) return;
    drag.mini.classList.remove('dragging');
    drag.mini.style.pointerEvents = '';
    window.removeEventListener('pointermove', onMove);
    window.removeEventListener('pointerup', onUp);
    window.removeEventListener('pointercancel', onUp);
    drag = null;
  }

  editor.addEventListener('pointerdown', function (e) {
    if (e.button !== undefined && e.button !== 0) return;
    var mini = e.target.closest('.widget-mini');
    if (!mini) return;
    e.preventDefault();
    var resizing = !!e.target.closest('.widget-resize') && preview.contains(mini);
    drag = { mini: mini, mode: resizing ? 'resize' : 'move' };
    if (!resizing) {
      mini.classList.add('dragging');
      mini.style.pointerEvents = 'none'; // elementFromPoint doit voir dessous
    }
    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', onUp);
  });

  var open = document.getElementById('dash-edit');
  if (open) open.addEventListener('click', function () { editor.hidden = false; msg(''); });
  var close = document.getElementById('dash-editor-close');
  if (close) close.addEventListener('click', function () { editor.hidden = true; });
  editor.addEventListener('click', function (e) {
    if (e.target === editor) editor.hidden = true; // clic sur le fond = fermer
  });

  function post(body) {
    return fetch('/ui/dashboard/layout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }).then(function (res) {
      if (res.ok) { location.reload(); return; }
      return res.text().then(function (t) { throw new Error(t || 'Enregistrement impossible.'); });
    }).catch(function (err) {
      msg((err && err.message && err.message.length < 120) ? err.message : 'Enregistrement impossible.');
    });
  }

  var save = document.getElementById('dash-layout-save');
  if (save) save.addEventListener('click', function () {
    var widgets = [];
    preview.querySelectorAll('.widget-mini').forEach(function (c) {
      widgets.push({ id: c.dataset.id, span: snap(c.dataset.span) });
    });
    if (!widgets.length) { msg('Garde au moins un widget sur le dashboard.'); return; }
    msg('Enregistrement…');
    post({ widgets: widgets });
  });

  var reset = document.getElementById('dash-layout-reset');
  if (reset) reset.addEventListener('click', function () {
    msg('Réinitialisation…');
    post({ reset: true });
  });
})();
