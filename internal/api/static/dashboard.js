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
