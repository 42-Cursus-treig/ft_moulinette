(function () {
  var list = document.getElementById('admin-sortable-list');
  if (!list) return;

  var dragged = null;

  function currentOrder() {
    return Array.prototype.map.call(
      list.querySelectorAll('.admin-row'),
      function (row) { return row.dataset.id; }
    );
  }

  function persistOrder() {
    fetch('/admin/reorder', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(currentOrder()),
    }).catch(function () {
      // Best effort : en cas d'échec réseau, un rechargement de la page
      // admin réaffichera simplement l'ordre encore persisté côté serveur.
    });
  }

  list.querySelectorAll('.admin-row').forEach(function (row) {
    row.addEventListener('dragstart', function (e) {
      dragged = row;
      row.classList.add('dragging');
      e.dataTransfer.effectAllowed = 'move';
    });

    row.addEventListener('dragend', function () {
      row.classList.remove('dragging');
      dragged = null;
    });

    row.addEventListener('dragover', function (e) {
      e.preventDefault();
      if (!dragged || dragged === row) return;

      var rect = row.getBoundingClientRect();
      var before = (e.clientY - rect.top) < rect.height / 2;
      list.insertBefore(dragged, before ? row : row.nextSibling);
    });

    row.addEventListener('drop', function (e) {
      e.preventDefault();
      persistOrder();
    });
  });

  // Confirmation avant suppression
  list.querySelectorAll('form[data-confirm-delete]').forEach(function (form) {
    form.addEventListener('submit', function (e) {
      if (!window.confirm('Confirmer la suppression.')) {
        e.preventDefault();
      }
    });
  });
})();
