// Pose de créneaux au glisser-déposer sur l'agenda : on trace un fantôme
// pendant le drag, et au relâchement on POST la plage à /ui/slots (htmx
// remplace le calendrier par sa version à jour). Pointer events : la même
// mécanique sert à la souris et au tactile.
(function () {
  var drag = null;

  function grid(col) { return col.closest('.ag-grid'); }

  // Convertit une position verticale en minutes depuis minuit, calée sur 15 min.
  function minsFromY(col, clientY) {
    var rect = col.getBoundingClientRect();
    var g = grid(col);
    var px15 = parseInt(g.dataset.px15, 10);
    var startMin = parseInt(g.dataset.start, 10);
    var y = Math.min(Math.max(clientY - rect.top, 0), rect.height);
    return startMin + Math.round(y / px15) * 15;
  }

  function fmt(m) {
    return ('0' + Math.floor(m / 60)).slice(-2) + ':' + ('0' + (m % 60)).slice(-2);
  }

  function currentRange(clientY) {
    var m = minsFromY(drag.col, clientY);
    var a = Math.min(drag.anchor, m);
    var b = Math.max(drag.anchor, m);
    if (b - a < 15) b = a + 15; // au moins une granule
    return [a, b];
  }

  function start(e) {
    if (drag) return;
    if (e.button !== undefined && e.button !== 0) return;
    if (e.target.closest('.ag-block')) return; // pas de drag depuis un bloc existant
    var col = e.currentTarget;
    e.preventDefault();
    try { col.setPointerCapture(e.pointerId); } catch (err) { /* tactile ancien */ }

    var ghost = document.createElement('div');
    ghost.className = 'ag-block ghost';
    col.appendChild(ghost);
    drag = { col: col, anchor: minsFromY(col, e.clientY), ghost: ghost };
    update(e);

    col.addEventListener('pointermove', update);
    col.addEventListener('pointerup', finish);
    col.addEventListener('pointercancel', cancel);
  }

  function update(e) {
    if (!drag) return;
    var g = grid(drag.col);
    var px15 = parseInt(g.dataset.px15, 10);
    var startMin = parseInt(g.dataset.start, 10);
    var r = currentRange(e.clientY);
    drag.ghost.style.top = ((r[0] - startMin) / 15 * px15) + 'px';
    drag.ghost.style.height = ((r[1] - r[0]) / 15 * px15 - 2) + 'px';
    drag.ghost.textContent = fmt(r[0]) + ' – ' + fmt(r[1]);
  }

  function finish(e) {
    if (!drag) return;
    var r = currentRange(e.clientY);
    var col = drag.col;
    var week = grid(col).dataset.week;
    cleanup();
    htmx.ajax('POST', '/ui/slots', {
      target: '#agenda-root',
      swap: 'innerHTML',
      values: { day: col.dataset.day, start: r[0], end: r[1], week: week },
    });
  }

  function cancel() { cleanup(); }

  function cleanup() {
    if (!drag) return;
    drag.ghost.remove();
    drag.col.removeEventListener('pointermove', update);
    drag.col.removeEventListener('pointerup', finish);
    drag.col.removeEventListener('pointercancel', cancel);
    drag = null;
  }

  // Le calendrier est re-rendu à chaque action htmx : on (ré)arme les colonnes
  // après chaque swap, avec un marqueur pour ne pas doubler les listeners.
  function setup() {
    document.querySelectorAll('.ag-col').forEach(function (col) {
      if (col.dataset.agReady) return;
      col.dataset.agReady = '1';
      col.addEventListener('pointerdown', start);
    });
  }

  document.body.addEventListener('htmx:afterSwap', setup);
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', setup);
  } else {
    setup();
  }
})();
