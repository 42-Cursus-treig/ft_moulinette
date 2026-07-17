(function () {
  // Encadré « statut exam » : décompte rouge jusqu'au début, vert pendant,
  // « Exam terminé » après - puis on enchaîne sur l'exam suivant dès que son
  // horaire est connu. Les instants (begin/finish) sont des dates absolues
  // (RFC3339) : le calcul du temps restant est donc indépendant du fuseau du
  // visiteur, seul son horloge compte.

  function parseItems(box) {
    try {
      return JSON.parse(box.getAttribute('data-exams') || '[]');
    } catch (e) {
      return [];
    }
  }

  function pad(n) { return String(n).padStart(2, '0'); }

  function formatRemaining(ms) {
    if (ms < 0) ms = 0;
    var s = Math.floor(ms / 1000);
    var d = Math.floor(s / 86400); s -= d * 86400;
    var h = Math.floor(s / 3600); s -= h * 3600;
    var m = Math.floor(s / 60); s -= m * 60;
    var hms = pad(h) + ':' + pad(m) + ':' + pad(s);
    return d > 0 ? d + 'j ' + hms : hms;
  }

  // Choisit l'exam pertinent : en cours, sinon le prochain à venir, sinon
  // (tous finis) le dernier avec l'état "done".
  function pickFocus(items, now) {
    for (var i = 0; i < items.length; i++) {
      var end = new Date(items[i].finish).getTime();
      if (now > end) continue; // exam terminé
      var begin = new Date(items[i].begin).getTime();
      return { item: items[i], state: now < begin ? 'upcoming' : 'active' };
    }
    return { item: items[items.length - 1], state: 'done' };
  }

  function scheduleRow(it) {
    return '<div class="exam-sched-row">' +
      '<span class="exam-sched-item">📅 ' + it.date + '</span>' +
      '<span class="exam-sched-item">🕒 ' + it.start + ' → ' + it.end + '</span>' +
      '<span class="exam-sched-item">⏱️ ' + it.duration + '</span>' +
      '</div>';
  }

  function render(box) {
    var items = box.__items || (box.__items = parseItems(box));
    if (!items.length) return;

    var now = Date.now();
    var f = pickFocus(items, now);

    box.classList.remove('state-upcoming', 'state-active', 'state-done');
    box.classList.add('state-' + f.state);

    if (f.state === 'done') {
      box.innerHTML = '<div class="exam-cd-done">✓ Exam terminé</div>';
      return;
    }

    var target = f.state === 'upcoming'
      ? new Date(f.item.begin).getTime()
      : new Date(f.item.finish).getTime();
    var remain = formatRemaining(target - now);
    var timer = f.state === 'upcoming'
      ? '🔴 Commence dans <b>' + remain + '</b>'
      : '🟢 En cours · fin dans <b>' + remain + '</b>';

    box.innerHTML =
      '<div class="exam-cd-head">' +
        '<span class="exam-cd-label">' + f.item.label + '</span>' +
        '<span class="exam-cd-timer">' + timer + '</span>' +
      '</div>' +
      scheduleRow(f.item);
  }

  var interval = null;

  function tick() {
    var boxes = document.querySelectorAll('[data-exam-countdown][data-exams]');
    if (!boxes.length) {
      if (interval) { clearInterval(interval); interval = null; }
      return;
    }
    boxes.forEach(render);
  }

  function start() {
    tick();
    if (!interval) interval = setInterval(tick, 1000);
  }

  if (document.readyState !== 'loading') start();
  else document.addEventListener('DOMContentLoaded', start);

  // Après un swap htmx, l'encadré est remplacé (horaires éventuellement mis à
  // jour) : on relance le ticker sur le nouvel élément.
  document.body.addEventListener('htmx:afterSwap', start);
})();
